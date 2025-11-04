package registry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/ulikunitz/xz"
)

var cacheLocation string

var (
	cacheLocks = map[string]*sync.Mutex{}
	cacheLock  = sync.Mutex{}
)

func lockCache(registry string, packageName string, version string) (unlock func()) {
	cacheLock.Lock()
	defer cacheLock.Unlock()
	key := fmt.Sprintf("%s:%s@%s", registry, packageName, version)
	if mut, ok := cacheLocks[key]; ok {
		mut.Lock()
		return func() {
			mut.Unlock()
		}
	}
	mut := &sync.Mutex{}
	mut.Lock()
	cacheLocks[key] = mut
	return func() {
		mut.Unlock()
	}
}

func getCacheLocation() string {
	if cacheLocation != "" {
		return cacheLocation
	}

	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		panic(err)
	}
	cacheLocation = filepath.Join(userCacheDir, "jmod")
	if err := os.MkdirAll(cacheLocation, 0755); err != nil {
		panic(err)
	}
	return cacheLocation
}

func CacheHas(registry string, packageName string, versionConstrains *semver.Constraints) (bool, string) {
	if versionConstrains == nil {
		return false, ""
	}
	dir := filepath.Join(getCacheLocation(), registry, packageName)
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return false, ""
	}
	for _, entry := range dirEntries {
		version, err := semver.NewVersion(entry.Name())
		if err != nil || version == nil {
			continue
		}
		if versionConstrains.Check(version) {
			return true, filepath.Join(dir, entry.Name(), "package")
		}
	}
	return false, ""
}

func CachePut(ctx context.Context, registry string, r Resolveable) (string, error) {
	unlock := lockCache(registry, r.GetName(), r.GetVersion())
	defer unlock()

	packageLocation := filepath.Join(
		getCacheLocation(),
		registry,
		r.GetName(),
		r.GetVersion(),
	)

	// Check if the package is already cached
	if _, err := os.Stat(packageLocation); err == nil {
		return filepath.Join(packageLocation, "package"), nil
	}

	// Prepare parent directory for temp artifacts and final extraction.
	parent := filepath.Dir(packageLocation)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("mkdir parent: %w", err)
	}

	// Download + checksum to a temp file.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	tmpArchive, gotSum, err := downloadToTempWithChecksum(
		ctx,
		r.GetSource(),
		parent,
		r.GetChecksumFormat(),
	)
	if err != nil {
		return "", err
	}
	// Always remove the temporary archive after we’re done with it.
	defer os.Remove(tmpArchive)

	// Normalize/parse expected checksum and compare.
	expSum, err := normalizeExpectedChecksum(
		r.GetChecksum(),
		r.GetChecksumFormat(),
	)
	if err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare(expSum, gotSum) != 1 {
		return "", fmt.Errorf("checksum mismatch for %s %s",
			r.GetName(), r.GetVersion())
	}

	// Extract to a staging dir, then atomically rename into place.
	staging, err := os.MkdirTemp(parent, ".extract-*")
	if err != nil {
		return "", fmt.Errorf("mktemp staging: %w", err)
	}
	// Clean up staging on error; on success we rename it and cleanup is moot.
	defer os.RemoveAll(staging)

	if err := extractArchive(tmpArchive, r.GetSourceFormat(), staging); err != nil {
		return "", err
	}

	// Replace existing destination atomically.
	if err := os.RemoveAll(packageLocation); err != nil {
		return "", fmt.Errorf("remove old dest: %w", err)
	}
	if err := os.Rename(staging, packageLocation); err != nil {
		return "", fmt.Errorf("rename staging: %w", err)
	}

	return filepath.Join(packageLocation, "package"), nil
}

func downloadToTempWithChecksum(
	ctx context.Context,
	url string,
	destDir string,
	cf ChecksumFormat,
) (tmpPath string, sum []byte, err error) {
	h, digestSize, err := hasherFor(cf)
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("new request: %w", err)
	}
	// Separate client gives us body read deadline via context.
	client := &http.Client{Timeout: 0}

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	f, err := os.CreateTemp(destDir, ".download-*.tmp")
	if err != nil {
		return "", nil, fmt.Errorf("create temp: %w", err)
	}
	tmpPath = f.Name()

	// On error, ensure the temp file is removed by caller's defer.
	defer func() {
		f.Close()
	}()

	// Stream copy into file and hasher simultaneously.
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("copy: %w", err)
	}

	// Flush to disk before verification.
	if err := f.Sync(); err != nil {
		return "", nil, fmt.Errorf("fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", nil, fmt.Errorf("close: %w", err)
	}

	sum = h.Sum(nil)
	if len(sum) != digestSize {
		return "", nil, fmt.Errorf("unexpected digest size")
	}
	return tmpPath, sum, nil
}

func hasherFor(cf ChecksumFormat) (h hash.Hash, size int, err error) {
	switch cf {
	case ChecksumFormatSha256:
		return sha256.New(), sha256.Size, nil
	case ChecksumFormatSha512:
		return sha512.New(), sha512.Size, nil
	default:
		return nil, 0, errors.New("unknown checksum format")
	}
}

func normalizeExpectedChecksum(
	exp []byte,
	cf ChecksumFormat,
) ([]byte, error) {
	want := 0
	switch cf {
	case ChecksumFormatSha256:
		want = sha256.Size
	case ChecksumFormatSha512:
		want = sha512.Size
	default:
		return nil, errors.New("unknown checksum format")
	}

	// If already raw bytes of correct size, use as-is.
	if len(exp) == want {
		return exp, nil
	}

	// Otherwise, try to decode as hex or base64 text.
	s := strings.TrimSpace(string(exp))

	// Try hex.
	if len(s) == want*2 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == want {
			return b, nil
		}
	}

	// Try base64 (std and raw).
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == want {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == want {
		return b, nil
	}

	return nil, fmt.Errorf("expected checksum not %d bytes (raw/hex/b64)", want)
}

func extractArchive(
	archivePath string,
	sf SourceFormat,
	destDir string,
) error {
	switch sf {
	case SourceFormatTarGz:
		return extractTarGz(archivePath, destDir)
	case SourceFormatTarXz:
		return extractTarXz(archivePath, destDir)
	default:
		return errors.New("unknown source format")
	}
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	return extractTarStream(gz, destDir)
}

func extractTarXz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("xz reader: %w", err)
	}

	return extractTarStream(xzr, destDir)
}

func extractTarStream(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("abs dest: %w", err)
	}

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		if hdr == nil || hdr.Name == "" {
			continue
		}

		target, err := secureJoin(absDest, hdr.Name)
		if err != nil {
			return fmt.Errorf("path check %q: %w", hdr.Name, err)
		}

		fi := hdr.FileInfo()

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, fi.Mode().Perm()); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}

		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkparent: %w", err)
			}
			out, err := os.OpenFile(
				target,
				os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				fi.Mode().Perm(),
			)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("write file: %w", err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file: %w", err)
			}

		case tar.TypeSymlink:
			// Only allow relative symlink targets.
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("absolute symlink rejected: %s",
					hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkparent: %w", err)
			}
			// Remove any existing path before creating the symlink.
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink: %w", err)
			}

		case tar.TypeLink:
			// Hardlink within the archive; ensure it resolves inside dest.
			linkTarget, err := secureJoin(absDest, hdr.Linkname)
			if err != nil {
				return fmt.Errorf("hardlink target: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkparent: %w", err)
			}
			// Remove any existing path before linking.
			_ = os.Remove(target)
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("hardlink: %w", err)
			}

		default:
			// Skip other types (pax headers, char/dev, etc.)
			continue
		}
	}
}

func secureJoin(base, name string) (string, error) {
	// Normalize incoming tar path (which uses forward slashes).
	clean := filepath.Clean(filepath.FromSlash(name))

	// Reject absolute paths.
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute path in archive: %q", name)
	}

	// Join and verify containment.
	full := filepath.Join(base, clean)

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}

	// Ensure full is within base.
	if absFull == absBase {
		return absFull, nil
	}
	sep := string(os.PathSeparator)
	if !strings.HasPrefix(absFull, absBase+sep) {
		return "", fmt.Errorf("path escapes dest: %q", name)
	}
	return absFull, nil
}

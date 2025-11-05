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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/tsukinoko-kun/disize"
	"github.com/tsukinoko-kun/jmod/statusui"
	"github.com/ulikunitz/xz"
)

var cacheLocation string
var tarballCacheLocation string

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

// GetTarballCacheLocation returns the directory where NPM tarballs are cached.
// For testing, you can override this by setting the JMOD_TARBALL_CACHE environment variable.
// To clear the cache during testing, simply delete this directory.
func GetTarballCacheLocation() string {
	return getTarballCacheLocation()
}

func getTarballCacheLocation() string {
	if tarballCacheLocation != "" {
		return tarballCacheLocation
	}

	// Check for environment variable override (useful for testing)
	if envCache := os.Getenv("JMOD_TARBALL_CACHE"); envCache != "" {
		tarballCacheLocation = envCache
		if err := os.MkdirAll(tarballCacheLocation, 0755); err != nil {
			panic(err)
		}
		return tarballCacheLocation
	}

	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		panic(err)
	}
	tarballCacheLocation = filepath.Join(userCacheDir, "jmod-tarballs")
	if err := os.MkdirAll(tarballCacheLocation, 0755); err != nil {
		panic(err)
	}
	return tarballCacheLocation
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

	statusKey := fmt.Sprintf("%s:%s@%s", registry, r.GetName(), r.GetVersion())

	// Check if the package is already cached
	if _, err := os.Stat(packageLocation); err == nil {
		// Package is already cached, no need to download/install again
		return filepath.Join(packageLocation, "package"), nil
	}

	// Prepare parent directory for temp artifacts and final extraction.
	parent := filepath.Dir(packageLocation)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		statusui.Set(statusKey, statusui.ErrorStatus{
			Message: fmt.Sprintf("Failed to prepare %s@%s", r.GetName(), r.GetVersion()),
			Err:     err,
		})
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
		statusKey,
		r.GetName(),
		r.GetVersion(),
	)
	if err != nil {
		// Check if error is due to context cancellation
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Silently clear the status on cancellation
			statusui.Clear(statusKey)
		} else {
			statusui.Set(statusKey, statusui.ErrorStatus{
				Message: fmt.Sprintf("Failed to download %s@%s", r.GetName(), r.GetVersion()),
				Err:     err,
			})
		}
		return "", err
	}
	// Always remove the temporary archive after we're done with it.
	defer os.Remove(tmpArchive)

	// Normalize/parse expected checksum and compare.
	expSum, err := normalizeExpectedChecksum(
		r.GetChecksum(),
		r.GetChecksumFormat(),
	)
	if err != nil {
		statusui.Set(statusKey, statusui.ErrorStatus{
			Message: fmt.Sprintf("Failed to verify checksum for %s@%s", r.GetName(), r.GetVersion()),
			Err:     err,
		})
		return "", err
	}
	if subtle.ConstantTimeCompare(expSum, gotSum) != 1 {
		err := fmt.Errorf("checksum mismatch for %s %s", r.GetName(), r.GetVersion())
		statusui.Set(statusKey, statusui.ErrorStatus{
			Message: fmt.Sprintf("Checksum mismatch for %s@%s", r.GetName(), r.GetVersion()),
			Err:     err,
		})
		return "", err
	}

	// Extract to a staging dir, then atomically rename into place.
	statusui.Set(statusKey, statusui.TextStatus{
		Text: fmt.Sprintf("📦 Extracting %s@%s", r.GetName(), r.GetVersion()),
	})

	staging, err := os.MkdirTemp(parent, ".extract-*")
	if err != nil {
		statusui.Set(statusKey, statusui.ErrorStatus{
			Message: fmt.Sprintf("Failed to create staging dir for %s@%s", r.GetName(), r.GetVersion()),
			Err:     err,
		})
		return "", fmt.Errorf("mktemp staging: %w", err)
	}
	// Clean up staging on error; on success we rename it and cleanup is moot.
	defer os.RemoveAll(staging)

	if err := extractArchive(tmpArchive, r.GetSourceFormat(), staging); err != nil {
		// Check if error is due to context cancellation
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Silently clear the status on cancellation
			statusui.Clear(statusKey)
		} else {
			statusui.Set(statusKey, statusui.ErrorStatus{
				Message: fmt.Sprintf("Failed to extract %s@%s", r.GetName(), r.GetVersion()),
				Err:     err,
			})
		}
		return "", err
	}

	// Replace existing destination atomically.
	if err := os.RemoveAll(packageLocation); err != nil {
		statusui.Set(statusKey, statusui.ErrorStatus{
			Message: fmt.Sprintf("Failed to prepare installation for %s@%s", r.GetName(), r.GetVersion()),
			Err:     err,
		})
		return "", fmt.Errorf("remove old dest: %w", err)
	}
	if err := os.Rename(staging, packageLocation); err != nil {
		statusui.Set(statusKey, statusui.ErrorStatus{
			Message: fmt.Sprintf("Failed to install %s@%s", r.GetName(), r.GetVersion()),
			Err:     err,
		})
		return "", fmt.Errorf("rename staging: %w", err)
	}

	statusui.Set(statusKey, statusui.SuccessStatus{
		Message: fmt.Sprintf("Installed %s@%s", r.GetName(), r.GetVersion()),
	})

	// Clear status after installation completes
	// The batching system will handle this smoothly
	go func(ctx context.Context, key string) {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
		}
		statusui.Clear(key)
	}(ctx, statusKey)

	return filepath.Join(packageLocation, "package"), nil
}

func downloadToTempWithChecksum(
	ctx context.Context,
	url string,
	destDir string,
	cf ChecksumFormat,
	statusKey string,
	packageName string,
	packageVersion string,
) (tmpPath string, sum []byte, err error) {
	h, digestSize, err := hasherFor(cf)
	if err != nil {
		return "", nil, err
	}

	// Check tarball cache first
	cachedTarball, cachedSum := getCachedTarball(url, cf)
	if cachedTarball != "" {
		// Verify checksum of cached tarball
		statusui.Set(statusKey, statusui.TextStatus{
			Text: fmt.Sprintf("📦 Using cached %s@%s", packageName, packageVersion),
		})
		f, err := os.Open(cachedTarball)
		if err == nil {
			defer f.Close()
			h.Reset()
			if _, err := io.Copy(h, f); err == nil {
				gotSum := h.Sum(nil)
				if len(gotSum) == digestSize && subtle.ConstantTimeCompare(cachedSum, gotSum) == 1 {
					// Cache hit - copy to temp location for caller
					tmpFile, err := os.CreateTemp(destDir, ".download-*.tmp")
					if err == nil {
						tmpPath = tmpFile.Name()
						tmpFile.Close()
						if err := copyFile(cachedTarball, tmpPath); err == nil {
							return tmpPath, cachedSum, nil
						}
						os.Remove(tmpPath)
					}
				}
			}
		}
		// If cache verification failed, continue to download
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

	// Get content length for progress tracking
	contentLength := resp.ContentLength

	// Create a progress reader
	var reader io.Reader = resp.Body
	if contentLength > 0 {
		statusui.Set(statusKey, statusui.ProgressStatus{
			Label:   fmt.Sprintf("⬇️  Downloading %s@%s", packageName, packageVersion),
			Current: 0,
			Total:   contentLength,
		})
		reader = &progressReader{
			reader:    resp.Body,
			statusKey: statusKey,
			label:     fmt.Sprintf("⬇️  Downloading %s@%s", packageName, packageVersion),
			total:     contentLength,
		}
	} else {
		statusui.Set(statusKey, statusui.TextStatus{
			Text: fmt.Sprintf("⬇️  Downloading %s@%s", packageName, packageVersion),
		})
	}

	// Stream copy into file and hasher simultaneously.
	_, err = io.Copy(io.MultiWriter(f, h), reader)
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

	// Save to tarball cache for future use
	saveTarballToCache(url, tmpPath, sum, cf)

	return tmpPath, sum, nil
}

// progressReader wraps an io.Reader to track download progress
type progressReader struct {
	reader    io.Reader
	statusKey string
	label     string
	total     int64
	current   int64
	lastPrint int64
	mu        sync.Mutex
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.mu.Lock()
	pr.current += int64(n)
	// Update status every 100KB or at completion
	shouldUpdate := pr.current-pr.lastPrint >= 100*disize.Kib || pr.current == pr.total || err == io.EOF
	if shouldUpdate {
		pr.lastPrint = pr.current
		// Copy values for use outside the lock
		current := pr.current
		total := pr.total
		label := pr.label
		statusKey := pr.statusKey
		pr.mu.Unlock()
		statusui.Set(statusKey, statusui.ProgressStatus{
			Label:   label,
			Current: current,
			Total:   total,
		})
	} else {
		pr.mu.Unlock()
	}

	return n, err
}

func getCachedTarball(url string, cf ChecksumFormat) (string, []byte) {
	tarballCacheDir := getTarballCacheLocation()

	// Create a filename based on URL hash (to avoid path issues)
	urlHash := fmt.Sprintf("%x", sha256.Sum256([]byte(url)))

	// Try both checksum formats (since we might have cached with a different format)
	for _, format := range []ChecksumFormat{cf, ChecksumFormatSha512, ChecksumFormatSha256} {
		if format == ChecksumFormatUnknown {
			continue
		}

		var ext string
		switch format {
		case ChecksumFormatSha256:
			ext = ".sha256"
		case ChecksumFormatSha512:
			ext = ".sha512"
		default:
			continue
		}

		cacheFile := filepath.Join(tarballCacheDir, urlHash+ext+".tgz")

		// Check if cached file exists
		if _, err := os.Stat(cacheFile); err != nil {
			continue
		}

		// Read checksum from companion file
		checksumFile := cacheFile + ".checksum"
		checksumData, err := os.ReadFile(checksumFile)
		if err != nil {
			continue
		}

		// Parse checksum based on stored format
		sum, err := normalizeExpectedChecksum(checksumData, format)
		if err != nil {
			continue
		}

		// Verify the format matches what we expect
		expSize := 0
		switch cf {
		case ChecksumFormatSha256:
			expSize = sha256.Size
		case ChecksumFormatSha512:
			expSize = sha512.Size
		default:
			continue
		}

		if len(sum) != expSize {
			continue
		}

		return cacheFile, sum
	}

	return "", nil
}

func saveTarballToCache(url string, tmpPath string, sum []byte, cf ChecksumFormat) {
	if cf == ChecksumFormatUnknown {
		return
	}

	tarballCacheDir := getTarballCacheLocation()

	// Create a filename based on URL hash with checksum format extension
	urlHash := fmt.Sprintf("%x", sha256.Sum256([]byte(url)))

	var ext string
	switch cf {
	case ChecksumFormatSha256:
		ext = ".sha256"
	case ChecksumFormatSha512:
		ext = ".sha512"
	default:
		return
	}

	cacheFile := filepath.Join(tarballCacheDir, urlHash+ext+".tgz")
	checksumFile := cacheFile + ".checksum"

	// Copy tarball to cache
	if err := copyFile(tmpPath, cacheFile); err != nil {
		return
	}

	// Save checksum
	sumHex := hex.EncodeToString(sum)
	if err := os.WriteFile(checksumFile, []byte(sumHex), 0644); err != nil {
		os.Remove(cacheFile) // Clean up on error
		return
	}
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
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
	tmp, err := os.CreateTemp("", "jmod-tar-*.tar")
	if err != nil {
		return fmt.Errorf("create temp tar: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return fmt.Errorf("buffer tar: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind tar: %w", err)
	}

	return extractTarReadSeeker(tmp, destDir)
}

func extractTarReadSeeker(rs io.ReadSeeker, destDir string) error {
	rootDir, err := determineTarRoot(rs)
	if err != nil {
		return err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind tar: %w", err)
	}

	absDest, err := filepath.Abs(filepath.Join(destDir, "package"))
	if err != nil {
		return fmt.Errorf("abs dest: %w", err)
	}
	if err := os.MkdirAll(absDest, 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	tr := tar.NewReader(rs)
	var extractedPkgJSON bool

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		if hdr == nil || hdr.Name == "" {
			continue
		}

		if isMetadataHeader(hdr.Typeflag) {
			continue
		}

		normName, err := normalizeTarPath(hdr.Name)
		if err != nil {
			return fmt.Errorf("tar entry %q: %w", hdr.Name, err)
		}
		if normName == "" {
			continue
		}

		trimmed, ok := trimTarPath(normName, rootDir)
		if !ok {
			continue
		}
		if trimmed == "" {
			continue
		}

		target, err := secureJoin(absDest, trimmed)
		if err != nil {
			return fmt.Errorf("path check %q: %w", hdr.Name, err)
		}

		fi := hdr.FileInfo()

		switch hdr.Typeflag {
		case tar.TypeDir:
			dirPerm := fi.Mode().Perm()
			if dirPerm&0o200 == 0 {
				dirPerm |= 0o200
			}
			if err := os.MkdirAll(target, dirPerm); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}

		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkparent: %w", err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
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
			if trimmed == "package.json" {
				extractedPkgJSON = true
			}

		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("absolute symlink rejected: %s", hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkparent: %w", err)
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink: %w", err)
			}

		case tar.TypeLink:
			linkName, err := normalizeTarPath(hdr.Linkname)
			if err != nil {
				return fmt.Errorf("hardlink linkname %q: %w", hdr.Linkname, err)
			}
			linkTrimmed, ok := trimTarPath(linkName, rootDir)
			if !ok || linkTrimmed == "" {
				return fmt.Errorf("hardlink target outside package root: %s", hdr.Linkname)
			}
			linkTarget, err := secureJoin(absDest, linkTrimmed)
			if err != nil {
				return fmt.Errorf("hardlink target: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkparent: %w", err)
			}
			_ = os.Remove(target)
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("hardlink: %w", err)
			}

		default:
			continue
		}
	}

	if !extractedPkgJSON {
		return errors.New("package.json not found in archive")
	}

	return nil
}

func determineTarRoot(rs io.ReadSeeker) (string, error) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind tar: %w", err)
	}
	tr := tar.NewReader(rs)

	topComponents := map[string]int{}
	var pkgJSONPaths []string

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}
		if hdr == nil || hdr.Name == "" {
			continue
		}
		if isMetadataHeader(hdr.Typeflag) {
			continue
		}

		normName, err := normalizeTarPath(hdr.Name)
		if err != nil {
			return "", fmt.Errorf("tar entry %q: %w", hdr.Name, err)
		}
		if normName == "" {
			continue
		}

		comp := firstPathComponent(normName)
		if comp != "" {
			topComponents[comp]++
		}

		if path.Base(normName) == "package.json" {
			pkgJSONPaths = append(pkgJSONPaths, normName)
		}
	}

	if len(pkgJSONPaths) > 0 {
		sort.Slice(pkgJSONPaths, func(i, j int) bool {
			return depth(pkgJSONPaths[i]) < depth(pkgJSONPaths[j])
		})
		dir := path.Dir(pkgJSONPaths[0])
		if dir == "." {
			return "", nil
		}
		return dir, nil
	}

	if len(topComponents) == 1 {
		var prefix string
		for k := range topComponents {
			prefix = k
		}
		if _, err := rs.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("rewind tar: %w", err)
		}
		tr = tar.NewReader(rs)
		prefixSlash := prefix + "/"
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", fmt.Errorf("tar read: %w", err)
			}
			if hdr == nil || hdr.Name == "" {
				continue
			}
			if isMetadataHeader(hdr.Typeflag) {
				continue
			}
			normName, err := normalizeTarPath(hdr.Name)
			if err != nil {
				return "", fmt.Errorf("tar entry %q: %w", hdr.Name, err)
			}
			if normName == "" {
				continue
			}
			if strings.HasPrefix(normName, prefixSlash) && strings.TrimPrefix(normName, prefixSlash) == "package.json" {
				return prefix, nil
			}
		}
	}

	return "", errors.New("package.json not found in archive")
}

func normalizeTarPath(name string) (string, error) {
	cleaned := strings.ReplaceAll(name, "\\", "/")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = path.Clean(cleaned)
	cleaned = strings.TrimPrefix(cleaned, "./")

	if cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("path escapes root: %q", name)
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("absolute path in archive: %q", name)
	}

	return cleaned, nil
}

func trimTarPath(normalized, root string) (string, bool) {
	if root == "" {
		return normalized, true
	}
	if normalized == root {
		return "", true
	}
	prefix := root + "/"
	if strings.HasPrefix(normalized, prefix) {
		return strings.TrimPrefix(normalized, prefix), true
	}
	return "", false
}

func firstPathComponent(p string) string {
	if p == "" {
		return ""
	}
	if idx := strings.IndexByte(p, '/'); idx >= 0 {
		return p[:idx]
	}
	return p
}

func depth(p string) int {
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

func isMetadataHeader(t byte) bool {
	switch t {
	case tar.TypeXHeader,
		tar.TypeXGlobalHeader,
		tar.TypeGNULongName,
		tar.TypeGNULongLink:
		return true
	}
	return false
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

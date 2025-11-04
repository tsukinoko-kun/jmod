package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/tsukinoko-kun/jmod/utils"
)

type npmLatest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Id      string `json:"_id"`
	Dist    dist   `json:"dist"`
}

type dist struct {
	Shasum       string      `json:"shasum"`
	Tarball      string      `json:"tarball"`
	FileCount    int         `json:"fileCount"`
	Integrity    string      `json:"integrity"`
	Signatures   []signature `json:"signatures"`
	UnpackedSize uint        `json:"unpackedSize"`
}

type signature struct {
	Sig   string `json:"sig"`
	Keyid string `json:"keyid"`
}

func (p npmLatest) String() string {
	return fmt.Sprintf("npm:%s@%s", p.Name, p.Version)
}

func (p npmLatest) GetName() string {
	return p.Name
}
func (p npmLatest) GetVersion() string {
	return p.Version
}
func (p npmLatest) GetSource() string {
	return p.Dist.Tarball
}
func (p npmLatest) GetSourceFormat() SourceFormat {
	return SourceFormatTarGz
}

func (p npmLatest) GetChecksumFormat() ChecksumFormat {
	if strings.HasPrefix(p.Dist.Integrity, "sha512-") {
		return ChecksumFormatSha512
	}
	if strings.HasPrefix(p.Dist.Integrity, "sha256-") {
		return ChecksumFormatSha256
	}
	return ChecksumFormatUnknown
}

func (p npmLatest) GetChecksum() []byte {
	if len(p.Dist.Integrity) > 7 {
		return utils.Must(base64.StdEncoding.DecodeString(p.Dist.Integrity[7:]))
	}
	return nil
}

func Npm_GetLatestVersion(pkg string) (string, error) {
	return Npm_GetVersion(pkg, "latest")
}

func Npm_GetVersion(pkg string, versionName string) (string, error) {
	urlPath, err := url.JoinPath("/", pkg, versionName)
	if err != nil {
		return "", err
	}
	resp, err := http.Get("https://registry.npmjs.org" + urlPath)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var latest npmLatest
	jd := json.NewDecoder(resp.Body)
	err = jd.Decode(&latest)
	if err != nil {
		return "", err
	}

	if versionName == "latest" {
		latest.Version = "^" + latest.Version
	}

	return latest.Version, nil
}

type npmFull struct {
	Name     string               `json:"name"`
	Versions map[string]npmLatest `json:"versions"`
}

func Npm_Resolve(ctx context.Context, packageName string, versionConstraint *semver.Constraints) (Resolveable, error) {
	urlPath, err := url.JoinPath("/", packageName)
	if err != nil {
		return nil, fmt.Errorf("construct URL path: %s", packageName)
	}
	url := "https://registry.npmjs.org" + urlPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", url, err)
	}
	defer resp.Body.Close()

	var full npmFull
	jd := json.NewDecoder(resp.Body)
	err = jd.Decode(&full)
	if err != nil {
		return nil, fmt.Errorf("decode npm API response %s: %w", url, err)
	}

	var semverVersions []*semver.Version
	for version := range full.Versions {
		semverVersion, err := semver.NewVersion(version)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", version, err)
		}
		if versionConstraint.Check(semverVersion) {
			semverVersions = append(semverVersions, semverVersion)
		}
	}
	sort.Sort(semver.Collection(semverVersions))
	for _, semverVersion := range slices.Backward(semverVersions) {
		if npmVersion, ok := full.Versions[semverVersion.String()]; ok {
			return npmVersion, nil
		}
	}

	return nil, errors.New("not found")
}

package registry

import (
	"encoding/json"
	"net/http"
	"net/url"
)

type npmLatest struct {
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

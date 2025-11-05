package registry

import (
	"fmt"
	"strings"
)

type Package struct {
	PackageName string
	Version     string
	Source      string
	Optional    bool
}

func (p Package) String() string {
	return fmt.Sprintf("%s:%s@%s", p.Source, p.PackageName, p.Version)
}

func FindInstallablePackage(identifier string) (Package, error) {
	if npmIdentifyer, ok := strings.CutPrefix(identifier, "npm:"); ok {
		return findInstallablePackageNpm(npmIdentifyer)
	}
	// fallback to npm
	return findInstallablePackageNpm(identifier)
}

func resolveVersion(identifier string) string {
	lastAtIndex := strings.LastIndex(identifier, "@")
	// scoped package would have @ as the first character
	if lastAtIndex > 0 {
		return identifier[lastAtIndex+1:]
	}
	// no version specified -> default to latest
	return "latest"
}

func findInstallablePackageNpm(identifier string) (Package, error) {
	version := resolveVersion(identifier)
	version, err := Npm_GetVersion(identifier, version)
	if err != nil {
		return Package{}, err
	}
	return Package{
		PackageName: identifier,
		Version:     version,
		Source:      "npm",
	}, nil
}

type SourceFormat uint8

const (
	SourceFormatUnknown SourceFormat = iota
	SourceFormatTarGz
	SourceFormatTarXz
)

type ChecksumFormat uint8

const (
	ChecksumFormatUnknown ChecksumFormat = iota
	ChecksumFormatSha256
	ChecksumFormatSha512
)

type Resolveable interface {
	// String representation of the package
	String() string
	// Name of the package
	GetName() string
	// Version of the package
	GetVersion() string
	// URL to download the package archive
	GetSource() string
	// Format of the package archive
	GetSourceFormat() SourceFormat
	// Format of the checksum
	GetChecksumFormat() ChecksumFormat
	// Checksum of the package archive as provided by the registry
	GetChecksum() []byte
}

package registry

import "strings"

type Package struct {
	PackageName string
	Version     string
	Source      string
}

func Resolve(identifier string) (Package, error) {
	if npmIdentifyer, ok := strings.CutPrefix(identifier, "npm:"); ok {
		return resolveNpm(npmIdentifyer)
	}
	// fallback to npm
	return resolveNpm(identifier)
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

func resolveNpm(identifier string) (Package, error) {
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

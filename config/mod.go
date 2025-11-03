package config

import (
	_json "encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tsukinoko-kun/jmod/ignore"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/registry"
	json "github.com/tsukinoko-kun/jsonedit"
)

type Decoder interface {
	Decode(v any) error
}

type Mod struct {
	fileLocation          string            `json:"-"`
	Scripts               map[string]string `json:"scripts"`
	NpmAutoDependencies   map[string]string `json:"dependencies"`
	NpmManualDependencies map[string]string `json:"devDependencies"`
}

func (m *Mod) GetFileLocation() string {
	return m.fileLocation
}

func Load(path string) (*json.Document[*Mod], error) {
	modFilePath, err := getPackageFilePath(path)
	if err != nil {
		return nil, err
	}
	modFile, err := os.Open(modFilePath)
	defer modFile.Close()
	mod := &Mod{fileLocation: modFilePath}
	jsonEditor, err := json.Parse(modFile, mod)
	return jsonEditor, err
}

func Write(mod *json.Document[*Mod]) error {
	if mod.TypedData.fileLocation == "" {
		return fmt.Errorf("mod file location not set")
	}
	f, err := os.Create(mod.TypedData.fileLocation)
	if err != nil {
		return err
	}
	defer f.Close()
	defer f.Sync()
	return mod.Write(f)
}

var packageFileName = []string{
	"package.json",
	"package.json5",
	"package.jsonc",
}

func getPackageFilePath(root string) (string, error) {
	for _, fileName := range packageFileName {
		packageFilePath := filepath.Join(root, fileName)
		if _, err := os.Stat(packageFilePath); err == nil {
			return packageFilePath, nil
		}
	}
	return "", fmt.Errorf("mod file not found")
}

func New() error {
	packageData := map[string]any{
		"scripts":         map[string]string{},
		"dependencies":    map[string]string{},
		"devDependencies": map[string]string{},
	}
	modFile := filepath.Join(meta.Pwd(), packageFileName[0])
	f, err := os.Create(modFile)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := _json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(packageData)
	if err != nil {
		return err
	}
	return nil
}

func FindSubMods(root string) []*json.Document[*Mod] {
	ignoreMatcher := ignore.GetIgnoreMatcher(root)

	subMods := []*json.Document[*Mod]{}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if d.IsDir() {
			gitPath := strings.Split(
				strings.TrimPrefix(path, root),
				string(filepath.Separator),
			)
			if len(gitPath) != 0 && gitPath[0] == "" {
				gitPath = gitPath[1:]
			}
			if ignoreMatcher.Match(gitPath, true) {
				return filepath.SkipDir
			}
			m, err := Load(path)
			if err == nil {
				subMods = append(subMods, m)
			}
		}
		return nil
	})
	return subMods
}

func Install(mod *json.Document[*Mod], pack registry.Package) error {
	if pack.Source != "npm" {
		return fmt.Errorf("unsupported package source: %s", pack.Source)
	}

	// check if the package is already installed
	if _, ok := mod.TypedData.NpmAutoDependencies[pack.PackageName]; ok {
		// update the version
		mod.TypedData.NpmAutoDependencies[pack.PackageName] = pack.Version
		return nil
	}

	mod.TypedData.NpmManualDependencies[pack.PackageName] = pack.Version
	return nil
}

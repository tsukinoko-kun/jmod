package config

import (
	"context"
	_json "encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/tsukinoko-kun/jmod/ignore"
	"github.com/tsukinoko-kun/jmod/logger"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/registry"
	"github.com/tsukinoko-kun/jmod/utils"
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

func (m *Mod) ListDependencies(yield func(registry.Package) bool) {
	for dep := range m.NpmAutoDependencies {
		if !yield(registry.Package{
			PackageName: dep,
			Version:     m.NpmAutoDependencies[dep],
			Source:      "npm",
		}) {
			return
		}
	}
	for dep := range m.NpmManualDependencies {
		if !yield(registry.Package{
			PackageName: dep,
			Version:     m.NpmManualDependencies[dep],
			Source:      "npm",
		}) {
			return
		}
	}
}

type ResolvedDependency struct {
	PackageName    string
	CachedLocation string
}

func (m *Mod) ResolveDependenciesDeep(ctx context.Context) <-chan ResolvedDependency {
	wg := sync.WaitGroup{}

	ch := make(chan ResolvedDependency, 32)

depLoop:
	for packageName, version := range utils.Join2(utils.IterMap(m.NpmAutoDependencies), utils.IterMap(m.NpmManualDependencies)) {
		select {
		case <-ctx.Done():
			break depLoop
		default:
		}

		constPackageName := packageName
		wg.Go(func() {
			if strings.HasPrefix(version, "file:") || strings.HasPrefix(version, "./") || strings.HasPrefix(version, "../") || strings.HasPrefix(version, "/") {
				absPath := filepath.Join(filepath.Dir(m.GetFileLocation()), strings.TrimPrefix(version, "file:"))
				if _, err := os.Stat(absPath); err == nil {
					select {
					case ch <- ResolvedDependency{constPackageName, absPath}:
					case <-ctx.Done():
					}
				} else {
					logger.Printf("local file dep %s not found for mod %s\n", version, m.GetFileLocation())
				}
				return
			} else if strings.HasPrefix(version, "git:") {
				fmt.Println("TODO git", version)
				// TODO
				return
			} else if strings.HasPrefix(version, "github:") {
				fmt.Println("TODO github", version)
				// TODO
				return
			} else if alias, ok := strings.CutPrefix(version, "npm:"); ok {
				if lastAtIndex := strings.LastIndex(alias, "@"); lastAtIndex > 0 {
					packageName = alias[:lastAtIndex]
					version = alias[lastAtIndex+1:]
				} else {
					packageName = alias
					version = "latest"
				}
			}
			versionConstraint, err := semver.NewConstraint(version)
			if err != nil {
				// might be a label/tag
				if tagVersion, reqErr := registry.Npm_GetVersion(packageName, version); reqErr == nil {
					var newErr error
					versionConstraint, newErr = semver.NewConstraint(tagVersion)
					if newErr != nil {
						logger.Errorf("invalid version constraint %s for %s in %s, skipping\n", version, packageName, m.GetFileLocation())
						return
					}
				} else {
					logger.Errorf("invalid version constraint %s for %s in %s, skipping\n", version, packageName, m.GetFileLocation())
					return
				}
			}
			if ok, cachedLocation := registry.CacheHas("npm", packageName, versionConstraint); ok {
				select {
				case ch <- ResolvedDependency{constPackageName, cachedLocation}:
				case <-ctx.Done():
				}
				return
			}
			resolver, err := registry.Npm_Resolve(ctx, packageName, versionConstraint)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					logger.Errorf("failed to resolve %s@%s: %s\n", packageName, version, err)
				}
				return
			}
			start := time.Now()
			cachedLocation, err := registry.CachePut(ctx, "npm", resolver)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					logger.Errorf("failed to cache %s@%s: %s\n", packageName, resolver.GetVersion(), err)
				}
				return
			}
			logger.Printf("downloaded %s in %s\n", resolver.String(), time.Since(start))
			select {
			case ch <- ResolvedDependency{constPackageName, cachedLocation}:
			case <-ctx.Done():
			}
		})
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
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
		if err != nil {
			return err
		}
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

func Install(mod *json.Document[*Mod], pack registry.Package, dev bool) error {
	if pack.Source != "npm" {
		return fmt.Errorf("unsupported package source: %s", pack.Source)
	}

	if dev {
		if mod.TypedData.NpmAutoDependencies != nil {
			delete(mod.TypedData.NpmAutoDependencies, pack.PackageName)
		}
		if mod.TypedData.NpmManualDependencies == nil {
			mod.TypedData.NpmManualDependencies = map[string]string{}
		}
		mod.TypedData.NpmManualDependencies[pack.PackageName] = pack.Version
		return nil
	}

	if mod.TypedData.NpmManualDependencies != nil {
		delete(mod.TypedData.NpmManualDependencies, pack.PackageName)
	}
	if mod.TypedData.NpmAutoDependencies == nil {
		mod.TypedData.NpmAutoDependencies = map[string]string{}
	}
	mod.TypedData.NpmAutoDependencies[pack.PackageName] = pack.Version
	return nil
}

func Uninstall(mod *json.Document[*Mod], name string) error {
	if mod.TypedData.NpmAutoDependencies != nil {
		delete(mod.TypedData.NpmAutoDependencies, name)
	}
	if mod.TypedData.NpmManualDependencies != nil {
		delete(mod.TypedData.NpmManualDependencies, name)
	}
	return nil
}

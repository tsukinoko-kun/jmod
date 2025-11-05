package config

import (
	"bytes"
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
	fileLocation            string            `json:"-"`
	Scripts                 map[string]string `json:"scripts"`
	NpmDependencies         map[string]string `json:"dependencies"`
	NpmDevDependencies      map[string]string `json:"devDependencies"`
	NpmOptionalDependencies map[string]string `json:"optionalDependencies"`
}

func (m *Mod) GetFileLocation() string {
	return m.fileLocation
}

type (
	ResolvedDependency struct {
		PackageName    string
		CachedLocation string
	}

	ResolvedDependencyBin struct {
		BinName string
		BinPath string
	}
)

type packageJsonForLifecycle struct {
	Name    *string            `json:"name,omitempty"`
	Version *string            `json:"version,omitempty"`
	Scripts *map[string]string `json:"scripts,omitempty"`
}

func (p *packageJsonForLifecycle) Identifier() string {
	if p.Name != nil {
		if p.Version != nil {
			return fmt.Sprintf("%s@%s", *p.Name, *p.Version)
		}
		return *p.Name
	}
	return ""
}

type packageJsonForBin struct {
	Name *string           `json:"name"`
	Bin  map[string]string `json:"bin"`
}

func (p *packageJsonForBin) UnmarshalJSON(data []byte) error {
	// intermediate type to capture raw bin bytes
	type rawPkg struct {
		Name *string          `json:"name"`
		Bin  _json.RawMessage `json:"bin"`
	}

	var r rawPkg
	if err := _json.Unmarshal(data, &r); err != nil {
		return err
	}

	p.Name = r.Name

	// no bin field or explicit null -> leave as nil
	if len(r.Bin) == 0 || bytes.Equal(bytes.TrimSpace(r.Bin), []byte("null")) {
		p.Bin = nil
		return nil
	}

	// Try to unmarshal as map[string]string first
	var m map[string]string
	if err := _json.Unmarshal(r.Bin, &m); err == nil {
		p.Bin = m
		return nil
	}

	// Otherwise try as single string
	var s string
	if err := _json.Unmarshal(r.Bin, &s); err == nil {
		if p.Name == nil {
			return fmt.Errorf("bin is a string but package name is missing")
		}
		p.Bin = map[string]string{*p.Name: s}
		return nil
	}

	return fmt.Errorf("bin field has an unexpected JSON type")
}

func getPackageJsonForBin(packageJsonPath string) (packageJsonForBin, error) {
	f, err := os.Open(packageJsonPath)
	if err != nil {
		return packageJsonForBin{}, err
	}
	defer f.Close()
	var pj packageJsonForBin
	jd := _json.NewDecoder(f)
	err = jd.Decode(&pj)
	if err != nil {
		return packageJsonForBin{}, fmt.Errorf("decode %s: %w", packageJsonPath, err)
	}
	return pj, nil
}

func GetPackageJsonForLifecycle(packageJsonPath string) (packageJsonForLifecycle, error) {
	f, err := os.Open(packageJsonPath)
	if err != nil {
		return packageJsonForLifecycle{}, err
	}
	defer f.Close()
	var pj packageJsonForLifecycle
	jd := _json.NewDecoder(f)
	err = jd.Decode(&pj)
	if err != nil {
		return packageJsonForLifecycle{}, fmt.Errorf("decode %s: %w", packageJsonPath, err)
	}
	return pj, nil
}

func ResolveBins(ctx context.Context, packageJsonPath string) ([]ResolvedDependencyBin, error) {
	pj, err := getPackageJsonForBin(packageJsonPath)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(packageJsonPath)
	var bins []ResolvedDependencyBin
	for binName, binRelativePath := range pj.Bin {
		binAbsolutePath := filepath.Join(root, binRelativePath)
		if err := utils.EnsureExecutable(binAbsolutePath); err != nil {
			return nil, err
		}
		bins = append(bins, ResolvedDependencyBin{
			BinName: binName,
			BinPath: binAbsolutePath,
		})
	}
	return bins, nil
}

func runGo(version string, packageName string, constPackageName string, ctx context.Context, ch chan<- ResolvedDependency, m *Mod, optional bool) func() {
	return func() {
		if strings.HasPrefix(version, "file:") || strings.HasPrefix(version, "./") || strings.HasPrefix(version, "../") || strings.HasPrefix(version, "/") {
			absPath := filepath.Join(filepath.Dir(m.GetFileLocation()), strings.TrimPrefix(version, "file:"))
			if _, err := os.Stat(absPath); err == nil {
				select {
				case ch <- ResolvedDependency{constPackageName, absPath}:
				case <-ctx.Done():
				}
			} else {
				if optional {
					logger.Printf("local file dep %s not found for mod %s", version, m.GetFileLocation())
				} else {
					meta.CancelCause(fmt.Errorf("local file dep %s not found for mod %s", version, m.GetFileLocation()))
				}
			}
			return
		} else if strings.HasPrefix(version, "git:") {
			if optional {
				logger.Printf("TODO git %s", version)
			} else {
				meta.CancelCause(fmt.Errorf("TODO git %s", version))
			}
			return
		} else if strings.HasPrefix(version, "github:") {
			if optional {
				logger.Printf("TODO github %s", version)
			} else {
				meta.CancelCause(fmt.Errorf("TODO github %s", version))
			}
			return
		} else if pck, ok := strings.CutPrefix(version, "jsr:"); ok {
			if optional {
				logger.Printf("TODO jsr %s", pck)
			} else {
				meta.CancelCause(fmt.Errorf("TODO jsr %s", pck))
			}
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
					if optional {
						logger.Printf("invalid version constraint %s for %s in %s, skipping", version, packageName, m.GetFileLocation())
					} else {
						meta.CancelCause(fmt.Errorf("invalid version constraint %s for %s in %s, skipping", version, packageName, m.GetFileLocation()))
					}
					return
				}
			} else {
				if optional {
					logger.Printf("invalid version constraint %s for %s in %s, skipping", version, packageName, m.GetFileLocation())
				} else {
					meta.CancelCause(fmt.Errorf("invalid version constraint %s for %s in %s, skipping", version, packageName, m.GetFileLocation()))
				}
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
				if optional {
					logger.Printf("failed to resolve %s@%s: %s", packageName, version, err)
				} else {
					meta.CancelCause(fmt.Errorf("failed to resolve %s@%s: %s", packageName, version, err))
				}
			}
			return
		}
		start := time.Now()
		cachedLocation, err := registry.CachePut(ctx, "npm", resolver)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				if optional {
					logger.Printf("failed to cache %s@%s: %s", packageName, resolver.GetVersion(), err)
				} else {
					meta.CancelCause(fmt.Errorf("failed to cache %s@%s: %s", packageName, resolver.GetVersion(), err))
				}
			}
			return
		}
		logger.Printf("downloaded %s in %s", resolver.String(), time.Since(start))
		select {
		case ch <- ResolvedDependency{constPackageName, cachedLocation}:
		case <-ctx.Done():
		}
	}
}

func (m *Mod) ResolveDependenciesDeep(ctx context.Context, dev bool, optional bool) <-chan ResolvedDependency {
	wg := sync.WaitGroup{}

	ch := make(chan ResolvedDependency, 16)

	for packageName, version := range m.NpmDependencies {
		constPackageName := packageName
		wg.Go(runGo(version, packageName, constPackageName, ctx, ch, m, false))
	}

	if dev {
		for packageName, version := range m.NpmDevDependencies {
			constPackageName := packageName
			wg.Go(runGo(version, packageName, constPackageName, ctx, ch, m, false))
		}
	}

	if optional {
		for packageName, version := range m.NpmOptionalDependencies {
			constPackageName := packageName
			wg.Go(runGo(version, packageName, constPackageName, ctx, ch, m, true))
		}
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
}

func Load(path string) (*json.Document[*Mod], error) {
	modFilePath, err := GetPackageFilePath(path)
	if err != nil {
		return nil, err
	}
	modFile, err := os.Open(modFilePath)
	defer modFile.Close()
	mod := &Mod{}
	jsonEditor, err := json.Parse(modFile, mod)
	mod.fileLocation = modFilePath
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

func GetPackageFilePath(root string) (string, error) {
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
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
	}); err != nil {
		logger.Errorf("failed to walk %s: %s", root, err)
	}
	return subMods
}

func Install(mod *json.Document[*Mod], pack registry.Package, dev bool) error {
	if pack.Source != "npm" {
		return fmt.Errorf("unsupported package source: %s", pack.Source)
	}

	if dev {
		if mod.TypedData.NpmDependencies != nil {
			delete(mod.TypedData.NpmDependencies, pack.PackageName)
		}
		if mod.TypedData.NpmDevDependencies == nil {
			mod.TypedData.NpmDevDependencies = map[string]string{}
		}
		mod.TypedData.NpmDevDependencies[pack.PackageName] = pack.Version
		return nil
	}

	if mod.TypedData.NpmDevDependencies != nil {
		delete(mod.TypedData.NpmDevDependencies, pack.PackageName)
	}
	if mod.TypedData.NpmDependencies == nil {
		mod.TypedData.NpmDependencies = map[string]string{}
	}
	mod.TypedData.NpmDependencies[pack.PackageName] = pack.Version
	return nil
}

func Uninstall(mod *json.Document[*Mod], name string) error {
	doneSomething := false
	if mod.TypedData.NpmDependencies != nil {
		delete(mod.TypedData.NpmDependencies, name)
		doneSomething = true
	}
	if mod.TypedData.NpmDevDependencies != nil {
		delete(mod.TypedData.NpmDevDependencies, name)
		doneSomething = true
	}
	if mod.TypedData.NpmOptionalDependencies != nil {
		delete(mod.TypedData.NpmOptionalDependencies, name)
		doneSomething = true
	}
	if !doneSomething {
		return fmt.Errorf("no such dependency %s", name)
	}
	return nil
}

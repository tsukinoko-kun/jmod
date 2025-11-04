package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/scriptsrunner"
)

func Run(ctx context.Context, root string, ignoreScripts bool) error {
	mods := config.FindSubMods(root)

	var errs []error
	errsMut := sync.Mutex{}

	wg := sync.WaitGroup{}

modsLoop:
	for _, modDoc := range mods {
		select {
		case <-ctx.Done():
			break modsLoop
		default:
		}
		wg.Go(func() {
			mod := modDoc.TypedData
			modRoot := filepath.Dir(mod.GetFileLocation())
			nodeModulesDir := filepath.Join(modRoot, "node_modules")
			_ = nodeModulesDir

			for dependency := range mod.ResolveDependenciesDeep(ctx) {
				if err := link(dependency.CachedLocation, filepath.Join(nodeModulesDir, dependency.PackageName)); err != nil {
					errsMut.Lock()
					errs = append(errs, err)
					errsMut.Unlock()
					return
				}
				// run lifecycle scripts before installing nested dependencies so preinstall can prepare assets
				if !ignoreScripts {
					if err := lifecyclePreinstall(dependency.CachedLocation); err != nil {
						errsMut.Lock()
						errs = append(errs, err)
						errsMut.Unlock()
						return
					}
				}
				// recursive install
				if err := Run(ctx, dependency.CachedLocation, ignoreScripts); err != nil {
					errsMut.Lock()
					errs = append(errs, err)
					errsMut.Unlock()
					return
				}
				// setup executables
				if err := setupBin(dependency.CachedLocation); err != nil {
					errsMut.Lock()
					errs = append(errs, err)
					errsMut.Unlock()
					return
				}
				if !ignoreScripts {
					if err := lifecyclePostinstall(dependency.CachedLocation); err != nil {
						errsMut.Lock()
						errs = append(errs, err)
						errsMut.Unlock()
						return
					}
				}
			}
		})
	}

	wg.Wait()

	return errors.Join(errs...)
}

type packageJson struct {
	Name *string           `json:"name"`
	Bin  map[string]string `json:"bin"`
}

func (p *packageJson) UnmarshalJSON(data []byte) error {
	// intermediate type to capture raw bin bytes
	type rawPkg struct {
		Name *string         `json:"name"`
		Bin  json.RawMessage `json:"bin"`
	}

	var r rawPkg
	if err := json.Unmarshal(data, &r); err != nil {
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
	if err := json.Unmarshal(r.Bin, &m); err == nil {
		p.Bin = m
		return nil
	}

	// Otherwise try as single string
	var s string
	if err := json.Unmarshal(r.Bin, &s); err == nil {
		if p.Name == nil {
			return fmt.Errorf("bin is a string but package name is missing")
		}
		p.Bin = map[string]string{*p.Name: s}
		return nil
	}

	return fmt.Errorf("bin field has an unexpected JSON type")
}

func getPackageJson(root string) (packageJson, error) {
	packageJsonPath := filepath.Join(root, "package.json")
	f, err := os.Open(packageJsonPath)
	if err != nil {
		return packageJson{}, err
	}
	defer f.Close()
	var pj packageJson
	jd := json.NewDecoder(f)
	err = jd.Decode(&pj)
	if err != nil {
		return packageJson{}, fmt.Errorf("decode %s: %w", packageJsonPath, err)
	}
	return pj, nil
}

func setupBin(root string) error {
	pj, err := getPackageJson(root)
	if err != nil {
		return err
	}

	for binName, binRelativePath := range pj.Bin {
		binAbsolutePath := filepath.Join(root, binRelativePath)
		if _, err := os.Stat(binAbsolutePath); err != nil {
			return fmt.Errorf("stat %s for bin %s in mod %s: %w", binAbsolutePath, binName, root, err)
		}
		if err := os.Chmod(binAbsolutePath, 0o755); err != nil {
			return err
		}
		if err := link(binAbsolutePath, filepath.Join(root, "node_modules", ".bin", binName)); err != nil {
			return err
		}
	}

	return nil
}

func lifecyclePreinstall(root string) error {
	return runLifecycleScript(root, "preinstall")
}

func lifecyclePostinstall(root string) error {
	if err := runLifecycleScript(root, "install"); err != nil {
		return err
	}
	return runLifecycleScript(root, "postinstall")
}

func runLifecycleScript(root, scriptName string) error {
	env := map[string]string{
		"npm_lifecycle_event": scriptName,
		"npm_config_target":   root,
		"npm_config_modules":  root,
	}
	if err := scriptsrunner.Run(root, scriptName, nil, env); err != nil {
		if errors.Is(err, scriptsrunner.ErrScriptNotFound) {
			return nil
		}
		return err
	}
	return nil
}

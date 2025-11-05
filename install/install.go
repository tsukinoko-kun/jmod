package install

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/logger"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/registry"
	"github.com/tsukinoko-kun/jmod/scriptsrunner"
	"github.com/tsukinoko-kun/jmod/statusui"
)

var (
	lifecycleScriptsMu   sync.RWMutex
	lifecycleScriptsSeen = make(map[string]struct{})
	installedPackagesMu  sync.RWMutex
	installedPackages    = make(map[string]struct{})
)

func shouldRunLifecycleScript(key string) bool {
	lifecycleScriptsMu.RLock()
	_, exists := lifecycleScriptsSeen[key]
	lifecycleScriptsMu.RUnlock()
	if exists {
		return false
	}
	lifecycleScriptsMu.Lock()
	defer lifecycleScriptsMu.Unlock()
	if _, exists := lifecycleScriptsSeen[key]; exists {
		return false
	}
	lifecycleScriptsSeen[key] = struct{}{}
	return true
}

func lifecycleScriptKey(packageJsonPath string, namePtr, versionPtr *string, scriptName string) string {
	packageDir := filepath.Dir(packageJsonPath)
	if resolved, err := filepath.EvalSymlinks(packageDir); err == nil && resolved != "" {
		packageDir = resolved
	}
	source := "workspace"
	name := ""
	version := ""
	if namePtr != nil {
		name = *namePtr
	}
	if versionPtr != nil {
		version = *versionPtr
	}
	if s, n, v, ok := registry.PackageIdentifierFromPath(packageDir); ok {
		source = s
		if name == "" {
			name = n
		}
		if version == "" {
			version = v
		}
	}
	if name == "" {
		name = filepath.Base(packageDir)
	}
	return fmt.Sprintf("%s:%s@%s#%s", source, name, version, scriptName)
}

func Run(ctx context.Context, root string, ignoreScripts bool, dev bool, optional bool, dependencyChain registry.DependencyChain) {
	mods := config.FindSubMods(root)

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
			dependencyChain := dependencyChain.With(mod.GetFileLocation())

			if !ignoreScripts {
				if err := lifecyclePreinstall(mod.GetFileLocation()); err != nil {
					err = dependencyChain.Err(err)
					if optional {
						logger.Printf("failed to run lifecycle preinstall for %s: %s", mod.GetFileLocation(), err)
					} else {
						meta.CancelCause(err)
					}
					return
				}
			}

			modRoot := filepath.Dir(mod.GetFileLocation())
			nodeModulesDir := filepath.Join(modRoot, "node_modules")
			binDir := filepath.Join(nodeModulesDir, ".bin")

			for dependency := range mod.ResolveDependenciesDeep(ctx, dev, optional, dependencyChain) {
				if err := link(dependency.CachedLocation, filepath.Join(nodeModulesDir, dependency.PackageName)); err != nil {
					err = dependencyChain.Err(err)
					if optional {
						logger.Printf("failed to link %s: %s", dependency.PackageName, err)
					} else {
						meta.CancelCause(fmt.Errorf("failed to link %s: %w", dependency.PackageName, err))
					}
					return
				}
				// recursive install - only if not already processed
				if shouldProcessPackage(dependency.CachedLocation) {
					Run(ctx, dependency.CachedLocation, ignoreScripts, false, optional, dependencyChain)
				}
				select {
				case <-ctx.Done():
					return
				default:
				}
				// setup executables
				if bins, err := config.ResolveBins(ctx, dependency.CachedLocation); err != nil {
					err = dependencyChain.Err(err)
					if optional {
						logger.Printf("failed to resolve bins for %s: %s", dependency.CachedLocation, err)
					} else {
						meta.CancelCause(fmt.Errorf("failed to resolve bins for %s: %w", dependency.CachedLocation, err))
					}
					return
				} else {
					for _, bin := range bins {
						if err := link(bin.BinPath, filepath.Join(binDir, bin.BinName)); err != nil {
							err = dependencyChain.Err(err)
							if optional {
								logger.Printf("failed to link %s: %s", bin.BinName, err)
							} else {
								meta.CancelCause(fmt.Errorf("failed to link %s: %w", bin.BinName, err))
							}
							return
						}
					}
				}
			}

			if !ignoreScripts {
				if err := lifecyclePostinstall(mod.GetFileLocation()); err != nil {
					err = dependencyChain.Err(err)
					if optional {
						logger.Printf("failed to run lifecycle postinstall for %s: %s", mod.GetFileLocation(), err)
					} else {
						meta.CancelCause(err)
					}
					return
				}
			}
		})
	}

	wg.Wait()
}

func shouldProcessPackage(cachedLocation string) bool {
	// Normalize the path - try to resolve symlinks once
	normalized := filepath.Clean(cachedLocation)
	var resolvedClean string
	if resolved, err := filepath.EvalSymlinks(cachedLocation); err == nil {
		resolvedClean = filepath.Clean(resolved)
	}

	// Fast path: check normalized path
	installedPackagesMu.RLock()
	_, exists := installedPackages[normalized]
	if !exists && resolvedClean != "" && resolvedClean != normalized {
		_, exists = installedPackages[resolvedClean]
	}
	installedPackagesMu.RUnlock()
	if exists {
		return false
	}

	// Double-checked locking pattern
	installedPackagesMu.Lock()
	defer installedPackagesMu.Unlock()
	if _, exists := installedPackages[normalized]; exists {
		return false
	}
	if resolvedClean != "" && resolvedClean != normalized {
		if _, exists := installedPackages[resolvedClean]; exists {
			return false
		}
	}
	// Mark both paths as processed to handle symlink cases
	installedPackages[normalized] = struct{}{}
	if resolvedClean != "" && resolvedClean != normalized {
		installedPackages[resolvedClean] = struct{}{}
	}
	return true
}

func lifecyclePreinstall(packageJsonPath string) error {
	return runLifecycleScript(packageJsonPath, "preinstall")
}

func lifecyclePostinstall(packageJsonPath string) error {
	if err := runLifecycleScript(packageJsonPath, "install"); err != nil {
		return err
	}
	return runLifecycleScript(packageJsonPath, "postinstall")
}

func runLifecycleScript(packageJsonPath, scriptName string) error {
	// Get package name for status key
	pj, err := config.GetPackageJsonForLifecycle(packageJsonPath)
	key := lifecycleScriptKey(packageJsonPath, pj.Name, pj.Version, scriptName)
	if !shouldRunLifecycleScript(key) {
		return nil
	}

	statusKey := packageJsonPath
	if err == nil && pj.Name != nil {
		statusKey = fmt.Sprintf("script:%s", pj.Identifier())
	}

	statusui.Set(statusKey, statusui.TextStatus{
		Text: fmt.Sprintf("🔧 Running %s script for %s", scriptName, pj.Identifier()),
	})

	if err := scriptsrunner.Run(packageJsonPath, scriptName, nil, "install"); err != nil {
		if errors.Is(err, scriptsrunner.ErrScriptNotFound) {
			// Clear status if script not found (not an error)
			statusui.Clear(statusKey)
			return nil
		}
		if pj.Name != nil {
			if pj.Version != nil {
				return fmt.Errorf("failed to run %s script for %s@%s: %w", scriptName, *pj.Name, *pj.Version, err)
			}
			return fmt.Errorf("failed to run %s script for %s: %w", scriptName, *pj.Name, err)
		}
		return fmt.Errorf("failed to run %s script: %w", scriptName, err)
	}

	// Success - clear after a moment
	statusui.Set(statusKey, statusui.SuccessStatus{
		Message: fmt.Sprintf("Completed %s script", scriptName),
	})
	go func() {
		time.Sleep(100 * time.Millisecond)
		// program might exit before this sleep is complete
		// does not matter
		statusui.Clear(statusKey)
	}()

	return nil
}

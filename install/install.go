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
	"github.com/tsukinoko-kun/jmod/scriptsrunner"
	"github.com/tsukinoko-kun/jmod/statusui"
)

func Run(ctx context.Context, root string, ignoreScripts bool, dev bool, optional bool) {
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

			if !ignoreScripts {
				if err := lifecyclePreinstall(mod.GetFileLocation()); err != nil {
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

			for dependency := range mod.ResolveDependenciesDeep(ctx, dev, optional) {
				if err := link(dependency.CachedLocation, filepath.Join(nodeModulesDir, dependency.PackageName)); err != nil {
					if optional {
						logger.Printf("failed to link %s: %s", dependency.PackageName, err)
					} else {
						meta.CancelCause(fmt.Errorf("failed to link %s: %w", dependency.PackageName, err))
					}
					return
				}
				// recursive install
				Run(ctx, dependency.CachedLocation, ignoreScripts, false, optional)
				select {
				case <-ctx.Done():
					return
				default:
				}
				// setup executables
				if bins, err := config.ResolveBins(ctx, mod.GetFileLocation()); err != nil {
					if optional {
						logger.Printf("failed to resolve bins for %s: %s", mod.GetFileLocation(), err)
					} else {
						meta.CancelCause(fmt.Errorf("failed to resolve bins for %s: %w", mod.GetFileLocation(), err))
					}
					return
				} else {
					for _, bin := range bins {
						logger.Printf("linking %s -> %s", filepath.Join(binDir, bin.BinName), bin.BinPath)
						if err := link(bin.BinPath, filepath.Join(binDir, bin.BinName)); err != nil {
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

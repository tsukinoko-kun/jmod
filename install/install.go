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

func Run(ctx context.Context, root string, ignoreScripts bool, dev bool) {
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
			modRoot := filepath.Dir(mod.GetFileLocation())
			nodeModulesDir := filepath.Join(modRoot, "node_modules")
			binDir := filepath.Join(nodeModulesDir, ".bin")

			for dependency := range mod.ResolveDependenciesDeep(ctx, dev) {
				if err := link(dependency.CachedLocation, filepath.Join(nodeModulesDir, dependency.PackageName)); err != nil {
					meta.CancelCause(fmt.Errorf("failed to link %s: %w", dependency.PackageName, err))
					return
				}
				// run lifecycle scripts before installing nested dependencies so preinstall can prepare assets
				if !ignoreScripts {
					if err := lifecyclePreinstall(dependency.CachedLocation); err != nil {
						// Error already logged in lifecyclePreinstall
						meta.CancelCause(err)
						return
					}
				}
				// recursive install
				Run(ctx, dependency.CachedLocation, ignoreScripts, false)
				// setup executables
				if bins, err := config.ResolveBins(ctx, dependency.CachedLocation); err != nil {
					meta.CancelCause(fmt.Errorf("failed to resolve bins for %s: %w", mod.GetFileLocation(), err))
					return
				} else {
					for _, bin := range bins {
						logger.Printf("linking %s -> %s", filepath.Join(binDir, bin.BinName), bin.BinPath)
						if err := link(bin.BinPath, filepath.Join(binDir, bin.BinName)); err != nil {
							meta.CancelCause(fmt.Errorf("failed to link %s: %w", bin.BinName, err))
							return
						}
					}
				}
				// run lifecycle scripts after installing nested dependencies so postinstall can prepare assets
				if !ignoreScripts {
					if err := lifecyclePostinstall(dependency.CachedLocation); err != nil {
						// Error already logged in lifecyclePostinstall
						meta.CancelCause(err)
						return
					}
				}
			}
		})
	}

	wg.Wait()
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
	// Get package name for status key
	pj, err := config.GetPackageJsonForLifecycle(root)
	statusKey := root
	if err == nil && pj.Name != nil {
		if pj.Version != nil {
			statusKey = fmt.Sprintf("script:%s@%s", *pj.Name, *pj.Version)
		} else {
			statusKey = fmt.Sprintf("script:%s", *pj.Name)
		}
	}

	statusui.Set(statusKey, statusui.TextStatus{
		Text: fmt.Sprintf("🔧 Running %s script", scriptName),
	})

	env := map[string]string{
		"npm_lifecycle_event": scriptName,
		"npm_config_target":   root,
		"npm_config_modules":  root,
	}
	if err := scriptsrunner.Run(root, scriptName, nil, env); err != nil {
		if errors.Is(err, scriptsrunner.ErrScriptNotFound) {
			// Clear status if script not found (not an error)
			statusui.Clear(statusKey)
			return nil
		}
		if pj.Name != nil {
			if pj.Version != nil {
				return fmt.Errorf("Failed to run %s script for %s@%s: %w", scriptName, *pj.Name, *pj.Version, err)
			}
			return fmt.Errorf("Failed to run %s script for %s: %w", scriptName, *pj.Name, err)
		}
		return fmt.Errorf("Failed to run %s script: %w", scriptName, err)
	}

	// Success - clear after a moment
	statusui.Set(statusKey, statusui.SuccessStatus{
		Message: fmt.Sprintf("Completed %s script", scriptName),
	})
	go func() {
		time.Sleep(100 * time.Millisecond)
		statusui.Clear(statusKey)
	}()

	return nil
}

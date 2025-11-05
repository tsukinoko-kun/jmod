package tidy

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/ignore"
	"github.com/tsukinoko-kun/jmod/logger"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/registry"
	"github.com/tsukinoko-kun/jmod/utils"
)

type Parser interface {
	ParseImports(path string) ([]string, error)
}

var parsers = map[string]Parser{}

func Run(root string) error {
	ignoreMatcher := ignore.GetIgnoreMatcher(root)

	mods := config.FindSubMods(root)

	var ignoreModDirs []string
	for _, modDoc := range mods {
		mod := modDoc.TypedData
		ignoreModDirs = append(ignoreModDirs, filepath.Dir(mod.GetFileLocation()))
	}

	for _, modDoc := range mods {
		mod := modDoc.TypedData
		root := filepath.Dir(mod.GetFileLocation())
		modImports := make(map[string]bool)
		var mu sync.Mutex
		var wg sync.WaitGroup
		errChan := make(chan error, 1)

		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			isDir := d.IsDir()
			gitPath := strings.Split(
				strings.TrimPrefix(path, root),
				string(filepath.Separator),
			)
			if len(gitPath) != 0 && gitPath[0] == "" {
				gitPath = gitPath[1:]
			}
			if ignoreMatcher.Match(gitPath, isDir) {
				if isDir {
					return filepath.SkipDir
				}
				return nil
			}

			if isDir {
				if path != root && slices.Contains(ignoreModDirs, path) {
					return filepath.SkipDir
				}
				return nil
			}

			ext := filepath.Ext(path)
			if parser, ok := parsers[ext]; ok {
				wg.Add(1)
				go func(path string, parser Parser) {
					defer wg.Done()
					imports, err := parser.ParseImports(path)
					if err != nil {
						select {
						case errChan <- err:
						default:
						}
						return
					}
					for _, imp := range imports {
						if !strings.HasPrefix(imp, "node:") {
							transformed := transformForNpm(imp)
							mu.Lock()
							modImports[transformed] = true
							mu.Unlock()
						}
					}
				}(path, parser)
			} else {
				logger.Printf("no parser for %s", ext)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to walk directory: %w", err)
		}

		wg.Wait()
		close(errChan)

		if err := <-errChan; err != nil {
			return fmt.Errorf("failed to parse imports: %w", err)
		}

		if len(modImports) == 0 {
			continue
		}

		logger.Printf("found %d imports for %s", len(modImports), utils.Must(filepath.Rel(meta.Pwd(), mod.GetFileLocation())))

		if mod.NpmDependencies == nil {
			mod.NpmDependencies = make(map[string]string)
		}

		for imp := range modImports {
			if slices.Contains(ignoredPackages, imp) {
				continue
			}

			// check if the package is already a dependency
			if _, included := mod.NpmDevDependencies[imp]; included {
				continue
			}
			if _, included := mod.NpmDependencies[imp]; !included {
				latestVersion, err := registry.Npm_GetLatestVersion(imp)
				if err != nil {
					logger.Printf("failed to get latest version for %s: %s", imp, err)
					continue
				}
				mod.NpmDependencies[imp] = latestVersion
				logger.Printf("  new package %s (version %s)", imp, latestVersion)
			}
		}

		if err := config.Write(modDoc); err != nil {
			return fmt.Errorf("failed to save mod file: %w", err)
		}
	}

	return nil
}

var ignoredPackages = []string{
	"server-only",
}

func transformForNpm(importString string) string {
	// @scope/package
	if strings.HasPrefix(importString, "@") {
		parts := strings.Split(importString, "/")
		if len(parts) >= 2 {
			return fmt.Sprintf("%s/%s", parts[0], parts[1])
		}
		// this should never happen
		return importString
	}

	// package
	slashIndex := strings.Index(importString, "/")
	if slashIndex == -1 {
		return importString
	}
	return importString[:slashIndex]
}

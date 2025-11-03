package ignore

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

var ignoreDirs = []string{
	".git",
	"node_modules",
	"vendor",
	".idea",
	".next",
	".open-next",
	".github",
	".wrangler",
}

var ignorePaths = []string{
	"*.json",
	"*.json5",
	"*.jsonc",
	"*.yaml",
	"*.yml",
	"*.md",
	"*.toml",
	"*.lock",
	"*.svg",
	"*.ico",
	"*.env",
	"*.env.*",
	".npmrc",
	".prettierignore",
	".prettierrc",
	".npmrc",
	".vars",
}

var ignoreMatchers = map[string]gitignore.Matcher{}

func GetIgnoreMatcher(path string) gitignore.Matcher {
	matcher, ok := ignoreMatchers[path]
	if ok {
		return matcher
	}

	matcher, err := getIgnoreMatcher(path)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return nil
	}
	ignoreMatchers[path] = matcher

	return matcher
}

func getIgnoreMatcher(path string) (gitignore.Matcher, error) {
	var patterns []gitignore.Pattern
	for _, dir := range ignoreDirs {
		patterns = append(patterns, gitignore.ParsePattern(dir+"/", nil))
	}
	for _, path := range ignorePaths {
		patterns = append(patterns, gitignore.ParsePattern(path, nil))
	}

	if err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(ignoreDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".gitignore" {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			ignoreBytes, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			ignoreStr := string(ignoreBytes)
			for line := range strings.Lines(ignoreStr) {
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return gitignore.NewMatcher(patterns), nil
}

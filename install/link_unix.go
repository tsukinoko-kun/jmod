//go:build !windows

package install

import (
	"os"
	"path/filepath"
)

func link(original, link string) error {
	linkMut.Lock()
	defer linkMut.Unlock()

	// Check if symlink already exists and points to the correct target
	if stat, err := os.Lstat(link); err == nil {
		if stat.Mode()&os.ModeSymlink != 0 {
			// Check if it already points to the right target
			if target, err := os.Readlink(link); err == nil {
				// Normalize both paths for comparison
				originalClean := filepath.Clean(original)
				targetClean := filepath.Clean(target)
				if originalClean == targetClean {
					return nil // Already linked correctly
				}
			}
			// Wrong target, remove it
			if err := os.Remove(link); err != nil {
				return err
			}
		} else if stat.IsDir() {
			if err := os.RemoveAll(link); err != nil {
				return err
			}
		} else {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(original, link)
}

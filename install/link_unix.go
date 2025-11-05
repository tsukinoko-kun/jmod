//go:build !windows

package install

import (
	"os"
	"path/filepath"
)

func link(original, link string) error {
	linkMut.Lock()
	defer linkMut.Unlock()

	if stat, err := os.Lstat(link); err == nil {
		if stat.Mode()&os.ModeSymlink != 0 {
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

//go:build !windows

package install

import (
	"os"
	"path/filepath"
)

func link(original, link string) error {
	if stat, err := os.Stat(link); err == nil {
		if stat.IsDir() {
			if err := os.RemoveAll(link); err != nil {
				return err
			}
		} else {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
	} else {
		_ = os.MkdirAll(filepath.Dir(link), 0o755)
	}
	return os.Symlink(original, link)
}

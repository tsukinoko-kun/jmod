package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

	var errs []error

	// Try junction first (no privilege needed, local paths)
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, original)
	if out, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else {
		errs = append(errs, fmt.Errorf("mklink: %s: %w", out, err))
	}

	// Fallback to symlink (may require Developer Mode/Admin)
	if err := os.Symlink(original, link); err == nil {
		return nil
	} else {
		errs = append(errs, fmt.Errorf("symlink: %w", err))
	}

	return errors.Join(errs...)
}

//go:build !windows

package utils

import (
	"fmt"
	"os"
)

func EnsureExecutable(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if stat.Mode().Perm()&0111 == 0 {
		if err := os.Chmod(path, stat.Mode().Perm()|0111); err != nil {
			return fmt.Errorf("failed to make %s executable: %w", path, err)
		}
	}
	return nil
}

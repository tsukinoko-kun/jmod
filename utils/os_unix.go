//go:build !windows

package utils

import (
	"fmt"
	"os"
)

// On Unix-like systems, this function adds the executable flag to the file without changing any other permissions.
// EnsureExecutable is a no-op on Windows because Windows doesn't have the concept of an executable flag.
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

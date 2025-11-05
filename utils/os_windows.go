package utils

// EnsureExecutable is a no-op on Windows because Windows doesn't have the concept of an executable flag.
// On Unix-like systems, this function adds the executable flag to the file without changing any other permissions.
func EnsureExecutable(path string) error {
	return nil
}

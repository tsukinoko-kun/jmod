//go:build !windows

package scriptsrunner

import (
	"os"
	"os/exec"
)

var defaultShell string

func getDefaultShell() string {
	if defaultShell != "" {
		return defaultShell
	}

	if shell, ok := os.LookupEnv("SHELL"); ok {
		defaultShell = shell
		return shell
	}
	if sh, err := exec.LookPath("sh"); err == nil {
		defaultShell = sh
		return sh
	}

	defaultShell = "/bin/sh"
	return defaultShell
}

func runShell(root, script string, args []string, env []string) error {
	sh := getDefaultShell()

	// Run: sh -c '<script> "$@"' _ <args...>
	// "$@" expands to each arg as a separate word, preserving spaces and
	// special characters. This is POSIX sh-safe.
	argv := append([]string{"-c", script + " \"$@\"", "_"}, args...)
	cmd := exec.Command(sh, argv...)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

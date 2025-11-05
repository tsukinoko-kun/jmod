//go:build !windows

package scriptsrunner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tsukinoko-kun/jmod/logger"
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

	// Capture output and log errors
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%s: %w", string(out), err)
	}
	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 0 {
		logger.Printf("%s $ %s\n%s", root, script, outStr)
	} else {
		logger.Printf("%s $ %s\nno output", root, script)
	}
	return err
}

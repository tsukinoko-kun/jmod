package scriptsrunner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/logger"
)

var jsExts = []string{".js", ".mjs", ".cjs", ".ts", ".mts", ".cts"}

var ErrScriptNotFound = errors.New("script not found")

func Run(packageJsonPath string, scriptName string, args []string, command string) error {
	root := filepath.Dir(packageJsonPath)
	// combine env with the current process env
	completeEnv := make([]string, 0, len(getDefaultEnv()))
	completeEnv = append(completeEnv, getDefaultEnv()...)
	for i, e := range completeEnv {
		if v, ok := strings.CutPrefix(e, "PATH="); ok {
			completeEnv[i] = "PATH=" + filepath.Join(root, "node_modules", ".bin") + string(filepath.ListSeparator) + v
		}
	}
	completeEnv = append(completeEnv, "npm_lifecycle_event="+scriptName)
	completeEnv = append(completeEnv, "npm_package_json="+packageJsonPath)
	completeEnv = append(completeEnv, "npm_command="+command)

	pj, err := config.GetPackageJsonForLifecycle(packageJsonPath)
	if err != nil {
		return err
	}
	if pj.Scripts == nil {
		return fmt.Errorf("%w: %s", ErrScriptNotFound, scriptName)
	}
	scripts := *pj.Scripts

	script, ok := scripts[scriptName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrScriptNotFound, scriptName)
	}

	completeEnv = append(completeEnv, fmt.Sprintf("npm_lifecycle_script=%s", script))

	// check if the script is a path to a js file
	if slices.Contains(jsExts, filepath.Ext(script)) {
		if _, err := os.Stat(script); err == nil {
			return runJsScript(root, scriptName, args, completeEnv)
		}
	}

	// shell out
	return runShell(root, script, args, completeEnv)
}

var defaultJsRunner string

func getDefaultJsRunner() string {
	if bun, err := exec.LookPath("bun"); err == nil {
		defaultJsRunner = bun
		return defaultJsRunner
	}
	if node, err := exec.LookPath("node"); err == nil {
		defaultJsRunner = node
		return defaultJsRunner
	}
	panic("no js runner found")
}

func runJsScript(root string, scriptName string, args []string, env []string) error {
	arg := append([]string{scriptName}, args...)
	cmd := exec.Command(getDefaultJsRunner(), arg...)
	cmd.Dir = root
	cmd.Env = env

	// Capture output and log errors
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%s: %w", string(out), err)
		}
		return err
	}
	if len(out) > 0 {
		logger.Printf("%s $ %s\n%s", root, scriptName, string(out))
	}
	return nil
}

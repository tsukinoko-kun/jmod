package scriptsrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/tsukinoko-kun/jmod/logger"
)

type packageJson struct {
	Scripts map[string]string `json:"scripts"`
}

func getPackageJson(root string) (packageJson, error) {
	packageJsonPath := filepath.Join(root, "package.json")
	f, err := os.Open(packageJsonPath)
	if err != nil {
		return packageJson{}, err
	}
	defer f.Close()
	var pj packageJson
	jd := json.NewDecoder(f)
	err = jd.Decode(&pj)
	if err != nil {
		return packageJson{}, err
	}
	return pj, nil
}

var jsExts = []string{".js", ".mjs", ".cjs", ".ts", ".mts", ".cts"}

var ErrScriptNotFound = errors.New("script not found")

func Run(root string, scriptName string, args []string, env map[string]string) error {
	// combine env with the current process env
	completeEnv := os.Environ()
	completeEnv = append(completeEnv, getDefaultEnv()...)
	for k, v := range env {
		completeEnv = append(completeEnv, fmt.Sprintf("%s=%s", k, v))
	}

	pj, err := getPackageJson(root)
	if err != nil {
		return err
	}

	script, ok := pj.Scripts[scriptName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrScriptNotFound, scriptName)
	}

	if env != nil {
		if _, ok := env["npm_lifecycle_event"]; ok {
			completeEnv = append(completeEnv, fmt.Sprintf("npm_lifecycle_script=%s", script))
		}
	}

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
	if logger.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else {
		return errors.Join(errors.New(string(out)), err)
	}
}

package scriptsrunner

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
)

func npmConfigProduction() string {
	if slices.Contains(os.Args, "--production") {
		return "true"
	}
	return "false"
}

func npmConfigSafe() string {
	if slices.Contains(os.Args, "--safe") {
		return "true"
	}
	return "false"
}

func nodeVersion() string {
	cmd := exec.Command("node", "--version")
	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(out))
}

var defaultEnv []string = nil

func getDefaultEnv() []string {
	if defaultEnv != nil {
		return defaultEnv
	}

	execpath, err := os.Executable()
	if err != nil {
		execpath = os.Args[0]
	}

	defaultEnv = append(
		os.Environ(),
		"npm_execpath="+execpath,
		"npm_config_verify_deps_before_run=false",
		"npm_config_frozen_lockfile=",
		"npm_config_global=false",
		"npm_config_production="+npmConfigProduction(),
		"npm_config_save="+npmConfigSafe(),
		"npm_config_registry=https://registry.npmjs.org/",
		"npm_config__jsr_registry=https://npm.jsr.io/",
		"NODE_ENV=production",
		"NODE_VERSION="+nodeVersion(),
		"npm_config_arch="+arch,
		"npm_config_platform="+runtime.GOOS,
		"npm_config_tmp="+os.TempDir(),
		"npm_node_execpath="+getDefaultJsRunner(),
	)

	nodeGyp, err := exec.LookPath("node-gyp")
	if err == nil {
		defaultEnv = append(defaultEnv, "npm_config_node_gyp="+nodeGyp)
	}

	nodeVersion := "?"
	if node, err := exec.LookPath("node"); err == nil {
		if out, err := exec.Command(node, "--version").Output(); err == nil {
			nodeVersion = strings.TrimSpace(string(out))
		}
	}
	defaultEnv = append(defaultEnv, fmt.Sprintf("npm_config_user_agent=npm/? node/%s %s %s", nodeVersion, runtime.GOOS, arch))

	return defaultEnv
}

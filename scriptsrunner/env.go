package scriptsrunner

import (
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

	defaultEnv = []string{
		"npm_config_prefix=",
		"npm_config_global=false",
		"npm_config_production=" + npmConfigProduction(),
		"npm_config_save=" + npmConfigSafe(),
		"npm_config_registry=https://registry.npmjs.org/",
		"NODE_ENV=production",
		"NODE_VERSION=" + nodeVersion(),
		"npm_config_arch=" + arch,
		"npm_config_platform=" + runtime.GOOS,
		"npm_config_tmp=" + os.TempDir(),
	}
	return defaultEnv
}

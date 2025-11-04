package scriptsrunner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type shellRunner func(root, script string, args []string, env []string) error

var defaultRunner shellRunner

func pickRunner() (shellRunner, error) {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return func(root, script string, args []string, env []string) error {
			// pwsh -Command '& { <script> } @args; exit $LASTEXITCODE' <args...>
			argv := []string{
				"-NoProfile",
				"-NonInteractive",
				"-Command",
				fmt.Sprintf("& { %s } @args; exit $LASTEXITCODE", script),
			}
			argv = append(argv, args...)

			cmd := exec.Command(p, argv...)
			cmd.Dir = root
			cmd.Env = env
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}, nil
	}

	if p, err := exec.LookPath("powershell"); err == nil {
		return func(root, script string, args []string, env []string) error {
			// powershell -Command '& { <script> } @args; exit $LASTEXITCODE' <args...>
			argv := []string{
				"-NoProfile",
				"-NonInteractive",
				"-Command",
				fmt.Sprintf("& { %s } @args; exit $LASTEXITCODE", script),
			}
			argv = append(argv, args...)

			cmd := exec.Command(p, argv...)
			cmd.Dir = root
			cmd.Env = env
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}, nil
	}

	if p, err := exec.LookPath("cmd"); err == nil {
		return func(root, script string, args []string, env []string) error {
			// For cmd there’s no "$@" equivalent on the command line.
			// Use a temporary .cmd wrapper and %* inside it.
			f, err := os.CreateTemp("", "jmod-run-*.cmd")
			if err != nil {
				return err
			}
			path := f.Name()
			// CRLF for .cmd files, append %* at the end.
			content := "@echo off\r\n" + script + " %*\r\n"
			if _, err := f.WriteString(content); err != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return err
			}
			_ = f.Close()
			defer os.Remove(path)

			argv := append([]string{"/d", "/c", path}, args...)
			cmd := exec.Command(p, argv...)
			cmd.Dir = root
			cmd.Env = env
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}, nil
	}

	return nil, errors.New("no shell found")
}

func runShell(root string, script string, args []string, env []string) error {
	if defaultRunner == nil {
		r, err := pickRunner()
		if err != nil {
			return err
		}
		defaultRunner = r
	}
	return defaultRunner(root, script, args, env)
}

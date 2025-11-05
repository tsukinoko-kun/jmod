package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/scriptsrunner"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a script from the package.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("script name required")
		}

		mod := cmd.Flag("mod").Value.String()
		packageJsonPath, err := config.GetPackageFilePath(filepath.Join(meta.Pwd(), mod))
		if err != nil {
			return err
		}

		return scriptsrunner.Run(packageJsonPath, args[0], args[1:], "run")
	},
}

func init() {
	runCmd.Flags().String("mod", ".", "module to add the dependency to")
	rootCmd.AddCommand(runCmd)
}

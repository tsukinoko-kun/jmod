package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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
		return scriptsrunner.Run(meta.Pwd(), args[0], args[1:], nil)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}

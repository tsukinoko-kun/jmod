package cmd

import (
	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/tidy"
)

var tidyCmd = &cobra.Command{
	Use:   "tidy",
	Short: "A brief description of your command",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := tidy.Run(meta.Pwd()); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tidyCmd)
}

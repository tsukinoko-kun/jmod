package cmd

import (
	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/logger"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new jmod project",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := config.New()
		if err != nil {
			return err
		}
		logger.Printf("jmod project initialized")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

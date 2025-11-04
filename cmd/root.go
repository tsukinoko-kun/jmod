package cmd

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/logger"
)

var rootCmd = &cobra.Command{
	DisableAutoGenTag: true,
	SilenceUsage:      true,
	Use:               "jmod",
	Short:             "The actually good package manager for JavaScript because JS devs are insane",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.Verbose = flagVerbose
	},
}

func Execute(ctx context.Context) {
	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		os.Exit(1)
	}
}

var flagVerbose bool

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
}

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/install"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/utils"
)

var installCmd = &cobra.Command{
	Use: "install",
	Aliases: []string{
		"i",
		"get",
		"pull",
	},
	Short: "Install dependencies from package.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		go func() {
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
			<-c
			cancel()
		}()
		return install.Run(ctx, meta.Pwd(), utils.Must(cmd.Flags().GetBool("ignore-scripts")))
	},
}

func init() {
	installCmd.Flags().Bool("ignore-scripts", false, "Ignore scripts in package.json")
	rootCmd.AddCommand(installCmd)
}

package cmd

import (
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
		return install.Run(cmd.Context(), meta.Pwd(), utils.Must(cmd.Flags().GetBool("ignore-scripts")))
	},
}

func init() {
	installCmd.Flags().Bool("ignore-scripts", false, "Ignore scripts in package.json")
	rootCmd.AddCommand(installCmd)
}

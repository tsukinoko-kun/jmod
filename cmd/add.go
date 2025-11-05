package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/install"
	"github.com/tsukinoko-kun/jmod/logger"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/registry"
	"github.com/tsukinoko-kun/jmod/statusui"
	"github.com/tsukinoko-kun/jmod/utils"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new dependency",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := statusui.Start(); err != nil {
			return err
		}
		defer statusui.Stop()

		// min 1 positional arg
		if len(args) < 1 {
			return cmd.Help()
		}

		ctx := cmd.Context()

		mod := cmd.Flag("mod").Value.String()
		c, err := config.Load(filepath.Join(meta.Pwd(), mod))
		if err != nil {
			return err
		}

		for _, arg := range args {
			pack, err := registry.FindInstallablePackage(arg)
			if err != nil {
				return err
			}
			config.Install(c, pack, utils.Must(cmd.Flags().GetBool("dev")))
			logger.Printf("added %s package %s version %s", pack.Source, pack.PackageName, pack.Version)
		}
		if err := config.Write(c); err != nil {
			return err
		}

		install.Run(ctx, meta.Pwd(), utils.Must(cmd.Flags().GetBool("ignore-scripts")), true)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().String("mod", ".", "module to add the dependency to")
	addCmd.Flags().BoolP("dev", "D", false, "add as a dev dependency")
	addCmd.Flags().Bool("ignore-scripts", false, "Ignore scripts in package.json")
}

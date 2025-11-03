package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/logger"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/registry"
)

var addCmd = &cobra.Command{
	Use: "add",
	Aliases: []string{
		"install"},
	Short: "Add a new dependency",
	RunE: func(cmd *cobra.Command, args []string) error {
		// min 1 positional arg
		if len(args) < 1 {
			return cmd.Help()
		}

		mod := cmd.Flag("mod").Value.String()
		c, err := config.Load(filepath.Join(meta.Pwd(), mod))
		if err != nil {
			return err
		}

		for _, arg := range args {
			pack, err := registry.Resolve(arg)
			if err != nil {
				return err
			}
			config.Install(c, pack)
			logger.Printf("added %s package %s version %s\n", pack.Source, pack.PackageName, pack.Version)
		}
		if err := config.Write(c); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().String("mod", ".", "module to add the dependency to")
}

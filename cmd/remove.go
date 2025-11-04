package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/meta"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a dependency",
	Aliases: []string{
		"rm",
		"uninstall",
	},
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
			if err := config.Uninstall(c, arg); err != nil {
				return fmt.Errorf("uninstall %s: %w", arg, err)
			}
		}
		if err := config.Write(c); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
	removeCmd.Flags().String("mod", ".", "module to remove the dependency from")
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tsukinoko-kun/jmod/config"
	"github.com/tsukinoko-kun/jmod/meta"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all dependencies",
	RunE: func(cmd *cobra.Command, args []string) error {
		mods := config.FindSubMods(meta.Pwd())
		for i, mod := range mods {
			if i > 0 {
				fmt.Println()
			}
			fmt.Println("module:", mod.TypedData.GetFileLocation())
			for dep, version := range mod.TypedData.NpmAutoDependencies {
				fmt.Printf("  %s@%s\n", dep, version)
			}
			for dep, version := range mod.TypedData.NpmManualDependencies {
				fmt.Printf("  %s@%s\n", dep, version)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}

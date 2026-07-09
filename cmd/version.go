package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the aegiskeys version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(binaryName(), version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

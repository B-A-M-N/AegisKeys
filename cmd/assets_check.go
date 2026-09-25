package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/logo"
)

var assetsCheckCmd = &cobra.Command{
	Use:    "assets-check",
	Short:  "Validate embedded animation assets",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := logo.ValidateDefaultAssets(); err != nil {
			return err
		}
		for id := range logo.DefaultAssets {
			if _, ok := logo.LoadDefaultMask(id); !ok {
				return fmt.Errorf("embedded logo mask unavailable: %s", id)
			}
		}
		fmt.Printf("validated %d publication-safe embedded asset(s); %d app identities resolved (generic fallback allowed)\n", logo.PublicationAssetCount(), len(logo.DefaultAssets))
		return nil
	},
}

func init() { rootCmd.AddCommand(assetsCheckCmd) }

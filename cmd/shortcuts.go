package cmd

import (
	"github.com/spf13/cobra"
)

// Root-level shortcuts for common commands. These let users type
// `ak profiles` instead of `ak profile list`, `ak keys` instead of
// `ak key list`, etc. The long forms still work.

var profilesCmd = &cobra.Command{
	Use:     "profiles",
	Aliases: []string{"p"},
	Short:   "List profiles (see 'profile list')",
	Hidden:  true,
	Args:    cobra.NoArgs,
	RunE:    profileListCmd.RunE,
}

var keysCmd = &cobra.Command{
	Use:     "keys",
	Aliases: []string{"k"},
	Short:   "List vault items (see 'key list')",
	Hidden:  true,
	Args:    cobra.NoArgs,
	RunE:    keyListCmd.RunE,
}

func init() {
	rootCmd.AddCommand(profilesCmd, keysCmd)
}

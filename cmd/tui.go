package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive terminal UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(cmd)
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// runTUI launches the interactive TUI. It is also the default action when
// aegiskeys is invoked with no subcommand (see root.go).
func runTUI(_ *cobra.Command) error {
	if err := requireInitialized(); err != nil {
		return err
	}
	// The TUI loads provider/profile/vault state itself; see internal/tui.
	if err := tui.Run(resolvedConfigDir(), version); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

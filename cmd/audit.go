package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
)

var auditCount int

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Show recent audit log events (metadata only, never secrets)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		logger := audit.NewLogger(config.AuditPath(resolvedConfigDir()))
		events, err := logger.Tail(auditCount)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			fmt.Println("No audit events recorded yet.")
			return nil
		}
		for _, e := range events {
			line := e.Time.Format("2006-01-02 15:04:05") + "  " + e.Event
			if e.Profile != "" {
				line += "  profile=" + e.Profile
			}
			if e.Provider != "" {
				line += "  provider=" + e.Provider
			}
			if e.Command != "" {
				line += "  command=" + e.Command
			}
			fmt.Println(line)
		}
		return nil
	},
}

func init() {
	auditCmd.Flags().IntVarP(&auditCount, "count", "n", 50, "number of recent events to show")
	rootCmd.AddCommand(auditCmd)
}

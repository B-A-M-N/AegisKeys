package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

var handoffProfile string

// handoffCmd implements "aegiskeys handoff --profile <name>" — the safe
// credential handoff mode for GUI apps (Zed, IntelliJ, Roo, Kilo, Cursor)
// where AegisKeys cannot inject secrets directly. It shows the user exactly
// what they need to do and what can be automated, without leaking secrets.
var handoffCmd = &cobra.Command{
	Use:   "handoff --profile <name>",
	Short: "Manual credential handoff for apps that cannot be launched directly",
	Long: "For IDEs and GUI apps (Zed, IntelliJ, Roo, Kilo, Cursor), AegisKeys " +
		"cannot inject secrets as environment variables. This command shows the " +
		"exact steps to configure the app manually, and offers a controlled " +
		"copy-to-clipboard under the key's secret policy.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if handoffProfile == "" {
			return fmt.Errorf("--profile is required")
		}
		reg, store, err := loadStores()
		if err != nil {
			return err
		}
		prof := store.Find(handoffProfile)
		if prof == nil {
			return fmt.Errorf("no profile named %q", handoffProfile)
		}
		prov := reg.Find(prof.ProviderSlug)
		if prov == nil {
			return fmt.Errorf("profile %q references missing provider %q", prof.Name, prof.ProviderSlug)
		}

		// Resolve the launch strategy to find what would be written.
		var key *secret.SecretRecord
		v, err := loadVault()
		if err == nil && v != nil && prof.KeyID != "" {
			key = v.Get(prof.KeyID)
		}
		adapt := adapter.NewRegistry()
		strategy, resolveErr := adapter.ResolveLaunchStrategyCatalog(*prof, *prov, key, adapt, reg, v, adapter.ResolvePreview)

		fmt.Printf("=== AegisKeys Handoff: %s ===\n", prof.Name)
		fmt.Printf("App:      %s\n", prof.TargetApp())
		fmt.Printf("Provider: %s\n", prov.Name)
		fmt.Printf("Env var:  %s\n\n", prov.CanonicalEnvVar())

		if resolveErr != nil {
			fmt.Printf("Warning: profile cannot be launched automatically: %v\n", resolveErr)
		}

		if strategy != nil && len(strategy.Plan.Files) > 0 {
			fmt.Println("Config files that AegisKeys can write for you:")
			for _, f := range strategy.Plan.Files {
				fmt.Printf("  → %s (merge=%s, backup=%s)\n", f.Path, f.MergePolicy, f.BackupPolicy)
			}
			fmt.Println()
		}

		if prov.CanonicalEnvVar() != "" {
			fmt.Println("Manual step required:")
			fmt.Printf("  Set the environment variable in your IDE/terminal:\n")
			fmt.Printf("    %s=<paste-key-here>\n\n", prov.CanonicalEnvVar())
		}

		if key != nil && key.ID != "" {
			// Offer copy-to-clipboard under policy + TTL.
			if err := key.AllowAccess(secret.AccessCopyClipboard); err != nil {
				fmt.Printf("Copy to clipboard: DISABLED by key policy (%v)\n", err)
			} else {
				fmt.Println("Copy to clipboard: available (policy allows)")
				if key.Policy.MaxClipboardTTLSeconds > 0 {
					fmt.Printf("  Clipboard TTL: %ds\n", key.Policy.MaxClipboardTTLSeconds)
				}
				fmt.Println("  Run `aegiskeys vault copy --id " + key.ID + "` to copy if clipboard support is available.")
			}
			fmt.Println()
		}

		if strategy != nil && len(strategy.ManualSteps) > 0 {
			fmt.Println("Manual steps from the adapter:")
			for i, step := range strategy.ManualSteps {
				fmt.Printf("  %d. %s\n", i+1, step.Title)
				if step.Description != "" {
					fmt.Printf("     %s\n", step.Description)
				}
			}
			fmt.Println()
		}

		if strategy != nil && len(strategy.Hazards) > 0 {
			fmt.Println("Warnings:")
			for _, h := range strategy.Hazards {
				fmt.Printf("  [%s] %s\n", h.Severity, h.Title)
				if h.Fix != "" {
					fmt.Printf("    fix: %s\n", h.Fix)
				}
			}
			fmt.Println()
		}

		// Write audit metadata only — never the secret value.
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "manual_handoff_started",
			Profile:  prof.Name,
			Provider: prof.ProviderSlug,
		})

		fmt.Println("After finishing, run `aegiskeys doctor` to verify your setup.")
		return nil
	},
}

func init() {
	handoffCmd.Flags().StringVarP(&handoffProfile, "profile", "p", "", "profile name or alias (required)")
	rootCmd.AddCommand(handoffCmd)
}

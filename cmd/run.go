package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/runner"
)

var runProfile string

var runCmd = &cobra.Command{
	Use:   "run --profile <name> -- <command> [args...]",
	Short: "Run a command with the profile's secrets injected into its environment",
	Long: "The command after `--` receives the resolved environment variables " +
		"in its child process only. The parent shell is never modified.\n\n" +
		"Note: The command is executed directly, not via a shell. Shell built-ins\n" +
		"(like 'export' or 'cd') and operators (like '|' or '>') will fail unless\n" +
		"explicitly wrapped in 'sh -c', e.g.:\n" +
		"  aegiskeys run --profile xyz -- sh -c 'printenv | grep KEY'",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName, err := effectiveProfileName(runProfile)
		if err != nil {
			return err
		}
		// Prompt once; reuse the password for load + save.
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		reg, store, err := loadStores()
		if err != nil {
			return err
		}
		prof := store.Find(profileName)
		if prof == nil {
			return fmt.Errorf("no profile named %q", profileName)
		}
		prov := reg.Find(prof.ProviderSlug)
		if prov == nil {
			return fmt.Errorf("profile %q references missing provider %q", prof.Name, prof.ProviderSlug)
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		rec := v.Get(prof.KeyID)
		if rec == nil {
			return fmt.Errorf("profile %q references missing key %q", prof.Name, prof.KeyID)
		}

		// Use the adapter system to resolve the launch strategy.
		// Pass the full record (incl. its access Policy) so enforcement in
		// buildBaseEnv honors the key's launch-injection policy instead of
		// rejecting every key on a zero-value policy.
		adapterReg := adapter.NewRegistry()
		strategy, err := adapter.ResolveLaunchStrategy(*prof, *prov, rec, adapterReg)
		if err != nil {
			return err
		}

		// Show hazards before launching.
		if len(strategy.Hazards) > 0 {
			fmt.Println("Warnings:")
			for _, h := range strategy.Hazards {
				fmt.Printf("  [%s] %s\n", h.Severity, h.Title)
				if h.Fix != "" {
					fmt.Printf("    fix: %s\n", h.Fix)
				}
			}
		}

		// Mark used and persist vault (timestamps only; secret unchanged).
		v.Touch(prof.KeyID)
		if err := saveVault(pw, v); err != nil {
			return err
		}

		// The resolved command is the child binary. The user's tokens after
		// `--` are the child's arguments. Normalize so the binary name is
		// never duplicated into argv:
		//   - command unset → use args[0] as the binary, pass args[1:].
		//   - args[0] repeats the command → drop it, pass args[1:].
		//   - otherwise → pass the whole args slice as child arguments.
		extraArgs := args
		switch {
		case strategy.Plan.Command == "" && len(args) > 0:
			strategy.Plan.Command = args[0]
			extraArgs = args[1:]
		case len(args) > 0 && args[0] == strategy.Plan.Command:
			extraArgs = args[1:]
		}

		// Run owns: file writes, child env construction, process execution,
		// audit events, and cleanup. CLI only resolves the strategy and
		// persists vault metadata.
		return runner.Run(context.Background(), strategy, runner.RunOptions{
			ProfileName:  prof.Name,
			ConfigDir:    resolvedConfigDir(),
			ExtraArgs:    extraArgs,
			InheritStdio: true,
		})
	},
}

func init() {
	runCmd.Flags().StringVarP(&runProfile, "profile", "p", "", "profile name or alias (defaults to settings.default_profile)")
	rootCmd.AddCommand(runCmd)
}

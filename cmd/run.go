package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/runner"
	"aegiskeys/internal/secret"
)

var runProfile string

func applyRunCommandOverride(strategy *adapter.LaunchStrategy, args []string) []string {
	extraArgs := args
	if len(args) == 0 {
		return extraArgs
	}
	if args[0] == strategy.Plan.Command {
		return args[1:]
	}
	strategy.Plan.Command = args[0]
	strategy.Plan.Args = nil
	return args[1:]
}

var (
	withKeys    []string
	withEnvVars []string
)

var withCmdRoot = &cobra.Command{
	Use:   "with --key <label-or-id> --env <VAR> -- <command> [args...]",
	Short: "Launch a command with selected vault credentials and no profile",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(withKeys) == 0 || len(withEnvVars) == 0 {
			return fmt.Errorf("at least one --key and --env are required")
		}
		if len(withKeys) != len(withEnvVars) {
			return fmt.Errorf("--key and --env must be provided in matching pairs")
		}
		v, err := loadVault()
		if err != nil {
			return err
		}
		env := make(map[string]string, len(withKeys))
		defer func() {
			for name := range env {
				env[name] = ""
				delete(env, name)
			}
			secret.ZeroVault(v)
		}()
		usedIDs := make(map[string]bool, len(withKeys))
		usedEnvNames := make(map[string]bool, len(withKeys))
		for i, selector := range withKeys {
			envName := strings.TrimSpace(withEnvVars[i])
			if !validEnvName(envName) {
				return fmt.Errorf("invalid environment variable name %q", envName)
			}
			if usedEnvNames[envName] {
				return fmt.Errorf("duplicate environment variable %q", envName)
			}
			rec := findVaultRecordByLabelOrID(v, selector)
			if rec == nil {
				return fmt.Errorf("no vault item matches %q", selector)
			}
			if usedIDs[rec.ID] {
				return fmt.Errorf("duplicate key selection %q", rec.Label)
			}
			if rec.Archived {
				return fmt.Errorf("key %q is archived", rec.Label)
			}
			if rec.Secret == "" {
				return fmt.Errorf("key %q has no primary secret", rec.Label)
			}
			usedIDs[rec.ID] = true
			usedEnvNames[envName] = true
			if err := rec.AllowAccess(secret.AccessInjectEnv); err != nil {
				return fmt.Errorf("key %q cannot be launch-injected: %w", rec.Label, err)
			}
			env[envName] = rec.Secret
		}
		strategy := &adapter.LaunchStrategy{
			Plan:    adapter.LaunchPlan{Command: args[0], Args: args[1:], Env: env, EnvSensitivity: make(map[string]string, len(env))},
			Support: adapter.AppSupportContract{ID: "explicit-launch", CanLaunchArbitraryCommand: true},
		}
		rawSecrets := make([]string, 0, len(withKeys))
		for name := range env {
			strategy.Plan.EnvSensitivity[name] = "secret"
			rawSecrets = append(rawSecrets, env[name])
		}
		if err := adapter.ValidateExplicitLaunch(strategy, rawSecrets); err != nil {
			return err
		}
		return runner.Run(context.Background(), strategy, runner.RunOptions{InheritStdio: true})
	},
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func findVaultRecordByLabelOrID(v *secret.Vault, selector string) *secret.SecretRecord {
	var found *secret.SecretRecord
	for i := range v.Keys {
		rec := &v.Keys[i]
		if rec.ID == selector || rec.Label == selector {
			if found != nil && found.ID != rec.ID {
				return nil
			}
			found = rec
		}
	}
	return found
}

func init() {
	withCmdRoot.Flags().StringSliceVar(&withKeys, "key", nil, "vault key label or ID (repeatable, paired with --env)")
	withCmdRoot.Flags().StringSliceVar(&withEnvVars, "env", nil, "target environment variable (repeatable, paired with --key)")
	rootCmd.AddCommand(withCmdRoot)
}

var runCmd = &cobra.Command{
	Use:   "run --profile <name> -- <command> [args...]",
	Short: "Run a command with the profile's secrets injected (legacy: use 'launch' or 'l')",
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
		strategy, err := adapter.ResolveLaunchStrategyCatalog(*prof, *prov, rec, adapterReg, reg, v, adapter.ResolveRun)
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
		if err := mutateVault(pw, secret.SessionMutation{
			Mutate: func(latest *secret.Vault) error {
				latest.Touch(prof.KeyID)
				return nil
			},
		}); err != nil {
			return err
		}

		// `run --profile <name> -- <command> [args...]` is the explicit
		// command-override surface. A launchable profile normally resolves its
		// own binary, but the documented run form must replace that binary so
		// callers can execute a diagnostic/helper with the profile-scoped env.
		// Repeating the resolved command preserves its adapter-provided args.
		extraArgs := applyRunCommandOverride(strategy, args)

		// Run owns: file writes, child env construction, process execution,
		// audit events, and cleanup. CLI only resolves the strategy and
		// persists vault metadata.
		return runner.Run(context.Background(), strategy, runner.RunOptions{
			ProfileName:     prof.Name,
			ConfigDir:       resolvedConfigDir(),
			ExtraArgs:       extraArgs,
			InheritStdio:    true,
			ExtraInheritEnv: loadAppConfig().InheritEnv,
		})
	},
}

func init() {
	runCmd.Flags().StringVarP(&runProfile, "profile", "p", "", "profile name or alias (defaults to settings.default_profile)")
	rootCmd.AddCommand(runCmd)
}

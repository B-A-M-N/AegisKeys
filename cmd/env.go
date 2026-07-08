package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/runner"
	"aegiskeys/internal/secret"
)

var envProfile string
var envExport bool

var envCmd = &cobra.Command{
	Use:   "env --profile <name> [--export]",
	Short: "Show the environment variables a profile would inject",
	Long: "By default prints a masked preview only. With --export (after " +
		"confirmation) prints full `export KEY='value'` lines.",
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName, err := effectiveProfileName(envProfile)
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
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(prof.KeyID)
		if rec == nil {
			return fmt.Errorf("profile %q references missing key %q", prof.Name, prof.KeyID)
		}

		// Enforce the secret's access policy BEFORE resolving or printing.
		if err := rec.AllowAccess(secret.AccessInjectEnv); err != nil {
			return fmt.Errorf("key %q: %w", rec.Label, err)
		}

		// Resolve through the adapter launch strategy gate so env output
		// reflects app-specific rendering and contract validation — same
		// path as `run`. Pass the full record so its access Policy is honored.
		adapterReg := adapter.NewRegistry()
		strategy, err := adapter.ResolveLaunchStrategyCatalog(*prof, *prov, rec, adapterReg, reg, v, adapter.ResolvePreview)
		if err != nil {
			return err
		}

		// Surface any adapter-detected hazards.
		if len(strategy.Hazards) > 0 {
			fmt.Println("Warnings:")
			for _, h := range strategy.Hazards {
				fmt.Printf("  [%s] %s\n", h.Severity, h.Title)
				if h.Fix != "" {
					fmt.Printf("    fix: %s\n", h.Fix)
				}
			}
			fmt.Println()
		}

		envVars := strategy.Plan.Env

		fmt.Printf("Profile: %s\n", prof.Name)
		fmt.Printf("Provider: %s\n", prov.Name)
		if !envExport {
			fmt.Println("Injected variables (masked):")
			printSorted(maskEnv(envVars, prov.CanonicalEnvVar(), prof))
			fmt.Println("\nNo full secrets printed.")
			fmt.Println("Use --export with explicit confirmation to print shell exports.")
			return nil
		}

		// Risky path: requires confirmation AND the key must allow reveal/export.
		if err := rec.AllowAccess(secret.AccessRevealStdout); err != nil {
			return fmt.Errorf("key %q cannot be revealed: %w", rec.Label, err)
		}
		fmt.Println("This will print full secrets to your terminal.")
		fmt.Println("Only continue if you understand the risk.")
		if !confirm("Type EXPORT to continue.", "EXPORT") {
			fmt.Println("Aborted.")
			return nil
		}
		fmt.Print(runner.BuildShellExport(envVars))
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "env_export_requested",
			Profile:  prof.Name,
			Provider: prof.ProviderSlug,
		})
		return nil
	},
}

// maskEnv returns a copy of env with credential-like values masked for display.
// The provider's canonical secret var and any profile-defined env keys are masked.
func maskEnv(env map[string]string, canonicalSecretVar string, prof *profile.Profile) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if k == canonicalSecretVar {
			out[k] = secret.MaskSecret(v)
			continue
		}
		if _, isProfEnv := prof.Env[k]; isProfEnv {
			out[k] = secret.MaskSecret(v)
			continue
		}
		out[k] = v
	}
	return out
}
func printSorted(m map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s=%s\n", k, m[k])
	}
}

func init() {
	envCmd.Flags().StringVarP(&envProfile, "profile", "p", "", "profile name or alias (defaults to settings.default_profile)")
	envCmd.Flags().BoolVar(&envExport, "export", false, "print full export commands (requires confirmation)")
	rootCmd.AddCommand(envCmd)
}

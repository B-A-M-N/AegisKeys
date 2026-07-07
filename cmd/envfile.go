package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/runner"
	"aegiskeys/internal/secret"
)

var envfileProfile string

var envfileCmd = &cobra.Command{
	Use:   "envfile --profile <name>",
	Short: "Create a temporary env file (0600) for the profile",
	Long: "Writes the profile's resolved environment to a temporary file " +
		"under the aegiskeys tmp/ directory with permission 0600, then " +
		"prints the path and a shred command.",
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName, err := effectiveProfileName(envfileProfile)
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
		// Runtime policy gate: the strict policy forbids materializing
		// secrets to a plaintext env file (the only risky-export surface).
		cfg := loadAppConfig()
		if !cfg.AllowsRiskyExport() {
			return fmt.Errorf("runtime policy %q forbids writing secrets to an env file; set runtime_policy to standard/permissive via `aegiskeys settings set runtime_policy standard`", cfg.RuntimePolicy)
		}
		if !confirm("This writes full secrets to a file on disk. Type CREATE to continue.", "CREATE") {
			fmt.Println("Aborted.")
			return nil
		}
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(prof.KeyID)
		if rec == nil {
			return fmt.Errorf("profile %q references missing key %q", prof.Name, prof.KeyID)
		}

		// Enforce the secret's access policy BEFORE materializing it to disk.
		if err := rec.AllowAccess(secret.AccessInjectEnv); err != nil {
			return fmt.Errorf("key %q: %w", rec.Label, err)
		}
		if !rec.Policy.AllowEnvExport {
			return fmt.Errorf("key %q policy forbids writing secrets to an env file", rec.Label)
		}

		// Resolve through the adapter launch strategy gate — same path
		// as `run` and `env`, so envfile contents match actual injection.
		// Pass the full record so its access Policy is honored.
		adapterReg := adapter.NewRegistry()
		strategy, err := adapter.ResolveLaunchStrategy(*prof, *prov, rec, adapterReg)
		if err != nil {
			return err
		}
		envVars := strategy.Plan.Env

		tmpDir := config.TmpPath(resolvedConfigDir())
		if err := os.MkdirAll(tmpDir, 0700); err != nil {
			return err
		}
		name := fmt.Sprintf("%s.%d.env", safeEnvfileName(prof.Name), time.Now().Unix())
		path := filepath.Join(tmpDir, name)

		if err := os.WriteFile(path, []byte(runner.BuildShellExport(envVars)), 0600); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "envfile_created",
			Profile:  prof.Name,
			Provider: prof.ProviderSlug,
		})
		fmt.Printf("Created temporary env file:\n%s\n\nPermissions: 0600\n\nDelete it with:\naegiskeys shred-envfile %s\n",
			path, path)
		return nil
	},
}

var shredEnvfileCmd = &cobra.Command{
	Use:   "shred-envfile <path>",
	Short: "Securely delete a temporary env file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		// Only allow shredding files inside the aegiskeys tmp dir to avoid
		// accidental deletion of arbitrary files.
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		tmpRoot, err := filepath.Abs(config.TmpPath(resolvedConfigDir()))
		if err != nil {
			return err
		}
		if !strings.HasPrefix(abs, tmpRoot) {
			return fmt.Errorf("refusing to shred %s: outside aegiskeys tmp dir", abs)
		}
		// Overwrite then remove.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, zeroBytes(len(data)), 0600); err != nil {
			// Non-fatal: SSDs/journaling may still retain traces anyway.
			fmt.Fprintf(os.Stderr, "warning: overwrite failed: %v\n", err)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Println("Shredded", path)
		fmt.Fprintln(os.Stderr, "Note: SSDs and journaling filesystems may still retain traces.")
		return nil
	},
}

func init() {
	envfileCmd.Flags().StringVarP(&envfileProfile, "profile", "p", "", "profile name or alias (defaults to settings.default_profile)")
	rootCmd.AddCommand(envfileCmd, shredEnvfileCmd)
}

func zeroBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 0
	}
	return b
}

// safeEnvfileName returns a filesystem-safe version of a profile name
// to prevent path traversal via crafted profile names.
func safeEnvfileName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, name)
}

// Package cmd implements the aegiskeys command-line interface using Cobra.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"aegiskeys/internal/bootstrap"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

// binaryName returns the name the user invoked this binary as, defaulting to
// "aegiskeys" when invoked via `go run .` or an unrecognized path. This makes
// `ak` and `aegiskeys` both show the correct name in help, completion, and
// version output.
func binaryName() string {
	if len(os.Args) > 0 {
		if base := filepath.Base(os.Args[0]); base != "" && base != "." {
			return base
		}
	}
	return "aegiskeys"
}

// version is the aegiskeys release version. Overridden at build time via
// -ldflags "-X aegiskeys/cmd.version=...".
var version = "dev"

// rootCmd is the entry point. Running aegiskeys (or its `ak` alias) with
// no subcommand launches the TUI (SPEC §17). All subcommands are attached
// below via init(). The Use field is set in Execute() from the binary name
// so help/completion show the invoked name.
var rootCmd = &cobra.Command{
	Short: "Secure local vault for API providers and secrets",
	Long: "AegisKeys stores API provider metadata and encrypted secrets " +
		"separately, then injects the correct credentials into coding " +
		"agents and CLIs as child-process-scoped environment variables.",
	// Silence usage on runtime errors (e.g. wrong password). Usage is still
	// shown for flag/argument parse errors via RunE returning cobra errors.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// No subcommand -> launch the TUI.
		return runTUI(cmd)
	},
}

// configDir holds the resolved config directory for this process. Set in
// Execute from the --config flag (or the default), and read by helpers.
var configDir string

func init() {
	rootCmd.PersistentFlags().StringVarP(&configDir, "config", "c", "",
		"config directory (default "+config.DefaultConfigDir()+")")
}

// Execute runs the root command. It is the sole entry point from main.
func Execute() {
	rootCmd.Use = binaryName()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolvedConfigDir returns the effective config directory, falling back to
// DefaultConfigDir() when the --config flag was not set.
func resolvedConfigDir() string {
	if configDir != "" {
		return configDir
	}
	return config.DefaultConfigDir()
}

// requireInitialized ensures the config directory exists and has been
// initialized. It auto-bootstraps non-interactive pieces (config dir,
// providers, profiles) and only errors if the vault is missing.
func requireInitialized() error {
	dir := resolvedConfigDir()
	state := bootstrap.Detect(dir)

	if state.IsFullyReady() {
		return nil
	}

	// Auto-bootstrap what we can without user interaction.
	if err := bootstrap.AutoBootstrap(dir); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	// Re-check state after bootstrap.
	state = bootstrap.Detect(dir)
	if !state.VaultExists {
		return fmt.Errorf("no vault found at %s\nRun `aegiskeys init` to create one", dir)
	}

	return nil
}

// loadStores loads the provider registry and profile store from the config
// directory. Missing files are not fatal: a fresh registry/store is returned
// (matching the first-run-friendly convention in the provider/profile packages).
func loadStores() (*provider.Registry, *profile.Store, error) {
	dir := resolvedConfigDir()
	reg, err := provider.LoadRegistry(config.ProvidersPath(dir))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("load providers: %w", err)
	}
	// Self-heal: merge missing defaults so a partial registry never strands
	// the user. requireInitialized already ran AutoBootstrap, which heals on
	// disk; this catches the CLI path that bypasses it.
	if reg == nil {
		reg = provider.NewRegistry()
	}
	if reg.MergeDefaults(provider.DefaultProviders()) {
		_ = reg.Save(config.ProvidersPath(dir))
	}
	store, err := profile.LoadStore(config.ProfilesPath(dir))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("load profiles: %w", err)
	}
	return reg, store, nil
}

// Package bootstrap handles first-run detection and auto-initialization.
// It eliminates the confusing "run init" pattern by auto-bootstrapping
// on first use.
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// State describes what needs to happen before the app is usable.
type State struct {
	ConfigDir     string
	DirExists     bool
	ConfigExists  bool
	VaultExists   bool
	ProvidersSeed bool
	ProfilesInit  bool
}

// NeedsInit reports whether the user must run the full init flow.
func (s State) NeedsInit() bool {
	return !s.DirExists || !s.VaultExists
}

// NeedsProviderSeed reports whether providers.json is missing.
func (s State) NeedsProviderSeed() bool {
	return s.DirExists && !s.ProvidersSeed
}

// NeedsProfileInit reports whether profiles.json is missing.
func (s State) NeedsProfileInit() bool {
	return s.DirExists && !s.ProfilesInit
}

// IsFullyReady reports whether the app can operate without any setup.
func (s State) IsFullyReady() bool {
	return s.DirExists && s.ConfigExists && s.VaultExists && s.ProvidersSeed && s.ProfilesInit
}

// Detect checks the current state of the config directory.
func Detect(configDir string) State {
	s := State{ConfigDir: configDir}

	info, err := os.Stat(configDir)
	s.DirExists = err == nil && info.IsDir()

	if !s.DirExists {
		return s
	}

	s.ConfigExists = fileExists(filepath.Join(configDir, config.ConfigFile))
	s.VaultExists = fileExists(filepath.Join(configDir, config.VaultFile))
	s.ProvidersSeed = fileExists(filepath.Join(configDir, config.ProvidersFile))
	s.ProfilesInit = fileExists(filepath.Join(configDir, config.ProfilesFile))

	return s
}

// EnsureDir creates the config directory if missing.
func EnsureDir(configDir string) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	return os.Chmod(configDir, 0700)
}

// AutoBootstrap performs non-interactive first-run setup:
// - Creates config dir
// - Seeds providers.json if missing
// - Creates empty profiles.json if missing
// - Creates config.json if missing
// It does NOT create the vault (that requires a password).
func AutoBootstrap(configDir string) error {
	if err := EnsureDir(configDir); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Seed or heal the provider registry. Always merge defaults so a partial
	// or hand-edited providers.json gets missing default providers and
	// backfilled structural fields — the app must never strand the user with
	// only a single custom provider and no path to create a profile.
	providersPath := filepath.Join(configDir, config.ProvidersFile)
	reg, err := provider.LoadRegistry(providersPath)
	if err != nil {
		reg = provider.NewRegistry()
	}
	if reg.MergeDefaults(provider.DefaultProviders()) {
		if err := reg.Save(providersPath); err != nil {
			return fmt.Errorf("save providers: %w", err)
		}
	}

	// Create empty profiles if missing.
	profilesPath := filepath.Join(configDir, config.ProfilesFile)
	if !fileExists(profilesPath) {
		if err := profile.SaveStore(profilesPath, profile.NewStore()); err != nil {
			return fmt.Errorf("create profiles: %w", err)
		}
	}

	// Create config if missing.
	configPath := filepath.Join(configDir, config.ConfigFile)
	if !fileExists(configPath) {
		cfg := config.DefaultConfig()
		if err := config.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}

	return nil
}

// InitVault creates the encrypted vault with the given password.
// Returns an error if the vault already exists.
func InitVault(configDir, password string) error {
	vaultPath := filepath.Join(configDir, config.VaultFile)
	if secret.VaultExists(vaultPath) {
		return fmt.Errorf("vault already exists at %s", vaultPath)
	}
	return secret.InitVault(vaultPath, password)
}

// InitVaultWithPassword is like InitVault but takes a byte slice
// so the caller can zero it after use.
func InitVaultWithPassword(configDir string, password []byte) error {
	vaultPath := filepath.Join(configDir, config.VaultFile)
	if secret.VaultExists(vaultPath) {
		return fmt.Errorf("vault already exists at %s", vaultPath)
	}
	return secret.InitVault(vaultPath, string(password))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

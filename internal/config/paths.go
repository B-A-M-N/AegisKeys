package config

import (
	"os"
	"path/filepath"
)

const (
	AppName   = "aegiskeys"
	ConfigDir = ".config/aegiskeys"

	ConfigFile    = "config.json"
	ProvidersFile = "providers.json"
	ProfilesFile  = "profiles.json"
	VaultFile     = "vault.enc"
	AuditFile     = "audit.log"
	TmpDir        = "tmp"
)

// DefaultConfigDir returns the OS-appropriate config directory path.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ConfigDir)
}

// EnsureDir creates the config directory with 0700 permissions.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

// ConfigPath returns the config.json path inside dir.
func ConfigPath(dir string) string { return filepath.Join(dir, ConfigFile) }

// ProvidersPath returns the providers.json path inside dir.
func ProvidersPath(dir string) string { return filepath.Join(dir, ProvidersFile) }

// ProfilesPath returns the profiles.json path inside dir.
func ProfilesPath(dir string) string { return filepath.Join(dir, ProfilesFile) }

// VaultPath returns the vault.enc path inside dir.
func VaultPath(dir string) string { return filepath.Join(dir, VaultFile) }

// AuditPath returns the audit.log path inside dir.
func AuditPath(dir string) string { return filepath.Join(dir, AuditFile) }

// TmpPath returns the tmp/ directory path inside dir.
func TmpPath(dir string) string { return filepath.Join(dir, TmpDir) }

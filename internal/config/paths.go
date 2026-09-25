package config

import (
	"os"
	"path/filepath"

	"aegiskeys/internal/fsutil"
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
	RuntimeDir    = "run"
	BrokerFile    = "broker.json"
	BrokerSocket  = "broker.sock"
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
	if err := fsutil.EnsureDir(path); err != nil {
		return err
	}
	return fsutil.ChmodNoFollow(path, 0700)
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

// BrokerPath returns the metadata-only broker binding/grant store path.
func BrokerPath(dir string) string { return filepath.Join(dir, BrokerFile) }

// RuntimePath returns the private runtime directory path inside dir.
func RuntimePath(dir string) string { return filepath.Join(dir, RuntimeDir) }

// BrokerSocketPath returns the local Unix-domain socket path.
func BrokerSocketPath(dir string) string {
	return filepath.Join(RuntimePath(dir), BrokerSocket)
}

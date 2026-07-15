package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aegiskeys/internal/config"
	"aegiskeys/internal/keychain"
	"aegiskeys/internal/secret"
	"github.com/spf13/cobra"
)

var keyringRecoveryFile string

var keyringEnableCmd = &cobra.Command{
	Use:   "keyring-enable",
	Short: "Enable password-recoverable OS-keyring vault unlock",
	Long: "Stores the existing password-derived vault key in the operating system keyring. " +
		"The master password remains a recovery method.",
	RunE: func(cmd *cobra.Command, args []string) error {
		pw, err := readPassword("Master password: ")
		if err != nil {
			return err
		}
		path := config.VaultPath(resolvedConfigDir())
		mode, err := secret.VaultKeyMode(path)
		if err != nil {
			return err
		}
		if mode == "keyring" {
			return errors.New("vault is keyring-required already; use its OS keyring entry or recovery key")
		}
		_, key, err := secret.LoadVaultWithKey(path, pw)
		if err != nil {
			return err
		}
		if err := keychain.Store(resolvedConfigDir(), key); err != nil {
			return err
		}
		cfg := loadAppConfig()
		cfg.KeyringEnabled = true
		if err := config.SaveConfig(config.ConfigPath(resolvedConfigDir()), cfg); err != nil {
			_ = keychain.Delete(resolvedConfigDir())
			return err
		}
		fmt.Println("OS keyring unlock enabled; password recovery remains available.")
		return nil
	},
}

var keyringRequiredCmd = &cobra.Command{
	Use:   "keyring-required",
	Short: "Migrate vault to OS-keyring-only unlock with a recovery key",
	Long: "Disables password unlock and creates a keyring-only vault. A recovery key is written " +
		"to --recovery-file with 0600 permissions; safeguard that file like the vault itself.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if keyringRecoveryFile == "" {
			return errors.New("--recovery-file is required to prevent an unrecoverable migration")
		}
		if !confirm("Password unlock will be disabled. Ensure the recovery file is stored securely.", "MIGRATE") {
			return errors.New("migration cancelled")
		}
		pw, err := readPassword("Master password: ")
		if err != nil {
			return err
		}
		path := config.VaultPath(resolvedConfigDir())
		mode, err := secret.VaultKeyMode(path)
		if err != nil {
			return err
		}
		if mode == "keyring" {
			return errors.New("vault is already keyring-required")
		}
		// Verify the password before creating a recovery artifact.
		if _, _, err := secret.LoadVaultWithKey(path, pw); err != nil {
			return err
		}
		key, err := secret.RandomVaultKey()
		if err != nil {
			return err
		}
		if err := keychain.Store(resolvedConfigDir(), key); err != nil {
			return fmt.Errorf("store vault key in OS keyring: %w", err)
		}
		if err := writeRecoveryKey(keyringRecoveryFile, key); err != nil {
			_ = keychain.Delete(resolvedConfigDir())
			return err
		}
		cfg := loadAppConfig()
		cfg.KeyringEnabled = true
		if err := config.SaveConfig(config.ConfigPath(resolvedConfigDir()), cfg); err != nil {
			_ = keychain.Delete(resolvedConfigDir())
			return err
		}
		if err := secret.MigrateToKeyringRequiredWithKey(path, pw, key); err != nil {
			return fmt.Errorf("keyring enabled but password migration did not complete; the vault remains password-recoverable: %w", err)
		}
		fmt.Println("Vault migrated to OS-keyring-only unlock. Password recovery is disabled.")
		return nil
	},
}

var keyringStatusCmd = &cobra.Command{
	Use:   "keyring-status",
	Short: "Show vault OS-keyring unlock status",
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := secret.VaultKeyMode(config.VaultPath(resolvedConfigDir()))
		if err != nil {
			return err
		}
		_, keyErr := keychain.Load(resolvedConfigDir())
		fmt.Printf("Vault mode: %s\nOS keyring entry: %s\n", mode, map[bool]string{true: "available", false: "unavailable"}[keyErr == nil])
		return nil
	},
}

var keyringRecoverCmd = &cobra.Command{
	Use:   "keyring-recover --recovery-file <path>",
	Short: "Restore a missing OS-keyring vault key from a recovery file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if keyringRecoveryFile == "" {
			return errors.New("--recovery-file is required")
		}
		path := config.VaultPath(resolvedConfigDir())
		mode, err := secret.VaultKeyMode(path)
		if err != nil {
			return err
		}
		if mode != "keyring" {
			return errors.New("recovery is only needed for a keyring-required vault")
		}
		raw, err := os.ReadFile(filepath.Clean(keyringRecoveryFile))
		if err != nil {
			return fmt.Errorf("read recovery file: %w", err)
		}
		decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(decoded) != 32 {
			return errors.New("recovery file does not contain a valid vault key")
		}
		var key [32]byte
		copy(key[:], decoded)
		if _, err := secret.LoadVaultByKey(path, key); err != nil {
			return fmt.Errorf("recovery key cannot open vault: %w", err)
		}
		if err := keychain.Store(resolvedConfigDir(), key); err != nil {
			return err
		}
		cfg := loadAppConfig()
		cfg.KeyringEnabled = true
		if err := config.SaveConfig(config.ConfigPath(resolvedConfigDir()), cfg); err != nil {
			_ = keychain.Delete(resolvedConfigDir())
			return err
		}
		fmt.Println("OS keyring vault key restored.")
		return nil
	},
}

func writeRecoveryKey(path string, key [32]byte) error {
	clean := filepath.Clean(path)
	if clean == "." || clean == string(filepath.Separator) {
		return errors.New("invalid recovery file path")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0700); err != nil {
		return fmt.Errorf("create recovery directory: %w", err)
	}
	f, err := os.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create recovery file (it must not already exist): %w", err)
	}
	data := []byte(base64.RawStdEncoding.EncodeToString(key[:]) + "\n")
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write recovery file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync recovery file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close recovery file: %w", err)
	}
	return nil
}

func init() {
	keyringRequiredCmd.Flags().StringVar(&keyringRecoveryFile, "recovery-file", "", "new 0600 file for the recovery key")
	keyringRecoverCmd.Flags().StringVar(&keyringRecoveryFile, "recovery-file", "", "0600 recovery-key file")
	rootCmd.AddCommand(keyringEnableCmd, keyringRequiredCmd, keyringStatusCmd, keyringRecoverCmd)
}

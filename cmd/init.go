package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/bootstrap"
	"aegiskeys/internal/config"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

var initPassword string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the AegisKeys vault and config directory",
	Long: "Creates the config directory, prompts for a master password, " +
		"creates an encrypted vault, and seeds the default provider registry.",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolvedConfigDir()

		// Auto-bootstrap non-interactive pieces first.
		if err := bootstrap.AutoBootstrap(dir); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		vaultPath := config.VaultPath(dir)
		if secret.VaultExists(vaultPath) {
			return fmt.Errorf("vault already exists at %s", vaultPath)
		}

		// Non-interactive path: a --password flag supplies the master
		// password directly (used in automation). It is still length-checked,
		// but confirmation is skipped since there is no second prompt.
		var pw string
		if initPassword != "" {
			pw = initPassword
		} else {
			var err error
			pw, err = readPassword("Choose a master password: ")
			if err != nil {
				return err
			}
			confirmPw, cerr := readPassword("Confirm master password: ")
			if cerr != nil {
				return cerr
			}
			if pw != confirmPw {
				return fmt.Errorf("passwords do not match")
			}
		}
		if len(pw) < 8 {
			return fmt.Errorf("master password must be at least 8 characters")
		}

		if err := bootstrap.InitVault(dir, pw); err != nil {
			return fmt.Errorf("create vault: %w", err)
		}

		// Mark config initialized.
		cfg := config.DefaultConfig()
		cfg.Initialized = true
		if err := config.SaveConfig(config.ConfigPath(dir), cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		// Log the event (no secret value).
		logger := audit.NewLogger(config.AuditPath(dir))
		logger.Log(audit.Event{Event: "vault_initialized"})

		fmt.Printf("Initialized AegisKeys at %s\n", dir)
		fmt.Printf("Vault: %s (encrypted, Argon2id + AES-256-GCM)\n", vaultPath)

		// Count seeded providers.
		reg, _ := provider.LoadRegistry(config.ProvidersPath(dir))
		fmt.Printf("Providers: %d seeded\n", len(reg.Providers))
		fmt.Println("\nNext: add a key with `aegiskeys key add`, then create a profile.")
		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&initPassword, "password", "", "master password (non-interactive; less secure — visible in shell history)")
	rootCmd.AddCommand(initCmd)
}

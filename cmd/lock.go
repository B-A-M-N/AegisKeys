package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

// AegisKeys is a short-lived CLI: the vault is only ever decrypted in memory
// for the duration of a single command, then discarded. There is no
// long-lived daemon or unlocked state to clear. These commands therefore
// serve as verification + audit-trail entry points: they confirm the vault
// can be opened with the given password (unlock) or simply acknowledge that
// no in-memory state persists between invocations (lock).

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Confirm no vault is held in memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("AegisKeys holds no vault in memory between commands.")
		fmt.Println("Secrets are decrypted only for the duration of a single command.")
		return nil
	},
}

var unlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Verify the master password opens the vault",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		vaultPath := config.VaultPath(resolvedConfigDir())
		if !secret.VaultExists(vaultPath) {
			return fmt.Errorf("no vault found at %s", vaultPath)
		}
		pw, err := readPassword("Master password: ")
		if err != nil {
			return err
		}
		v, err := secret.LoadVault(vaultPath, pw)
		if err != nil {
			return err
		}
		fmt.Printf("Vault unlocked: %d key(s).\n", len(v.Keys))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(lockCmd, unlockCmd)
}

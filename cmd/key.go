package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage encrypted API keys",
}

// Flags for `key add`.
var keyAddProvider, keyAddLabel, keyAddTags string

var keyAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an API key to the vault",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		if keyAddProvider == "" {
			return fmt.Errorf("--provider is required")
		}
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		if reg.Find(keyAddProvider) == nil {
			return fmt.Errorf("unknown provider %q; run `aegiskeys provider list`", keyAddProvider)
		}
		// Unlock once; reuse the password for the save below.
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		// Always read secrets through the no-echo prompt. Do not accept secret
		// values as flags: argv can be captured by shell history and process
		// inspection.
		sec, err := readPassword("API key: ")
		if err != nil {
			return err
		}
		if sec == "" {
			return fmt.Errorf("secret value must not be empty")
		}
		rec := secret.SecretRecord{
			ProviderSlug: keyAddProvider,
			Label:        firstNonEmpty(keyAddLabel, keyAddProvider),
			Secret:       sec,
			Kind:         secret.SecretAPIKey,
		}
		id, err := secret.NewID()
		if err != nil {
			return fmt.Errorf("generate key id: %w", err)
		}
		rec.ID = id
		if keyAddTags != "" {
			rec.Tags = splitCSV(keyAddTags)
		}
		rec.Policy = secret.DefaultSecretPolicy(rec.Kind)
		if err := v.Add(rec); err != nil {
			return err
		}
		if err := saveVault(pw, v); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "key_added",
			Provider: keyAddProvider,
		})
		fmt.Printf("Added key %s (%s)\n", rec.ID, rec.Label)
		return nil
	},
}

var keyShowCmd = &cobra.Command{
	Use:   "show --id <id>",
	Short: "Show metadata and masked value of a key",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no key with id %q", keyID)
		}
		fmt.Printf("ID:          %s\n", rec.ID)
		fmt.Printf("Provider:    %s\n", rec.ProviderSlug)
		fmt.Printf("Label:       %s\n", rec.Label)
		fmt.Printf("Secret:      %s\n", secret.MaskSecret(rec.Secret))
		if len(rec.Tags) > 0 {
			fmt.Printf("Tags:        %s\n", joinStr(rec.Tags, ", "))
		}
		if rec.LastUsedAt != nil {
			fmt.Printf("Last used:   %s\n", rec.LastUsedAt.Format("2006-01-02 15:04"))
		} else {
			fmt.Println("Last used:   never")
		}
		return nil
	},
}

var keyRenameCmd = &cobra.Command{
	Use:   "rename --id <id> --label <label>",
	Short: "Rename a key",
	RunE: func(cmd *cobra.Command, args []string) error {
		if keyRenameLabel == "" {
			return fmt.Errorf("--label is required")
		}
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no key with id %q", keyID)
		}
		rec.Label = keyRenameLabel
		if err := saveVault(pw, v); err != nil {
			return err
		}
		fmt.Printf("Renamed key %s to %s\n", keyID, keyRenameLabel)
		return nil
	},
}

var keyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List keys in the vault (masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		masked := secret.ToMaskedList(v.Keys)
		if len(masked) == 0 {
			fmt.Println("No keys in the vault. Run `aegiskeys key add`.")
			return nil
		}
		fmt.Printf("%-18s %-14s %-20s %-22s\n", "ID", "PROVIDER", "LABEL", "SECRET")
		for _, m := range masked {
			fmt.Printf("%-18s %-14s %-20s %-22s\n", m.ID, m.ProviderSlug, m.Label, m.MaskedSecret)
		}
		return nil
	},
}

var keyRotateCmd = &cobra.Command{
	Use:   "rotate --id <id>",
	Short: "Replace the secret value of an existing key",
	RunE: func(cmd *cobra.Command, args []string) error {
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no key with id %q", keyID)
		}
		newSecret, err := readPassword("New API key: ")
		if err != nil {
			return err
		}
		if err := v.Rotate(keyID, newSecret); err != nil {
			return err
		}
		if err := saveVault(pw, v); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "key_rotated",
			Provider: rec.ProviderSlug,
		})
		fmt.Printf("Rotated key %s\n", keyID)
		return nil
	},
}

var keyDeleteCmd = &cobra.Command{
	Use:     "delete --id <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a key from the vault",
	RunE: func(cmd *cobra.Command, args []string) error {
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no key with id %q", keyID)
		}
		fmt.Printf("Deleting key %s (%s / %s)\n", rec.ID, rec.ProviderSlug, rec.Label)
		if !confirm("This permanently removes the key from the vault.", "DELETE") {
			fmt.Println("Aborted.")
			return nil
		}
		if err := v.Remove(keyID); err != nil {
			return err
		}
		if err := saveVault(pw, v); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "key_deleted",
			Provider: rec.ProviderSlug,
		})
		fmt.Printf("Deleted key %s\n", keyID)
		return nil
	},
}

var keyRevealCmd = &cobra.Command{
	Use:   "reveal --id <id>",
	Short: "Print the full secret value of a key to stdout",
	Long: "WARNING: prints the full secret to the terminal. Terminal " +
		"scrollback, shell logs, and screen recordings may capture it.",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no key with id %q", keyID)
		}
		if err := rec.AllowAccess(secret.AccessRevealStdout); err != nil {
			return fmt.Errorf("key %q policy forbids reveal: %w", rec.Label, err)
		}
		fmt.Println("This will print the full secret to your terminal.")
		fmt.Println("Terminal scrollback, shell logs, and screen recordings may capture it.")
		if !confirm("Type REVEAL to continue.", "REVEAL") {
			fmt.Println("Aborted.")
			return nil
		}
		fmt.Print(rec.Secret)
		if !strings.HasSuffix(rec.Secret, "\n") {
			fmt.Println()
		}
		return nil
	},
}

// keyID is shared by show/rotate/delete/reveal; keyRenameLabel for rename.
var keyID string
var keyRenameLabel string

func init() {
	keyAddCmd.Flags().StringVar(&keyAddProvider, "provider", "", "provider slug (required)")
	keyAddCmd.Flags().StringVar(&keyAddLabel, "label", "", "human-friendly label")
	keyAddCmd.Flags().StringVar(&keyAddTags, "tags", "", "comma-separated tags")

	keyShowCmd.Flags().StringVar(&keyID, "id", "", "key id (required)")
	keyRenameCmd.Flags().StringVar(&keyID, "id", "", "key id (required)")
	keyRenameCmd.Flags().StringVar(&keyRenameLabel, "label", "", "new label (required)")
	keyRotateCmd.Flags().StringVar(&keyID, "id", "", "key id (required)")
	keyDeleteCmd.Flags().StringVar(&keyID, "id", "", "key id (required)")
	keyRevealCmd.Flags().StringVar(&keyID, "id", "", "key id (required)")
	if err := cobra.MarkFlagRequired(keyShowCmd.Flags(), "id"); err != nil {
		panic(err)
	}
	if err := cobra.MarkFlagRequired(keyRenameCmd.Flags(), "id"); err != nil {
		panic(err)
	}
	if err := cobra.MarkFlagRequired(keyRenameCmd.Flags(), "label"); err != nil {
		panic(err)
	}
	if err := cobra.MarkFlagRequired(keyRotateCmd.Flags(), "id"); err != nil {
		panic(err)
	}
	if err := cobra.MarkFlagRequired(keyDeleteCmd.Flags(), "id"); err != nil {
		panic(err)
	}
	if err := cobra.MarkFlagRequired(keyRevealCmd.Flags(), "id"); err != nil {
		panic(err)
	}

	keyCmd.AddCommand(keyAddCmd, keyShowCmd, keyListCmd, keyRenameCmd, keyRotateCmd, keyDeleteCmd, keyRevealCmd)
	rootCmd.AddCommand(keyCmd)
}

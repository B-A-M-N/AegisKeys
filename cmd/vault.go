package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

// vaultCmd is the root command for general secret vault management.
var vaultCmd = &cobra.Command{
	Use:   "vault",
	Short: "Manage encrypted vault items (API keys, tokens, secrets)",
}

// Flags for `vault add`.
var (
	vaultAddKind         string
	vaultAddProvider     string
	vaultAddLabel        string
	vaultAddTags         string
	vaultAddDescription  string
	vaultAddAccount      string
	vaultAddEnvVar       string
	vaultAddBaseURL      string
	vaultAddDocsURL      string
	vaultAddRotationDays int
)

var vaultAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a vault item (API key, token, webhook secret, etc.)",
	Long: `Add an encrypted vault item. The secret is never written to disk in plaintext.

Kinds:
  api_key           Standard API key (sk-..., anthropic, etc.)
  bearer_token      OAuth / personal access token
  webhook_secret    Signing secret for webhooks
  service_account_json  GCP/AWS service-account JSON
  basic_auth        Username:password credentials
  generic_secret    Any other secret value

Examples:
  aegiskeys vault add --kind api_key --provider openrouter --label main
  aegiskeys vault add --kind bearer_token --label "GitHub PAT" --env-var GITHUB_TOKEN
  aegiskeys vault add --kind webhook_secret --label "Stripe webhook"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		kind := secret.SecretKind(vaultAddKind)
		if kind == "" {
			kind = secret.SecretAPIKey
		}
		// Validate provider if specified.
		if vaultAddProvider != "" {
			reg, _, err := loadStores()
			if err != nil {
				return err
			}
			if reg.Find(vaultAddProvider) == nil {
				return fmt.Errorf("unknown provider %q; run `aegiskeys provider list`", vaultAddProvider)
			}
		}
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		// Always prompt for the secret. A flag would put key material in argv,
		// which can be captured by shell history and process listings.
		sec, err := readPassword("Secret value: ")
		if err != nil {
			return err
		}
		if sec == "" {
			return fmt.Errorf("secret value must not be empty")
		}
		label := vaultAddLabel
		if label == "" {
			label = vaultAddProvider
		}
		if label == "" {
			label = strings.Replace(string(kind), "_", " ", -1)
		}
		rec := secret.SecretRecord{
			Kind:         kind,
			ProviderSlug: vaultAddProvider,
			Label:        label,
			Description:  vaultAddDescription,
			Account:      vaultAddAccount,
			Secret:       sec,
			EnvVarHint:   vaultAddEnvVar,
			BaseURLHint:  vaultAddBaseURL,
			DocsURL:      vaultAddDocsURL,
			Policy:       secret.DefaultSecretPolicy(kind),
			RevealPolicy: secret.RevealConfirm,
		}
		id, err := secret.NewID()
		if err != nil {
			return fmt.Errorf("generate key id: %w", err)
		}
		rec.ID = id
		if vaultAddTags != "" {
			rec.Tags = splitCSV(vaultAddTags)
		}
		if vaultAddRotationDays > 0 {
			t := timeNow().AddDate(0, 0, vaultAddRotationDays)
			rec.RotatesAt = &t
		}
		if err := v.Add(rec); err != nil {
			return err
		}
		if err := saveVault(pw, v); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "vault_item_added",
			Provider: vaultAddProvider,
		})
		fmt.Printf("Added vault item %s (%s)\n", rec.ID, rec.Label)
		return nil
	},
}

var vaultListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List vault items (masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		masked := secret.ToMaskedList(v.Keys)
		if len(masked) == 0 {
			fmt.Println("No vault items. Run `aegiskeys vault add`.")
			return nil
		}
		fmt.Printf("%-18s %-12s %-14s %-20s %-22s\n", "ID", "KIND", "PROVIDER", "LABEL", "SECRET")
		for _, m := range masked {
			provider := m.ProviderSlug
			if provider == "" {
				provider = "unlinked"
			}
			fmt.Printf("%-18s %-12s %-14s %-20s %-22s\n", m.ID, m.Kind, provider, m.Label, m.MaskedSecret)
		}
		return nil
	},
}

var vaultShowCmd = &cobra.Command{
	Use:   "show --id <id>",
	Short: "Show metadata of a vault item (masked secret)",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		fmt.Printf("ID:          %s\n", rec.ID)
		fmt.Printf("Kind:        %s\n", rec.Kind)
		fmt.Printf("Provider:    %s\n", firstNonEmpty(rec.ProviderSlug, "unlinked"))
		fmt.Printf("Label:       %s\n", rec.Label)
		if rec.Description != "" {
			fmt.Printf("Description: %s\n", rec.Description)
		}
		if rec.Account != "" {
			fmt.Printf("Account:     %s\n", rec.Account)
		}
		fmt.Printf("Secret:      %s\n", secret.MaskSecret(rec.Secret))
		if rec.EnvVarHint != "" {
			fmt.Printf("Env var:     %s\n", rec.EnvVarHint)
		}
		if rec.ExpiresAt != nil {
			fmt.Printf("Expires:     %s\n", rec.ExpiresAt.Format("2006-01-02"))
		}
		if rec.RotatesAt != nil {
			fmt.Printf("Rotates:     %s\n", rec.RotatesAt.Format("2006-01-02"))
		}
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

var vaultCopyCmd = &cobra.Command{
	Use:   "copy --id <id>",
	Short: "Copy a secret to the clipboard (confirmation required)",
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		if !rec.Policy.AllowClipboard {
			return fmt.Errorf("clipboard access denied by policy for %s", rec.ID)
		}
		fmt.Printf("About to copy secret for %q to clipboard.\n", rec.Label)
		fmt.Println("WARNING: Clipboard may be logged by OS, terminal multiplexer, or screenshot tools.")
		confirmed, err := confirmPrompt("Type the item label to confirm: ", rec.Label)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("aborted")
		}
		if err := copyToClipboard(rec.Secret); err != nil {
			return fmt.Errorf("clipboard not available: %w (use `aegiskeys vault reveal` instead)", err)
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event: "secret_copied",
		})
		ttl := rec.Policy.MaxClipboardTTLSeconds
		if ttl <= 0 {
			ttl = loadAppConfig().ClipboardTTLSeconds
		}
		if ttl > 0 {
			fmt.Printf("Copied to clipboard. Clearing in %d seconds...\n", ttl)
			time.Sleep(time.Duration(ttl) * time.Second)
			if err := clearClipboard(); err != nil {
				return fmt.Errorf("clear clipboard: %w", err)
			}
			fmt.Println("Clipboard cleared.")
			return nil
		}
		fmt.Println("Copied to clipboard. Clear it manually when done.")
		return nil
	},
}

var vaultRevealCmd = &cobra.Command{
	Use:   "reveal --id <id>",
	Short: "Reveal a secret on stdout (type label to confirm)",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		if rec.RevealPolicy == secret.RevealDeny {
			return fmt.Errorf("reveal denied by policy for %s", rec.ID)
		}
		fmt.Println("WARNING: This will print your raw secret to the terminal.")
		fmt.Println("Risks: shell history, scrollback, logs, screenshots, terminal capture.")
		confirmed, err := confirmPrompt("Type the item label to confirm: ", rec.Label)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("aborted")
		}
		fmt.Println(rec.Secret)
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event: "secret_revealed",
		})
		return nil
	},
}

var vaultEnvCmd = &cobra.Command{
	Use:   "env --id <id> [--name KEY]",
	Short: "Print KEY=<secret> to stdout (explicit confirmation required)",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(keyID)
		if rec == nil {
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		if !rec.Policy.AllowEnvExport {
			return fmt.Errorf("env export denied by policy for %s", rec.ID)
		}
		keyName := vaultEnvKeyName
		if keyName == "" {
			keyName = rec.EnvVarHint
		}
		if keyName == "" {
			keyName = "SECRET"
		}
		fmt.Println("WARNING: This will print KEY=secret to stdout.")
		fmt.Println("Risks: shell history, scrollback, logs, tmux capture, copy buffers.")
		confirmed, err := confirmPrompt("Type the item label to confirm: ", rec.Label)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("aborted")
		}
		fmt.Printf("%s=%s\n", keyName, rec.Secret)
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event: "secret_env_exported",
		})
		return nil
	},
}

var vaultRotateCmd = &cobra.Command{
	Use:   "rotate --id <id>",
	Short: "Replace the secret value of an existing vault item",
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		newSecret, err := readPassword("New secret value: ")
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
			Event:    "vault_item_rotated",
			Provider: rec.ProviderSlug,
		})
		fmt.Printf("Rotated vault item %s\n", keyID)
		return nil
	},
}

var vaultRenameCmd = &cobra.Command{
	Use:   "rename --id <id> --label <label>",
	Short: "Rename a vault item",
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		rec.Label = keyRenameLabel
		if err := saveVault(pw, v); err != nil {
			return err
		}
		fmt.Printf("Renamed vault item %s to %s\n", keyID, keyRenameLabel)
		return nil
	},
}

var vaultArchiveCmd = &cobra.Command{
	Use:   "archive --id <id>",
	Short: "Archive a vault item (hidden from default lists)",
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		rec.Archived = true
		if err := saveVault(pw, v); err != nil {
			return err
		}
		fmt.Printf("Archived vault item %s\n", keyID)
		return nil
	},
}

var vaultLinkCmd = &cobra.Command{
	Use:   "link --id <id> --provider <slug>",
	Short: "Link a vault item to a provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		if keyLinkProvider == "" {
			return fmt.Errorf("--provider is required")
		}
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		if reg.Find(keyLinkProvider) == nil {
			return fmt.Errorf("unknown provider %q", keyLinkProvider)
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		rec.ProviderSlug = keyLinkProvider
		if err := saveVault(pw, v); err != nil {
			return err
		}
		fmt.Printf("Linked %s to provider %s\n", keyID, keyLinkProvider)
		return nil
	},
}

var vaultUnlinkCmd = &cobra.Command{
	Use:   "unlink --id <id>",
	Short: "Remove the provider link from a vault item",
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		rec.ProviderSlug = ""
		if err := saveVault(pw, v); err != nil {
			return err
		}
		fmt.Printf("Unlinked vault item %s\n", keyID)
		return nil
	},
}

var vaultDeleteCmd = &cobra.Command{
	Use:     "delete --id <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a vault item permanently",
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
			return fmt.Errorf("no vault item with id %q", keyID)
		}
		fmt.Printf("WARNING: This will permanently delete %q (%s).\n", rec.Label, rec.ID)
		confirmed, err := confirmPrompt("Type the item label to confirm: ", rec.Label)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("aborted")
		}
		if err := v.Remove(keyID); err != nil {
			return err
		}
		if err := saveVault(pw, v); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event: "vault_item_deleted",
		})
		fmt.Printf("Deleted vault item %s\n", keyID)
		return nil
	},
}

// vaultEnvKeyName is the env var name for `vault env` output.
var vaultEnvKeyName string
var keyLinkProvider string
var vaultRepairMode string

// --- Phase 1: vault recovery & inspection commands ---

// vaultInspectCmd shows envelope metadata WITHOUT decrypting. Never prompts for
// a password and never prints secrets. Read-only diagnostic.
var vaultInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Show vault envelope metadata (read-only, no decryption)",
	Long: `Show the encrypted vault's envelope metadata without decrypting.

Reports envelope version, KDF algorithm, KDF parameters, salt/nonce presence,
and whether the envelope needs a rekey. Never decrypts, never prints secrets.
Useful for diagnosing unlock failures.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath := config.VaultPath(resolvedConfigDir())
		raw, err := os.ReadFile(vaultPath)
		if err != nil {
			return fmt.Errorf("read vault: %w", err)
		}
		var env secret.VaultEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("parse envelope: %w", err)
		}

		fmt.Printf("Vault path:      %s\n", vaultPath)
		if info, serr := os.Stat(vaultPath); serr == nil {
			fmt.Printf("File perms:      %04o\n", info.Mode().Perm())
		}
		fmt.Printf("Envelope version: %d\n", env.Version)
		fmt.Printf("KDF:             %s\n", env.KDF)
		fmt.Printf("KDF time:        %d\n", env.KDFParams.Time)
		fmt.Printf("KDF memory KiB:  %d\n", env.KDFParams.MemoryKiB)
		fmt.Printf("KDF threads:     %d\n", env.KDFParams.Threads)
		fmt.Printf("Salt present:    %v\n", env.Salt != "")
		fmt.Printf("Nonce present:   %v\n", env.Nonce != "")
		fmt.Printf("Needs rekey:     %v\n", secret.NeedsRekey(&env))

		if env.KDFParams.Time == 0 {
			fmt.Println("\n(no KDF params stored — legacy Argon2i envelope)")
		}
		return nil
	},
}

// vaultBackupCmd copies vault.enc to vault.enc.bak.<utc-timestamp>.
var vaultBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up the encrypted vault (no decryption)",
	Long:  "Copy the encrypted vault file to vault.enc.bak.<timestamp> for recovery. Does not decrypt.",
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath := config.VaultPath(resolvedConfigDir())
		if !secret.VaultExists(vaultPath) {
			return fmt.Errorf("no vault found at %s", vaultPath)
		}
		backupPath, err := secret.WriteVaultBackup(vaultPath)
		if err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
		fmt.Printf("Backed up vault to %s\n", backupPath)
		return nil
	},
}

// vaultRepairUnlockCmd detects and fixes KDF metadata / key mismatches.
var vaultRepairUnlockCmd = &cobra.Command{
	Use:   "repair-unlock",
	Short: "Detect and fix vault unlock failures caused by KDF metadata mismatch",
	Long: `Diagnose why a correct password fails to unlock the vault.

When the envelope's stored KDF params do not match the derivation that
actually sealed the key (a state some older versions could create), unlock
reports "wrong password" even with the correct password.

This command tries both current and legacy derivations, detects the mismatch,
writes a backup, then re-seals the vault so the password works again.

Modes:
  preserve  keep the legacy Argon2i metadata (Time=0)  — fastest, smallest change
  upgrade   rekey to current Argon2id params + new salt — moves off legacy path`,
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath := config.VaultPath(resolvedConfigDir())
		if !secret.VaultExists(vaultPath) {
			return fmt.Errorf("no vault found at %s", vaultPath)
		}
		mode := secret.RepairPreserve
		if vaultRepairMode == "upgrade" {
			mode = secret.RepairUpgrade
		} else if vaultRepairMode != "" && vaultRepairMode != "preserve" {
			return fmt.Errorf("invalid --mode %q (want preserve or upgrade)", vaultRepairMode)
		}

		pw, err := promptPassword()
		if err != nil {
			return err
		}

		// Diagnose first so we can report clearly.
		canUnlock, legacyWorks, _, derr := secret.DiagnoseUnlock(vaultPath, pw)
		if derr != nil {
			return derr
		}
		if canUnlock {
			fmt.Println("Vault unlocks normally — no mismatch detected.")
			fmt.Println("Use `aegiskeys vault rekey` if you want to rotate KDF params.")
			return nil
		}
		if !legacyWorks {
			return fmt.Errorf("password incorrect: neither current nor legacy derivation unlocks the vault")
		}

		fmt.Println("Detected KDF metadata / key mismatch. A backup will be written first.")
		result, err := secret.RepairVault(vaultPath, pw, mode)
		if err != nil {
			return err
		}
		if !result.Repaired {
			fmt.Println("No repair was needed.")
			return nil
		}
		fmt.Printf("Repaired vault (mode=%s, prev KDF time=%d).\n", result.Mode, result.PrevTime)
		fmt.Println("Unlock now works with the same password.")
		return nil
	},
}

// vaultRekeyCmd re-encrypts the vault with current Argon2id KDF params.
var vaultRekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Re-encrypt the vault with current Argon2id KDF params (same password)",
	Long: `Decrypt with the current password and reseal with the current Argon2id
KDF parameters. Use this to move a legacy (Argon2i) vault onto the current
policy. The password does not change. Backs up the vault first.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultPath := config.VaultPath(resolvedConfigDir())
		if !secret.VaultExists(vaultPath) {
			return fmt.Errorf("no vault found at %s", vaultPath)
		}
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		// Backup first.
		if backupPath, berr := secret.WriteVaultBackup(vaultPath); berr == nil {
			fmt.Printf("Backup written to %s\n", backupPath)
		} else {
			return fmt.Errorf("backup before rekey failed: %w", berr)
		}
		result, err := secret.RekeyVault(vaultPath, pw, secret.DefaultArgon2Params)
		if err != nil {
			return fmt.Errorf("rekey failed: %w", err)
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "vault_rekeyed",
			Metadata: map[string]string{"reason": result.Reason, "old_time": fmt.Sprint(result.OldTime), "new_time": fmt.Sprint(result.NewTime)},
		})
		fmt.Printf("Rekeyed vault (reason=%s): KDF time %d -> %d\n", result.Reason, result.OldTime, result.NewTime)
		return nil
	},
}

func init() {
	// Register vault command with root.
	rootCmd.AddCommand(vaultCmd)

	// `vault add` flags.
	vaultAddCmd.Flags().StringVar(&vaultAddKind, "kind", "api_key", "Secret kind: api_key, bearer_token, webhook_secret, service_account_json, basic_auth, generic_secret")
	vaultAddCmd.Flags().StringVar(&vaultAddProvider, "provider", "", "Optional provider slug")
	vaultAddCmd.Flags().StringVar(&vaultAddLabel, "label", "", "Human-readable label")
	vaultAddCmd.Flags().StringVar(&vaultAddTags, "tags", "", "Comma-separated tags")
	vaultAddCmd.Flags().StringVar(&vaultAddDescription, "description", "", "Optional description")
	vaultAddCmd.Flags().StringVar(&vaultAddAccount, "account", "", "Associated account (email, user, team)")
	vaultAddCmd.Flags().StringVar(&vaultAddEnvVar, "env-var", "", "Environment variable name hint")
	vaultAddCmd.Flags().StringVar(&vaultAddBaseURL, "base-url", "", "Base URL hint")
	vaultAddCmd.Flags().StringVar(&vaultAddDocsURL, "docs-url", "", "Documentation URL")
	vaultAddCmd.Flags().IntVar(&vaultAddRotationDays, "rotation-days", 0, "Days until rotation reminder")

	vaultEnvCmd.Flags().StringVar(&vaultEnvKeyName, "name", "", "Env var name (defaults to EnvVarHint or SECRET)")

	vaultCmd.AddCommand(vaultAddCmd, vaultListCmd, vaultShowCmd, vaultCopyCmd, vaultRevealCmd, vaultEnvCmd, vaultRotateCmd, vaultRenameCmd, vaultArchiveCmd, vaultLinkCmd, vaultUnlinkCmd, vaultDeleteCmd)

	// Recovery & inspection commands.
	vaultRepairUnlockCmd.Flags().StringVar(&vaultRepairMode, "mode", "preserve", "Repair mode: preserve (keep legacy KDF) or upgrade (rekey to Argon2id)")
	vaultCmd.AddCommand(vaultInspectCmd, vaultBackupCmd, vaultRepairUnlockCmd, vaultRekeyCmd)
}

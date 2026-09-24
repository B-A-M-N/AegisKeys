package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/broker"
	"aegiskeys/internal/config"
	"aegiskeys/internal/keychain"
	"aegiskeys/internal/secret"
)

var (
	accessBindingName        string
	accessBindingSecret      string
	accessBindingDescription string
	accessBindingComponents  []string
	accessAllowApp           string
	accessAllowKey           string
	accessAllowPermanent     bool
	accessBrokerResolve      bool
	accessBrokerRotate       bool
	accessGrantExec          string
	accessGrantName          string
	accessGrantCapabilities  []string
	accessGrantExpiry        string
	accessGrantID            string
	accessBindingID          string
)

func loadBrokerMeta() (*broker.File, string) {
	dir := resolvedConfigDir()
	path := config.BrokerPath(dir)
	meta, err := broker.LoadBrokerFile(path)
	if err != nil {
		exitWithError(err)
	}
	return meta, path
}

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

func parseCapability(value string) (broker.Capability, error) {
	capability := broker.Capability(strings.ToLower(strings.TrimSpace(value)))
	if !capability.Valid() {
		return "", fmt.Errorf("unknown capability %q (use resolve or rotate)", value)
	}
	return capability, nil
}

func requireVaultKeyForBroker() [32]byte {
	if key, err := keychain.Load(resolvedConfigDir()); err == nil {
		return key
	}
	pw, err := promptPassword()
	if err != nil {
		exitWithError(err)
	}
	_, key, err := secret.LoadVaultWithKey(config.VaultPath(resolvedConfigDir()), pw)
	if err != nil {
		exitWithError(err)
	}
	return key
}

func requestSecretFromPrompt(prompt string) string {
	value, err := readPassword(prompt)
	if err != nil {
		exitWithError(err)
	}
	return strings.TrimSpace(value)
}

var brokerCmd = &cobra.Command{
	Use:   "broker",
	Short: "Run and inspect the local credential broker",
}

var brokerServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve authorized credential requests over a local Unix socket",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		dir := resolvedConfigDir()
		meta, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
		key := requireVaultKeyForBroker()
		socket := config.BrokerSocketPath(dir)
		peer := broker.NewPeerResolver()
		listener, err := broker.Listen(dir, socket, peer)
		if err != nil {
			return err
		}
		logger := audit.NewLogger(config.AuditPath(dir))
		session, err := broker.NewSession(listener, meta, config.VaultPath(dir), key, peer, brokerAuditLogger{logger})
		if err != nil {
			_ = listener.Close()
			return err
		}
		brokerAuditLogger{logger}.Log("broker_started", "ok", map[string]string{"socket": socket})
		fmt.Printf("Credential broker listening on %s\n", socket)

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		shutdown := make(chan struct{})
		var closeOnce sync.Once
		closeSession := func() { closeOnce.Do(func() { close(shutdown); _ = session.Close() }) }
		defer signal.Stop(stop)
		go func() {
			<-stop
			closeSession()
		}()

		if autoLockMinutes := loadAppConfig().BrokerAutoLockMinutes; autoLockMinutes > 0 {
			go func() {
				lock := time.NewTicker(time.Duration(autoLockMinutes) * time.Minute)
				defer lock.Stop()
				select {
				case <-lock.C:
					session.Lock()
				case <-shutdown:
				}
			}()
		}

		err = session.Serve()
		if err == nil || err == http.ErrServerClosed || strings.Contains(err.Error(), "use of closed network connection") {
			return nil
		}
		return err
	},
}

type brokerAuditLogger struct{ logger *audit.Logger }

func (l brokerAuditLogger) Log(event, result string, metadata map[string]string) {
	clean := make(map[string]string, len(metadata)+1)
	for k, v := range metadata {
		clean[k] = v
	}
	clean["result"] = result
	l.logger.Log(audit.Event{Event: event, Metadata: clean})
}

var brokerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check broker socket and locked state",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := resolvedConfigDir()
		socket := config.BrokerSocketPath(dir)
		fmt.Printf("Socket: %s\n", socket)
		info, err := os.Lstat(socket)
		if err != nil {
			fmt.Println("State: STOPPED")
			return nil
		}
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket path is unsafe")
		}
		client, err := brokerUnixClient(socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://broker/v1/status", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("State: STOPPED (stale socket)")
			return nil
		}
		defer resp.Body.Close()
		var out struct {
			Protocol string `json:"protocol"`
			Locked   bool   `json:"locked"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}
		state := "UNLOCKED"
		if out.Locked {
			state = "LOCKED"
		}
		fmt.Printf("State: %s\nProtocol: %s\n", state, out.Protocol)
		return nil
	},
}

var accessCmd = &cobra.Command{
	Use:   "access",
	Short: "Manage application credential bindings and grants",
}

var accessBindingCmd = &cobra.Command{
	Use:   "binding",
	Short: "Manage stable credential binding names",
}

var accessBindingAddCmd = &cobra.Command{
	Use:   "add --name <app/credential> --secret <secret-id>",
	Short: "Create a binding from a stable name to an encrypted vault record",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		if accessBindingName == "" || accessBindingSecret == "" {
			return fmt.Errorf("--name and --secret are required")
		}
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		meta, path := loadBrokerMeta()
		if meta.FindBinding(accessBindingName) != nil {
			return fmt.Errorf("binding %q already exists", accessBindingName)
		}
		var latestRec *secret.SecretRecord
		if err := precheckVault(pw, func(latest *secret.Vault) error {
			found := latest.Get(accessBindingSecret)
			if found == nil {
				return fmt.Errorf("secret %q not found", accessBindingSecret)
			}
			latestRec = found
			return nil
		}); err != nil {
			return err
		}
		fmt.Println("Review credential binding:")
		fmt.Printf("  Binding:      %s\n", accessBindingName)
		fmt.Printf("  Secret ID:    %s\n", accessBindingSecret)
		fmt.Printf("  Secret label: %s\n", latestRec.Label)
		fmt.Printf("  Secret value: %s\n", secret.MaskSecret(latestRec.Secret))
		fmt.Printf("  Broker access:%s%s\n", map[bool]string{true: " resolve", false: ""}[accessBrokerResolve], map[bool]string{true: " rotate", false: ""}[accessBrokerRotate])
		fmt.Println("  Broker access defaults to deny and is not a sandbox.")
		if !accessBrokerResolve && !accessBrokerRotate {
			fmt.Println("  No policy change; binding metadata will still be created.")
		} else {
			ok, err := confirmPrompt("Type ENABLE to change this secret's broker policy: ", "ENABLE")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("aborted")
			}
			if err := mutateVault("", secret.SessionMutation{
				Mutate: func(latest *secret.Vault) error {
					rec := latest.Get(accessBindingSecret)
					if rec == nil {
						return fmt.Errorf("secret %q not found", accessBindingSecret)
					}
					rec.Policy.AllowBrokerResolve = rec.Policy.AllowBrokerResolve || accessBrokerResolve
					rec.Policy.AllowBrokerRotate = rec.Policy.AllowBrokerRotate || accessBrokerRotate
					rec.Policy.Version = 1
					return nil
				},
			}); err != nil {
				return err
			}
		}
		now := time.Now()
		id, err := broker.NewBindingID()
		if err != nil {
			return err
		}
		if err := broker.MutateBrokerFile(path, func(latest *broker.File) error {
			if latest.FindBinding(accessBindingName) != nil {
				return fmt.Errorf("binding %q already exists", accessBindingName)
			}
			latest.Bindings = append(latest.Bindings, broker.CredentialBinding{
				ID: id, Name: accessBindingName, SecretID: accessBindingSecret, Description: accessBindingDescription, ComponentAllowlist: accessBindingComponents,
				CreatedAt: now, UpdatedAt: now,
			})
			return nil
		}); err != nil {
			return err
		}
		logAudit("credential_binding_created", "", map[string]string{"binding_id": id})
		fmt.Printf("Created binding %s (%s)\n", accessBindingName, id)
		return nil
	},
}

var accessBindingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List credential bindings (metadata only)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, _ := loadBrokerMeta()
		if len(meta.Bindings) == 0 {
			fmt.Println("No bindings.")
			return nil
		}
		fmt.Printf("%-20s %-32s %-20s\n", "NAME", "ID", "SECRET ID")
		for _, b := range meta.Bindings {
			fmt.Printf("%-20s %-32s %-20s\n", b.Name, b.ID, b.SecretID)
		}
		return nil
	},
}

var accessBindingInspectCmd = &cobra.Command{
	Use:   "inspect --binding <name-or-id>",
	Short: "Inspect a binding and its grants",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, _ := loadBrokerMeta()
		binding, err := findBinding(meta, accessBindingID)
		if err != nil {
			return err
		}
		fmt.Printf("Name:       %s\nID:         %s\nSecret ID:  %s\n", binding.Name, binding.ID, binding.SecretID)
		if binding.Description != "" {
			fmt.Printf("Description:%s\n", binding.Description)
		}
		count := 0
		for _, grant := range meta.Grants {
			if grant.BindingID == binding.ID {
				fmt.Printf("Grant:      %s (%s) enabled=%v capabilities=%s\n", grant.Name, grant.ID, grant.Enabled, strings.Join(capabilityStrings(grant.Capabilities), ","))
				count++
			}
		}
		if count == 0 {
			fmt.Println("Grants:     none")
		}
		return nil
	},
}

var accessBindingRebindCmd = &cobra.Command{
	Use:   "rebind --binding <name-or-id> --secret <new-secret-id>",
	Short: "Point an existing binding at a different encrypted vault record",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if accessBindingSecret == "" {
			return fmt.Errorf("--secret is required")
		}
		meta, path := loadBrokerMeta()
		if _, err := findBinding(meta, accessBindingID); err != nil {
			return err
		}
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		if err := precheckVault(pw, func(latest *secret.Vault) error {
			if latest.Get(accessBindingSecret) == nil {
				return fmt.Errorf("secret %q not found", accessBindingSecret)
			}
			return nil
		}); err != nil {
			return err
		}
		oldID, newID := "", ""
		if err := broker.MutateBrokerFile(path, func(latest *broker.File) error {
			var found *broker.CredentialBinding
			for i := range latest.Bindings {
				if latest.Bindings[i].ID == accessBindingID || latest.Bindings[i].Name == accessBindingID {
					found = &latest.Bindings[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("binding %q not found", accessBindingID)
			}
			oldID, newID = found.SecretID, accessBindingSecret
			found.SecretID = accessBindingSecret
			found.UpdatedAt = time.Now()
			for i := range latest.Grants {
				if latest.Grants[i].BindingID == found.ID {
					latest.Grants[i].Enabled = false
				}
			}
			return nil
		}); err != nil {
			return err
		}
		logAudit("credential_binding_rebound", "", map[string]string{"binding_id": accessBindingID, "old_secret_id": oldID, "new_secret_id": newID})
		fmt.Printf("Rebound %s from %s to %s\n", accessBindingID, oldID, newID)
		return nil
	},
}

var accessBindingDeleteCmd = &cobra.Command{
	Use:   "delete --binding <name-or-id>",
	Short: "Delete a binding and its grants",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, path := loadBrokerMeta()
		if _, err := findBinding(meta, accessBindingID); err != nil {
			return err
		}
		if err := broker.MutateBrokerFile(path, func(latest *broker.File) error {
			var found *broker.CredentialBinding
			for i := range latest.Bindings {
				if latest.Bindings[i].ID == accessBindingID || latest.Bindings[i].Name == accessBindingID {
					found = &latest.Bindings[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("binding %q not found", accessBindingID)
			}
			kept := latest.Bindings[:0]
			for _, b := range latest.Bindings {
				if b.ID != found.ID {
					kept = append(kept, b)
				}
			}
			latest.Bindings = kept
			grants := latest.Grants[:0]
			for _, g := range latest.Grants {
				if g.BindingID != found.ID {
					grants = append(grants, g)
				}
			}
			latest.Grants = grants
			return nil
		}); err != nil {
			return err
		}
		logAudit("credential_binding_deleted", "", map[string]string{"binding_id": accessBindingID})
		fmt.Printf("Deleted binding %s\n", accessBindingID)
		return nil
	},
}

var accessAllowCmd = &cobra.Command{
	Use:   "allow --app ./trusted-app --key <vault-id> --capability resolve --expires 1h",
	Short: "Approve one executable for one key with an expiring broker grant",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if accessAllowApp == "" || accessAllowKey == "" || len(accessGrantCapabilities) == 0 {
			return fmt.Errorf("--app, --key, and at least one --capability are required")
		}
		canonical, err := filepath.EvalSymlinks(accessAllowApp)
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		hash, err := broker.HashExecutable(canonical)
		if err != nil {
			return err
		}
		capabilities := make([]broker.Capability, 0, len(accessGrantCapabilities))
		for _, raw := range accessGrantCapabilities {
			capability, err := parseCapability(raw)
			if err != nil {
				return err
			}
			capabilities = append(capabilities, capability)
		}
		if accessAllowPermanent {
			return fmt.Errorf("permanent access requires the explicit access grant command")
		}
		expiration, err := time.Parse(time.RFC3339, accessGrantExpiry)
		if err != nil || !expiration.After(time.Now()) {
			return fmt.Errorf("--expires must be a future RFC3339 timestamp")
		}
		name := filepath.Base(canonical)
		bindingName := "app/" + name
		dir := resolvedConfigDir()
		path := config.BrokerPath(dir)
		if existing, loadErr := broker.LoadBrokerFile(path); loadErr != nil {
			return loadErr
		} else if existing.FindBinding(bindingName) != nil {
			return fmt.Errorf("binding %q already exists; use access grant after reviewing it", bindingName)
		}
		fmt.Printf("Application: %s\nSHA-256: %s\nBinding: %s\nKey: %s\nCapabilities: %s\nExpires: %s\n", canonical, hash, bindingName, accessAllowKey, strings.Join(capabilityStrings(capabilities), ","), expiration.Format(time.RFC3339))
		ok, err := confirmPrompt("Type ALLOW to authorize: ", "ALLOW")
		if err != nil || !ok {
			return fmt.Errorf("aborted")
		}
		// Stage the grant disabled before enabling the secret policy. A crash
		// can leave inactive metadata, never an unexpectedly live grant.
		bindingID, err := broker.NewBindingID()
		if err != nil {
			return err
		}
		grantID, err := broker.NewGrantID()
		if err != nil {
			return err
		}
		now := time.Now()
		if err := broker.MutateBrokerFile(path, func(meta *broker.File) error {
			if meta.FindBinding(bindingName) != nil {
				return fmt.Errorf("binding already exists")
			}
			meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: bindingID, Name: bindingName, SecretID: accessAllowKey, ComponentAllowlist: []string{"primary"}, Description: "Created by access allow", CreatedAt: now, UpdatedAt: now})
			meta.Grants = append(meta.Grants, broker.AccessGrant{ID: grantID, Name: name, BindingID: bindingID, Client: broker.ClientConstraint{UID: os.Getuid(), ExecutablePath: canonical, ExecutableHash: hash}, Capabilities: capabilities, Enabled: false, CreatedAt: now, ExpiresAt: &expiration})
			return nil
		}); err != nil {
			return err
		}
		if err := mutateVault("", secret.SessionMutation{Mutate: func(v *secret.Vault) error {
			rec := v.Get(accessAllowKey)
			if rec == nil {
				return fmt.Errorf("key %q not found", accessAllowKey)
			}
			for _, capability := range capabilities {
				if capability == broker.CapabilityResolve {
					rec.Policy.AllowBrokerResolve = true
				}
				if capability == broker.CapabilityRotate {
					rec.Policy.AllowBrokerRotate = true
				}
			}
			rec.Policy.Version = 1
			return nil
		}}); err != nil {
			return err
		}
		if err := broker.MutateBrokerFile(path, func(meta *broker.File) error {
			for i := range meta.Grants {
				if meta.Grants[i].ID == grantID {
					meta.Grants[i].Enabled = true
				}
			}
			return nil
		}); err != nil {
			return err
		}
		logAudit("credential_access_granted", "", map[string]string{"binding_id": bindingID, "grant_id": grantID})
		fmt.Printf("Granted %s to %s\n", bindingName, name)
		return nil
	},
}

var accessGrantCmd = &cobra.Command{
	Use:   "grant",
	Short: "Grant a local executable access to a binding",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		if accessGrantExec == "" || len(accessGrantCapabilities) == 0 {
			return fmt.Errorf("--exec and at least one --capability are required")
		}
		capabilities := make([]broker.Capability, 0, len(accessGrantCapabilities))
		for _, raw := range accessGrantCapabilities {
			capability, err := parseCapability(raw)
			if err != nil {
				return err
			}
			if capability == broker.CapabilityRotate {
				fmt.Println("WARNING: rotate allows this application to replace credential material.")
				fmt.Println("Confirm only if this executable is trusted for provider-side credential updates.")
			}
			capabilities = append(capabilities, capability)
		}
		canonical, err := filepath.EvalSymlinks(accessGrantExec)
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		hash, err := broker.HashExecutable(canonical)
		if err != nil {
			return err
		}
		meta, path := loadBrokerMeta()
		binding, err := findBinding(meta, accessBindingID)
		if err != nil {
			return err
		}
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		var rec *secret.SecretRecord
		if err := precheckVault(pw, func(latest *secret.Vault) error {
			found := latest.Get(binding.SecretID)
			if found == nil {
				return fmt.Errorf("binding target %q not found", binding.SecretID)
			}
			rec = found
			return nil
		}); err != nil {
			return err
		}
		var expires *time.Time
		if accessGrantExpiry != "" {
			expiration, err := time.Parse(time.RFC3339, accessGrantExpiry)
			if err != nil {
				return fmt.Errorf("invalid --expires (want RFC3339): %w", err)
			}
			if !expiration.After(time.Now()) {
				return fmt.Errorf("--expires must be in the future")
			}
			expires = &expiration
		}
		fmt.Println("Review access grant:")
		fmt.Printf("  Application:      %s\n", accessGrantName)
		fmt.Printf("  Executable path:  %s\n", canonical)
		fmt.Printf("  SHA-256:          %s\n", hash)
		fmt.Printf("  Binding:          %s\n", binding.Name)
		fmt.Printf("  Secret label:     %s\n", rec.Label)
		fmt.Printf("  Secret value:     %s\n", secret.MaskSecret(rec.Secret))
		fmt.Printf("  Operations:       %s\n", strings.Join(capabilityStrings(capabilities), ","))
		if expires != nil {
			fmt.Printf("  Expiration:       %s\n", expires.Format(time.RFC3339))
		}
		ok, err := confirmPrompt("Type GRANT to authorize this application: ", "GRANT")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("aborted")
		}
		now := time.Now()
		id, err := broker.NewGrantID()
		if err != nil {
			return err
		}
		grant := broker.AccessGrant{ID: id, Name: accessGrantName, BindingID: binding.ID, Client: broker.ClientConstraint{UID: os.Getuid(), ExecutablePath: canonical, ExecutableHash: hash}, Capabilities: capabilities, Enabled: false, CreatedAt: now, ExpiresAt: expires}
		prior := rec.Policy
		err = broker.TransactBrokerFile(path, func(latest *broker.File) error {
			current := latest.FindBinding(binding.ID)
			if current == nil || current.SecretID != binding.SecretID {
				return fmt.Errorf("binding changed during grant setup")
			}
			latest.Grants = append(latest.Grants, grant)
			if err := mutateVault(pw, secret.SessionMutation{Mutate: func(v *secret.Vault) error {
				r := v.Get(binding.SecretID)
				if r == nil {
					return fmt.Errorf("binding target not found")
				}
				for _, c := range capabilities {
					if c == broker.CapabilityResolve {
						r.Policy.AllowBrokerResolve = true
					}
					if c == broker.CapabilityRotate {
						r.Policy.AllowBrokerRotate = true
					}
				}
				r.Policy.Version = 1
				return nil
			}}); err != nil {
				return err
			}
			for i := range latest.Grants {
				if latest.Grants[i].ID == id {
					latest.Grants[i].Enabled = true
					return nil
				}
			}
			return fmt.Errorf("staged grant missing")
		})
		if err != nil {
			_ = mutateVault(pw, secret.SessionMutation{Mutate: func(v *secret.Vault) error {
				if r := v.Get(binding.SecretID); r != nil {
					r.Policy = prior
				}
				return nil
			}})
			return err
		}
		logAudit("credential_access_granted", "", map[string]string{"binding_id": binding.ID, "grant_id": id})
		fmt.Printf("Granted access %s\n", id)
		return nil
	},
}

var accessRevokeCmd = &cobra.Command{
	Use:   "revoke --grant <id>",
	Short: "Revoke an application access grant",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if accessGrantID == "" {
			return fmt.Errorf("--grant is required")
		}
		_, path := loadBrokerMeta()
		bindingID := ""
		if err := broker.MutateBrokerFile(path, func(latest *broker.File) error {
			for i := range latest.Grants {
				if latest.Grants[i].ID == accessGrantID {
					latest.Grants[i].Enabled = false
					bindingID = latest.Grants[i].BindingID
					return nil
				}
			}
			return fmt.Errorf("grant %q not found", accessGrantID)
		}); err != nil {
			return err
		}
		logAudit("credential_access_revoked", "", map[string]string{"binding_id": bindingID, "grant_id": accessGrantID})
		fmt.Printf("Revoked grant %s\n", accessGrantID)
		return nil
	},
}

var accessListCmd = &cobra.Command{
	Use:   "list",
	Short: "List access grants",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, _ := loadBrokerMeta()
		if len(meta.Grants) == 0 {
			fmt.Println("No access grants.")
			return nil
		}
		fmt.Printf("%-22s %-24s %-32s %-24s %s\n", "ID", "NAME", "BINDING ID", "EXECUTABLE", "CAPABILITIES")
		for _, g := range meta.Grants {
			exec := g.Client.ExecutablePath
			fmt.Printf("%-22s %-24s %-32s %-24s %s enabled=%v\n", g.ID, g.Name, g.BindingID, exec, strings.Join(capabilityStrings(g.Capabilities), ","), g.Enabled)
		}
		return nil
	},
}

var accessInspectCmd = &cobra.Command{
	Use:   "inspect --grant <id>",
	Short: "Inspect an access grant",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		meta, _ := loadBrokerMeta()
		for _, grant := range meta.Grants {
			if grant.ID == accessGrantID {
				fmt.Printf("Name:         %s\nID:           %s\nBinding ID:   %s\nExecutable:   %s\nHash:         %s\nUID:          %d\nCapabilities: %s\nEnabled:      %v\n", grant.Name, grant.ID, grant.BindingID, grant.Client.ExecutablePath, grant.Client.ExecutableHash, grant.Client.UID, strings.Join(capabilityStrings(grant.Capabilities), ","), grant.Enabled)
				return nil
			}
		}
		return fmt.Errorf("grant %q not found", accessGrantID)
	},
}

func findBinding(meta *broker.File, nameOrID string) (*broker.CredentialBinding, error) {
	if nameOrID == "" {
		return nil, fmt.Errorf("--binding is required")
	}
	if binding := meta.FindBinding(nameOrID); binding != nil {
		return binding, nil
	}
	for i := range meta.Bindings {
		if meta.Bindings[i].ID == nameOrID {
			return &meta.Bindings[i], nil
		}
	}
	return nil, fmt.Errorf("binding %q not found", nameOrID)
}

func capabilityStrings(caps []broker.Capability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}

func logAudit(event, provider string, metadata map[string]string) {
	audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{Event: event, Provider: provider, Metadata: metadata})
}

func brokerUnixClient(socket string) (*http.Client, error) {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}, nil
}

func init() {
	brokerCmd.AddCommand(brokerServeCmd, brokerStatusCmd)
	accessCmd.AddCommand(accessBindingCmd, accessAllowCmd, accessGrantCmd, accessRevokeCmd, accessListCmd, accessInspectCmd)
	accessAllowCmd.Flags().StringVar(&accessAllowApp, "app", "", "trusted executable path")
	accessAllowCmd.Flags().StringVar(&accessAllowKey, "key", "", "existing vault key ID")
	accessAllowCmd.Flags().StringSliceVar(&accessGrantCapabilities, "capability", nil, "resolve and/or rotate")
	accessAllowCmd.Flags().StringVar(&accessGrantExpiry, "expires", "", "required future RFC3339 expiration")
	accessAllowCmd.Flags().BoolVar(&accessAllowPermanent, "permanent", false, "explicitly request permanent access (rejected by quick workflow)")
	accessBindingCmd.AddCommand(accessBindingAddCmd, accessBindingListCmd, accessBindingInspectCmd, accessBindingRebindCmd, accessBindingDeleteCmd)
	accessBindingAddCmd.Flags().StringVar(&accessBindingName, "name", "", "stable binding name, e.g. athena/openrouter")
	accessBindingAddCmd.Flags().StringVar(&accessBindingSecret, "secret", "", "existing vault secret ID")
	accessBindingAddCmd.Flags().StringVar(&accessBindingDescription, "description", "", "non-secret description")
	accessBindingAddCmd.Flags().StringSliceVar(&accessBindingComponents, "component", []string{"primary"}, "credential components to expose (primary and/or extra component keys)")
	accessBindingAddCmd.Flags().BoolVar(&accessBrokerResolve, "allow-broker-resolve", false, "enable broker resolve policy after confirmation")
	accessBindingAddCmd.Flags().BoolVar(&accessBrokerRotate, "allow-broker-rotate", false, "enable broker rotate policy after confirmation")
	accessBindingInspectCmd.Flags().StringVar(&accessBindingID, "binding", "", "binding name or ID")
	accessBindingRebindCmd.Flags().StringVar(&accessBindingID, "binding", "", "binding name or ID")
	accessBindingRebindCmd.Flags().StringVar(&accessBindingSecret, "secret", "", "new existing vault secret ID")
	accessBindingDeleteCmd.Flags().StringVar(&accessBindingID, "binding", "", "binding name or ID")
	accessGrantCmd.Flags().StringVar(&accessBindingID, "binding", "", "binding name or ID")
	accessGrantCmd.Flags().StringVar(&accessGrantExec, "exec", "", "executable path to pin")
	accessGrantCmd.Flags().StringVar(&accessGrantName, "name", "", "application display name")
	accessGrantCmd.Flags().StringSliceVar(&accessGrantCapabilities, "capability", nil, "resolve and/or rotate")
	accessGrantCmd.Flags().StringVar(&accessGrantExpiry, "expires", "", "RFC3339 expiration")
	accessRevokeCmd.Flags().StringVar(&accessGrantID, "grant", "", "grant ID")
	accessInspectCmd.Flags().StringVar(&accessGrantID, "grant", "", "grant ID")
	rootCmd.AddCommand(brokerCmd, accessCmd)
}

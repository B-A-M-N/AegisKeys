package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegiskeys/internal/broker"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

func TestAccessApprovalStagesThenActivatesAfterConfirmedPolicyChange(t *testing.T) {
	dir := t.TempDir()
	vaultPath := config.VaultPath(dir)
	if err := secret.InitVault(vaultPath, "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "key_1", Label: "Main", Secret: "secret", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "app/main", SecretID: "key_1"})
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	msg := prepareAccessApprovalCmd(dir, "binding_1", key, exe, "resolve", time.Now().Add(time.Hour).Format(time.RFC3339))()
	prepared, ok := msg.(accessApprovalPreparedMsg)
	if !ok {
		t.Fatalf("prepare returned %T", msg)
	}
	staged, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(staged.Grants) != 1 || staged.Grants[0].Enabled {
		t.Fatal("grant was not staged disabled")
	}
	done := commitStagedAccessCmd(dir, prepared.pending)().(accessMutationDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	final, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(final.Grants) != 1 || !final.Grants[0].Enabled {
		t.Fatal("confirmed grant not activated")
	}
}

func TestAccessApprovalMissingKeyRemovesStagedGrant(t *testing.T) {
	dir := t.TempDir()
	vaultPath := config.VaultPath(dir)
	if err := secret.InitVault(vaultPath, "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "app/missing", SecretID: "key_missing"})
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "app")
	_ = os.WriteFile(exe, []byte("#!/bin/sh\n"), 0700)
	prepared := prepareAccessApprovalCmd(dir, "binding_1", key, exe, "resolve", time.Now().Add(time.Hour).Format(time.RFC3339))().(accessApprovalPreparedMsg)
	done := commitStagedAccessCmd(dir, prepared.pending)().(accessMutationDoneMsg)
	if done.err == nil {
		t.Fatal("missing key accepted")
	}
	final, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(final.Grants) != 0 {
		t.Fatal("failed approval left staged grant")
	}
}

func TestAccessScreenShowsMetadataOnly(t *testing.T) {
	m := newTestModel(t)
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "athena/openrouter", SecretID: "key_1"})
	meta.Grants = append(meta.Grants, broker.AccessGrant{
		ID: "grant_1", Name: "Athena", BindingID: "binding_1",
		Client:       broker.ClientConstraint{UID: 1000, ExecutablePath: "/opt/athena/bin/athena"},
		Capabilities: []broker.Capability{broker.CapabilityResolve}, Enabled: true, CreatedAt: time.Now(),
	})
	if err := broker.SaveBrokerFile(config.BrokerPath(m.configDir), meta); err != nil {
		t.Fatal(err)
	}
	m.brokerMeta = meta
	m.unlocked = true
	m.vaultSession = &vaultSession{vault: &secret.Vault{Keys: []secret.SecretRecord{{ID: "key_1", Label: "Primary", Secret: "raw-never-shown-123456"}}}}
	m.active = screenAccess
	content := stripANSIForTest(m.View().Content)
	if !strings.Contains(content, "athena/openrouter") || !strings.Contains(content, "athena resolve") {
		t.Fatalf("access metadata missing: %q", content)
	}
	if strings.Contains(content, "raw-never-shown") {
		t.Fatalf("access screen exposed sensitive or internal identifier: %q", content)
	}
}

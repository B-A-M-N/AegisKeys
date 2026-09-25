package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aegiskeys/internal/secret"
)

func approvalFixture(t *testing.T) (string, string, [32]byte, *File) {
	t.Helper()
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	if err := secret.InitVault(vaultPath, "password"); err != nil {
		t.Fatal(err)
	}
	_, key, err := secret.LoadVaultWithKey(vaultPath, "password")
	if err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "key", Label: "Key", Secret: "value", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := NewFile()
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "binding", Name: "app/key", SecretID: "key"})
	path := filepath.Join(dir, "broker.json")
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	return path, vaultPath, key, meta
}

func TestFindBindingByIDDoesNotConfuseGeneratedIDWithName(t *testing.T) {
	meta := NewFile()
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "binding_generated", Name: "app/name", SecretID: "key"})
	if got := meta.FindBindingByID("binding_generated"); got == nil || got.Name != "app/name" {
		t.Fatalf("FindBindingByID = %+v", got)
	}
	if got := meta.FindBinding("binding_generated"); got != nil {
		t.Fatalf("name lookup unexpectedly resolved generated ID: %+v", got)
	}
}

func TestInterpreterGrantRefusedByDefault(t *testing.T) {
	if _, err := NewClientConstraint(1000, "/usr/bin/python3", "hash", false); err == nil {
		t.Fatal("interpreter-wide grant was accepted by default")
	}
	constraint, err := NewClientConstraint(1000, "/usr/bin/python3", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if constraint.IdentityScope != IdentityInterpreterWide || !constraint.InterpreterAcknowledged {
		t.Fatalf("bad interpreter scope: %+v", constraint)
	}
}

func TestApprovalCommitCancellationRecomputesPolicyAndRemovesGrant(t *testing.T) {
	path, vaultPath, key, meta := approvalFixture(t)
	exe := filepath.Join(t.TempDir(), "app")
	if err := os.WriteFile(exe, []byte("app"), 0700); err != nil {
		t.Fatal(err)
	}
	hash, err := HashExecutable(exe)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClientConstraint(os.Getuid(), exe, hash, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{
		ID: "grant", Name: "app", BindingID: "binding", Client: client,
		Capabilities: []Capability{CapabilityResolve}, ExpiresAt: timePtr(time.Now().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = CommitApproval(ctx, path, vaultPath, key, "grant")
	if !errors.Is(err, ErrApprovalCancelled) {
		t.Fatalf("CommitApproval error = %v", err)
	}
	got, err := LoadBrokerFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Grants) != 0 || len(got.PendingApprovals) != 0 {
		t.Fatalf("cancelled grant survived: %+v", got)
	}
	v, err := secret.LoadVaultByKey(vaultPath, key)
	if err != nil {
		t.Fatal(err)
	}
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("cancelled approval left broker policy enabled")
	}
	_ = meta
}

func timePtr(t time.Time) *time.Time { return &t }

func TestRecoveryPreservesUnrelatedManualPolicy(t *testing.T) {
	path, vaultPath, key, meta := approvalFixture(t)
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		if err := v.Add(secret.SecretRecord{ID: "other", Label: "Other", Secret: "other-value", Policy: secret.SecretPolicy{
			Version: 1, AllowBrokerResolve: true, AllowBrokerRotate: true,
			BrokerResolveSource: secret.BrokerPolicySourceAdministrative,
			BrokerRotateSource:  secret.BrokerPolicySourceAdministrative,
		}}); err != nil {
			return err
		}
		v.Get("key").Policy.AllowBrokerResolve = true
		v.Get("key").Policy.BrokerResolveSource = secret.BrokerPolicySourceGrant
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "other-binding", Name: "app/other", SecretID: "other"})
	meta.Grants = append(meta.Grants, AccessGrant{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}, CreatedAt: time.Now()})
	meta.PendingApprovals = []ApprovalIntent{{GrantID: "staged", BindingID: "binding", SecretID: "key", Capabilities: []Capability{CapabilityResolve}, CreatedAt: time.Now()}}
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingApprovals(path, vaultPath, key); err != nil {
		t.Fatal(err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("incomplete affected grant policy survived")
	}
	other := v.Get("other").Policy
	if !other.AllowBrokerResolve || !other.AllowBrokerRotate || other.BrokerResolveSource != secret.BrokerPolicySourceAdministrative || other.BrokerRotateSource != secret.BrokerPolicySourceAdministrative {
		t.Fatalf("unrelated administrative policy changed: %+v", other)
	}
}

func TestRecoveryPreservesAdministrativePolicyCapturedForAffectedRecord(t *testing.T) {
	path, vaultPath, key, meta := approvalFixture(t)
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		rec := v.Get("key")
		rec.Policy.AllowBrokerResolve = true
		rec.Policy.BrokerResolveSource = secret.BrokerPolicySourceAdministrative
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	intent, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}})
	if err != nil {
		t.Fatal(err)
	}
	if err := MutateBrokerFile(path, func(f *File) error { p := intent; _ = p; return nil }); err != nil {
		t.Fatal(err)
	}
	// Simulate the policy write from a crashed commit; recovery must restore the
	// captured administrative baseline rather than derive deny from grants.
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		rec := v.Get("key")
		rec.Policy.AllowBrokerResolve = true
		rec.Policy.BrokerResolveSource = secret.BrokerPolicySourceGrant
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingApprovals(path, vaultPath, key); err != nil {
		t.Fatal(err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	p := v.Get("key").Policy
	if !p.AllowBrokerResolve || p.BrokerResolveSource != secret.BrokerPolicySourceAdministrative {
		t.Fatalf("administrative baseline not restored: %+v", p)
	}
	_ = meta
}

func TestRecoveryPreservesConcurrentAdministrativeChange(t *testing.T) {
	path, vaultPath, key, _ := approvalFixture(t)
	if _, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}}); err != nil {
		t.Fatal(err)
	}
	// Administrative policy is enabled after staging, before cancellation.
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		rec := v.Get("key")
		rec.Policy.AllowBrokerRotate = true
		rec.Policy.BrokerRotateSource = secret.BrokerPolicySourceAdministrative
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = CommitApproval(ctx, path, vaultPath, key, "staged")
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	p := v.Get("key").Policy
	if !p.AllowBrokerRotate || p.BrokerRotateSource != secret.BrokerPolicySourceAdministrative {
		t.Fatalf("concurrent administrative policy was cleared: %+v", p)
	}
}

func TestRecoveryKeepsPolicyRequiredByConcurrentGrant(t *testing.T) {
	path, vaultPath, key, meta := approvalFixture(t)
	if _, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}}); err != nil {
		t.Fatal(err)
	}
	meta.Grants = append(meta.Grants, AccessGrant{ID: "concurrent", Name: "concurrent", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/other"}, Capabilities: []Capability{CapabilityResolve}, Enabled: true, CreatedAt: time.Now()})
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	// Simulate the enabled grant's policy write before recovery.
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		rec := v.Get("key")
		rec.Policy.AllowBrokerResolve = true
		rec.Policy.BrokerResolveSource = secret.BrokerPolicySourceGrant
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingApprovals(path, vaultPath, key); err != nil {
		t.Fatal(err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	p := v.Get("key").Policy
	if !p.AllowBrokerResolve || p.BrokerResolveSource != secret.BrokerPolicySourceGrant {
		t.Fatalf("concurrent grant policy lost: %+v", p)
	}
}

func TestRecoverApprovalGrantPreservesUnrelatedManualPolicy(t *testing.T) {
	path, vaultPath, key, meta := approvalFixture(t)
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		if err := v.Add(secret.SecretRecord{ID: "other", Label: "Other", Secret: "other", Policy: secret.SecretPolicy{Version: 1, AllowBrokerResolve: true, BrokerResolveSource: secret.BrokerPolicySourceAdministrative}}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "other-binding", Name: "app/other", SecretID: "other"})
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}}); err != nil {
		t.Fatal(err)
	}
	if err := recoverApprovalGrant(path, vaultPath, key, "staged"); err != nil {
		t.Fatal(err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	p := v.Get("other").Policy
	if !p.AllowBrokerResolve || p.BrokerResolveSource != secret.BrokerPolicySourceAdministrative {
		t.Fatalf("unrelated policy changed during grant cleanup: %+v", p)
	}
}

func TestRecoveryDoesNotResurrectNewerAdministrativeDeny(t *testing.T) {
	path, vaultPath, key, _ := approvalFixture(t)
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, true, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}}); err != nil {
		t.Fatal(err)
	}
	// Newer administrative denial increments revision and wins over staged true.
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, false, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingApprovals(path, vaultPath, key); err != nil {
		t.Fatal(err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("recovery resurrected newer administrative deny")
	}
}

func TestOverlappingIntentsUseLatestAdministrativeRevision(t *testing.T) {
	path, vaultPath, key, _ := approvalFixture(t)
	secret.MarkBrokerAdministrative(&secret.SecretPolicy{}, true, false) // no-op setup marker
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, true, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	i1, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "g1", Name: "g1", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/a"}, Capabilities: []Capability{CapabilityResolve}})
	if err != nil {
		t.Fatal(err)
	}
	i2, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "g2", Name: "g2", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/b"}, Capabilities: []Capability{CapabilityResolve}})
	if err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, false, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingApprovals(path, vaultPath, key); err != nil {
		t.Fatal(err)
	}
	_ = i1
	_ = i2
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("overlapping recovery resurrected permission")
	}
}

func TestRecoverApprovalGrantDoesNotResurrectNewerAdministrativeDeny(t *testing.T) {
	path, vaultPath, key, _ := approvalFixture(t)
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, true, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := StageApproval(context.Background(), path, vaultPath, key, "binding", ApprovalGrantSpec{ID: "staged", Name: "staged", BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app"}, Capabilities: []Capability{CapabilityResolve}}); err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, false, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := recoverApprovalGrant(path, vaultPath, key, "staged"); err != nil {
		t.Fatal(err)
	}
	v, err := secret.LoadVaultByKey(vaultPath, key)
	if err != nil {
		t.Fatal(err)
	}
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("direct approval cleanup resurrected a newer administrative deny")
	}
}

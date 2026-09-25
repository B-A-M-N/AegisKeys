package broker

import (
	"path/filepath"
	"testing"
	"time"

	"aegiskeys/internal/secret"
)

func lifecycleFixture(t *testing.T, bindingID string, grants int) (string, string, [32]byte, *File) {
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
		if err := v.Add(secret.SecretRecord{ID: "key", Label: "Key", Secret: "secret", Policy: secret.SecretPolicy{Version: 1, AllowBrokerResolve: true, BrokerResolveSource: secret.BrokerPolicySourceGrant}}); err != nil {
			return err
		}
		return v.Add(secret.SecretRecord{ID: "other", Label: "Other", Secret: "other", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := NewFile()
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "binding", Name: "app/key", SecretID: bindingID, CreatedAt: time.Now()})
	for i := 0; i < grants; i++ {
		id := "grant-" + string(rune('1'+i))
		meta.Grants = append(meta.Grants, AccessGrant{ID: id, Name: id, BindingID: "binding", Client: ClientConstraint{UID: 1, ExecutablePath: "/app/" + id}, Capabilities: []Capability{CapabilityResolve}, Enabled: true, CreatedAt: time.Now()})
	}
	path := filepath.Join(dir, "broker.json")
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	return path, vaultPath, key, meta
}

func TestRevokeGrantPersistsAndReconcilesOneOfSeveralThenLast(t *testing.T) {
	path, vaultPath, key, _ := lifecycleFixture(t, "key", 2)
	if err := RevokeGrant(path, vaultPath, key, "grant-1"); err != nil {
		t.Fatal(err)
	}
	meta, err := LoadBrokerFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findGrantByID(meta, "grant-1").Enabled {
		t.Fatal("revoked grant remained enabled in broker.json")
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if !v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("revoking one grant cleared policy required by another")
	}
	if err := RevokeGrant(path, vaultPath, key, "grant-2"); err != nil {
		t.Fatal(err)
	}
	v, _ = secret.LoadVaultByKey(vaultPath, key)
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("last grant revocation left grant-sourced policy enabled")
	}
}

func TestRevokeGrantPreservesAdministrativePolicy(t *testing.T) {
	path, vaultPath, key, _ := lifecycleFixture(t, "key", 1)
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		secret.MarkBrokerAdministrative(&v.Get("key").Policy, true, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RevokeGrant(path, vaultPath, key, "grant-1"); err != nil {
		t.Fatal(err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	p := v.Get("key").Policy
	if !p.AllowBrokerResolve || p.BrokerResolveSource != secret.BrokerPolicySourceAdministrative {
		t.Fatalf("administrative policy was lost: %+v", p)
	}
}

func TestReconcileExpiredGrantDisablesAndClearsPolicy(t *testing.T) {
	path, vaultPath, key, meta := lifecycleFixture(t, "key", 1)
	expired := time.Now().Add(-time.Minute)
	meta.Grants[0].ExpiresAt = &expired
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileExpiredGrants(path, vaultPath, key, time.Now()); err != nil {
		t.Fatal(err)
	}
	meta, _ = LoadBrokerFile(path)
	if meta.Grants[0].Enabled {
		t.Fatal("expired grant remained enabled")
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if v.Get("key").Policy.AllowBrokerResolve {
		t.Fatal("expired grant left grant-sourced policy enabled")
	}
}

func TestRebindAndDeleteReconcileFormerTarget(t *testing.T) {
	path, vaultPath, key, _ := lifecycleFixture(t, "key", 1)
	if err := RebindBinding(path, vaultPath, key, "app/key", "other"); err != nil {
		t.Fatal(err)
	}
	meta, _ := LoadBrokerFile(path)
	if meta.Bindings[0].SecretID != "other" || meta.Grants[0].Enabled {
		t.Fatalf("rebind metadata not durable: %+v", meta)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if v.Get("key").Policy.AllowBrokerResolve || v.Get("other").Policy.AllowBrokerResolve {
		t.Fatal("rebind did not reconcile old or new target policy")
	}
	if err := DeleteBinding(path, vaultPath, key, "binding"); err != nil {
		t.Fatal(err)
	}
	meta, _ = LoadBrokerFile(path)
	if len(meta.Bindings) != 0 || len(meta.Grants) != 0 {
		t.Fatalf("delete metadata not durable: %+v", meta)
	}
}

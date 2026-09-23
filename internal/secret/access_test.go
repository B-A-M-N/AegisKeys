package secret

import (
	"path/filepath"
	"testing"
)

func TestAllowAccess_RefreshModels(t *testing.T) {
	allowed := SecretRecord{
		ID:     "k1",
		Policy: SecretPolicy{Version: 1, AllowModelRefresh: true, AllowLaunchInject: true},
	}
	if err := allowed.AllowAccess(AccessRefreshModels); err != nil {
		t.Errorf("expected refresh allowed, got: %v", err)
	}

	denied := SecretRecord{
		ID:     "k2",
		Policy: SecretPolicy{Version: 1, AllowModelRefresh: false, AllowLaunchInject: true},
	}
	if err := denied.AllowAccess(AccessRefreshModels); err == nil {
		t.Error("expected refresh denied when AllowModelRefresh=false")
	}
}

func TestExplicitDenyAllPolicySurvivesSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	if err := InitVault(path, "password"); err != nil {
		t.Fatal(err)
	}
	_, key, err := LoadVaultWithKey(path, "password")
	if err != nil {
		t.Fatal(err)
	}
	if err := MutateVaultWithKey(path, key, func(v *Vault) error {
		return v.Add(SecretRecord{ID: "deny", Kind: SecretAPIKey, Policy: SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	v, err := LoadVaultByKey(path, key)
	if err != nil {
		t.Fatal(err)
	}
	rec := v.Get("deny")
	for _, mode := range []AccessMode{AccessRevealStdout, AccessCopyClipboard, AccessEnvExport, AccessInjectEnv, AccessBrokerResolve, AccessBrokerRotate} {
		if err := rec.AllowAccess(mode); err == nil {
			t.Fatalf("explicit deny-all allowed %s after reload", mode)
		}
	}
}

func TestDefaultSecretPolicy_AllowsModelRefresh(t *testing.T) {
	for _, kind := range []SecretKind{SecretAPIKey, SecretServiceAccount, SecretGeneric} {
		p := DefaultSecretPolicy(kind)
		if !p.AllowModelRefresh {
			t.Errorf("default policy for %s should allow model refresh", kind)
		}
	}
}

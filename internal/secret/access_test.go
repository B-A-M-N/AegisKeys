package secret

import "testing"

func TestAllowAccess_RefreshModels(t *testing.T) {
	allowed := SecretRecord{
		ID:     "k1",
		Policy: SecretPolicy{AllowModelRefresh: true, AllowLaunchInject: true},
	}
	if err := allowed.AllowAccess(AccessRefreshModels); err != nil {
		t.Errorf("expected refresh allowed, got: %v", err)
	}

	denied := SecretRecord{
		ID:     "k2",
		Policy: SecretPolicy{AllowModelRefresh: false, AllowLaunchInject: true},
	}
	if err := denied.AllowAccess(AccessRefreshModels); err == nil {
		t.Error("expected refresh denied when AllowModelRefresh=false")
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

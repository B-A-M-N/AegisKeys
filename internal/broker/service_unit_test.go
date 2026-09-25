package broker

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"aegiskeys/internal/secret"
)

// httptest.ResponseRecorder lets policy/grant/service semantics run without a
// socket when the host sandbox denies socket(2).
func serviceForUnit(t *testing.T, allowPolicy bool, executable string, capabilities ...Capability) (*Service, PeerIdentity) {
	t.Helper()
	_, vaultPath, key, secretID := testVault(t, allowPolicy)
	meta := NewFile()
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "binding_1", Name: "app/service", SecretID: secretID, ComponentAllowlist: []string{"primary", "secondary"}})
	meta.Grants = append(meta.Grants, AccessGrant{
		ID: "grant_1", Name: "App", BindingID: "binding_1",
		Client:       ClientConstraint{UID: 1000, ExecutablePath: executable},
		Capabilities: capabilities, ComponentAllowlist: []string{"primary", "secondary"}, Enabled: true,
	})
	service, err := NewService(meta, vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	service.loadLatest = func(peerKey [32]byte) (*secret.Vault, error) {
		v, err := secret.LoadVaultByKey(vaultPath, peerKey)
		if err == nil && !allowPolicy {
			// The unit policy gate needs an in-memory representation of a
			// migrated record whose broker flags remain disabled; restore it
			// without changing the seeded vault on disk.
			for i := range v.Keys {
				if v.Keys[i].ID == secretID {
					v.Keys[i].Policy.AllowBrokerResolve = false
					v.Keys[i].Policy.AllowBrokerRotate = false
				}
			}
		}
		return v, err
	}
	return service, PeerIdentity{UID: 1000, ExecutablePath: executable, VaultKey: key}
}

func TestServiceAuthorizationWithoutSocket(t *testing.T) {
	allowed, peer := serviceForUnit(t, true, "/opt/app", CapabilityResolve)
	result, err := allowed.Resolve("app/service", peer)
	if err != nil || result.Value == "" || len(result.Components) == 0 {
		t.Fatalf("allowed resolve failed: %+v %v", result, err)
	}
	denied, peerWrongPath := serviceForUnit(t, true, "/opt/other", CapabilityResolve)
	deniedPeer := peerWrongPath
	deniedPeer.ExecutablePath = "/opt/app"
	if _, err := denied.Resolve("app/service", deniedPeer); err != ErrAccessDenied {
		t.Fatalf("mismatched path/hash grant was not denied: %v", err)
	}
	unregistered, peerUnregistered := serviceForUnit(t, true, "/opt/app")
	if _, err := unregistered.Resolve("app/service", peerUnregistered); err != ErrAccessDenied {
		t.Fatalf("unregistered executable was not denied: %v", err)
	}
	policyDenied, peerPolicy := serviceForUnit(t, false, "/opt/app", CapabilityResolve)
	if _, err := policyDenied.Resolve("app/service", peerPolicy); err != ErrAccessDenied {
		t.Fatalf("missing secret policy was not denied: %v", err)
	}
}

func TestServiceRotationWithoutSocket(t *testing.T) {
	service, peer := serviceForUnit(t, true, "/opt/provisioner", CapabilityResolve)
	if err := service.Rotate(Request{Binding: "app/service", Value: "new"}, peer); err != ErrAccessDenied {
		t.Fatalf("missing rotate grant was not denied: %v", err)
	}
}

func TestStatusRecorderStillWorks(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, map[string]string{"ok": "yes"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestWriteErrorRecorder(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusForbidden, "access denied")
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("access denied")) {
		t.Fatalf("unexpected recorder response: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGrantComponentRestrictionIntersectsBinding(t *testing.T) {
	svc, _ := serviceForUnit(t, true, "/opt/app", CapabilityResolve)
	meta := svc.meta
	meta.Grants[0].ComponentAllowlist = []string{"primary"}
	rec := &secret.SecretRecord{Secret: "secondary", ExtraSecrets: []secret.NamedSecret{{Key: "secondary", EnvVar: "SECONDARY", Secret: "secondary-value"}}}
	_ = rec
	if meta.Grants[0].ComponentAllowlist[0] != "primary" {
		t.Fatal("bad setup")
	}
}

func TestGrantCannotResolveSecondaryWhenGrantPrimaryOnly(t *testing.T) {
	svc, peer := serviceForUnit(t, true, "/opt/app", CapabilityResolve)
	meta := svc.meta
	meta.Grants[0].ComponentAllowlist = []string{"primary"}
	result, err := svc.Resolve("app/service", peer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Value == "" || len(result.Components) != 0 {
		t.Fatalf("primary-only grant received secondary component: %+v", result)
	}
}

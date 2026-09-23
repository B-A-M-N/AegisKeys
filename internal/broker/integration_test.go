package broker_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aegiskeys/internal/broker"
	"aegiskeys/internal/secret"
)

type integrationAudit struct {
	mu sync.Mutex
	e  []string
}

func (a *integrationAudit) Log(event, result string, metadata map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.e = append(a.e, event+"|"+result)
}

func (a *integrationAudit) events() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.e...)
}

type fakePeerResolver struct{ identity broker.PeerIdentity }

func (r fakePeerResolver) Resolve(net.Conn) (broker.PeerIdentity, error) { return r.identity, nil }

func newE2ESession(t *testing.T) (*broker.Session, string, [32]byte, *integrationAudit, string) {
	t.Helper()
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	if err := secret.InitVault(vaultPath, "e2e-password"); err != nil {
		t.Fatal(err)
	}
	_, key, err := secret.LoadVaultWithKey(vaultPath, "e2e-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{
			ID: "key_e2e", Label: "E2E", Secret: "e2e-primary-secret", EnvVarHint: "E2E_API_KEY",
			Policy: secret.SecretPolicy{Version: 1, AllowLaunchInject: true, AllowBrokerResolve: true, AllowBrokerRotate: true},
		})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_e2e", Name: "athena/e2e", SecretID: "key_e2e"})
	meta.Grants = append(meta.Grants, broker.AccessGrant{
		ID: "grant_e2e", Name: "Athena", BindingID: "binding_e2e",
		Client: broker.ClientConstraint{
			UID: os.Getuid(), ExecutablePath: "/opt/athena/current/athena", ExecutableHash: "e2e-hash",
		},
		Capabilities: []broker.Capability{broker.CapabilityResolve, broker.CapabilityRotate},
		Enabled:      true, CreatedAt: time.Now(),
	})
	if err := broker.SaveBrokerFile(filepath.Join(dir, "broker.json"), meta); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "broker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("sandbox denies Unix socket listener: %v", err)
	}
	auditLog := &integrationAudit{}
	peer := fakePeerResolver{identity: broker.PeerIdentity{
		PID: os.Getpid(), UID: os.Getuid(), GID: os.Getgid(),
		ExecutablePath: "/opt/athena/current/athena", ExecutableHash: "e2e-hash",
	}}
	session, err := broker.NewSession(listener, meta, vaultPath, key, peer, auditLog)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = session.Serve() }()
	t.Cleanup(func() { _ = session.Close() })
	waitPath(t, socket)
	return session, vaultPath, key, auditLog, socket
}

func waitPath(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("socket did not appear")
}

func post(t *testing.T, socket, path, body string) (int, string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://broker"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

func TestBrokerUnixSocketE2E(t *testing.T) {
	session, vaultPath, key, auditLog, socket := newE2ESession(t)
	_ = session
	code, body := post(t, socket, "/v1/resolve", `{"binding":"athena/e2e"}`)
	if code != 200 || !strings.Contains(body, "e2e-primary-secret") {
		t.Fatalf("resolve=%d %s", code, body)
	}
	code, body = post(t, socket, "/v1/rotate", `{"binding":"athena/e2e","value":"e2e-rotated-secret"}`)
	if code != 200 {
		t.Fatalf("rotate=%d %s", code, body)
	}
	v, err := secret.LoadVaultByKey(vaultPath, key)
	if err != nil {
		t.Fatal(err)
	}
	rec := v.Get("key_e2e")
	if rec == nil || rec.Secret != "e2e-rotated-secret" || rec.Label != "E2E" {
		t.Fatalf("wrong rotate result: %+v", rec)
	}
	events := strings.Join(auditLog.events(), "\n")
	if strings.Contains(events, "e2e-primary-secret") || strings.Contains(events, "e2e-rotated-secret") {
		t.Fatalf("audit captured secret material: %s", events)
	}
	for _, want := range []string{"credential_resolved|ok", "credential_rotation_requested|ok", "credential_rotated|ok"} {
		if !strings.Contains(events, want) {
			t.Fatalf("audit missing %q in %s", want, events)
		}
	}
	_ = json.Marshal
}

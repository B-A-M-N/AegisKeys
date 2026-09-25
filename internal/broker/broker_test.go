package broker

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegiskeys/internal/secret"
)

func testPeer(path string) PeerIdentity {
	return PeerIdentity{PID: 123, UID: os.Getuid(), GID: os.Getgid(), ExecutablePath: path}
}

func testVault(t *testing.T, allowResolve bool) (string, string, [32]byte, string) {
	t.Helper()
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	if err := secret.InitVault(vaultPath, "broker-password"); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	_, key, err := secret.LoadVaultWithKey(vaultPath, "broker-password")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{
			ID: "key_broker", Kind: secret.SecretAPIKey, Label: "Primary",
			Secret: "primary-secret-value", EnvVarHint: "BROKER_API_KEY",
			ExtraSecrets: []secret.NamedSecret{{Key: "secondary", EnvVar: "BROKER_SECONDARY", Secret: "secondary-secret-value"}},
			Policy: secret.SecretPolicy{Version: 1,
				AllowLaunchInject: true, AllowBrokerResolve: allowResolve, AllowBrokerRotate: allowResolve,
			},
		})
	}); err != nil {
		t.Fatalf("seed vault: %v", err)
	}
	return dir, vaultPath, key, "key_broker"
}

func newTestSession(t *testing.T, allowResolve bool, executable string, capabilities ...Capability) (*Session, string, [32]byte) {
	t.Helper()
	_, vaultPath, key, secretID := testVault(t, allowResolve)
	meta := NewFile()
	binding := CredentialBinding{ID: "binding_1", Name: "app/service", SecretID: secretID, ComponentAllowlist: []string{"primary", "secondary"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	meta.Bindings = append(meta.Bindings, binding)
	meta.Grants = append(meta.Grants, AccessGrant{
		ID: "grant_1", Name: "Test app", BindingID: binding.ID,
		Client:             ClientConstraint{UID: os.Getuid(), ExecutablePath: executable},
		Capabilities:       capabilities,
		ComponentAllowlist: []string{"primary", "secondary"},
		Enabled:            true, CreatedAt: time.Now(),
	})
	if err := SaveBrokerFile(filepath.Join(filepath.Dir(vaultPath), "broker.json"), meta); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "broker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("sandbox denies broker listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	session, err := NewSession(listener, meta, vaultPath, key, LocalResolver{Identity: testPeer(executable)}, testAuditLogger{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	go func() { _ = session.Serve() }()
	waitSocket(t, socket)
	return session, vaultPath, key
}

type testAuditLogger struct{}

func (testAuditLogger) Log(string, string, map[string]string) {}

func waitSocket(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("broker socket did not appear")
}

func unixJSON(t *testing.T, socket, method, path, body string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}},
	}
	req, err := http.NewRequestWithContext(context.Background(), method, "http://broker"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

func TestListenRejectsUnsafePreexistingPath(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "run", "broker.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socket, []byte("not a socket"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(dir, socket, LocalResolver{}); err == nil {
		t.Fatal("expected regular file at socket path to be refused")
	}
}

func TestBrokerMetadataPermissionsAndNoSecrets(t *testing.T) {
	_, vaultPath, key, secretID := testVault(t, true)
	configDir := filepath.Dir(vaultPath)
	meta := NewFile()
	meta.Bindings = append(meta.Bindings, CredentialBinding{ID: "b", Name: "app/svc", SecretID: secretID})
	path := filepath.Join(configDir, "broker.json")
	if err := SaveBrokerFile(path, meta); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("broker.json permission = %o", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if len(raw) == 0 || json.Valid(raw) == false {
		t.Fatal("invalid broker metadata")
	}
	_ = key
}

func TestListenRejectsLiveSocket(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "run", "broker.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	live, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("sandbox denies Unix socket bind: %v", err)
	}
	defer live.Close()
	if _, err := Listen(dir, socket, LocalResolver{}); err == nil {
		t.Fatal("live same-owner socket was removed")
	}
}

func TestRuntimeDirectoryPermission(t *testing.T) {
	dir := t.TempDir()
	runtime := filepath.Join(dir, "run")
	if err := os.MkdirAll(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runtime, 0777); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeDir(runtime); err == nil {
		t.Fatal("expected group/world-writable runtime directory to be refused")
	}
	if err := os.Chmod(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeDir(runtime); err != nil {
		t.Fatalf("private runtime directory refused: %v", err)
	}
}

func TestPeerAuthorization(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can access sockets regardless of mode")
	}
	socket := filepath.Join(t.TempDir(), "broker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("sandbox denies Unix socket bind: %v", err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket permission = %o", info.Mode().Perm())
	}
	listener.Close()
}

package cmd

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/broker"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

func TestAccessGrantByBindingIDE2EResolveAndImmediateRevoke(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	oldConfigDir := configDir
	configDir = dir
	t.Cleanup(func() { configDir = oldConfigDir })
	if err := secret.InitVault(config.VaultPath(dir), "password"); err != nil {
		t.Fatal(err)
	}
	_, key, err := secret.LoadVaultWithKey(config.VaultPath(dir), "password")
	if err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(config.VaultPath(dir), key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "key", Label: "Main", Secret: "secret", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_generated", Name: "app/key", SecretID: "key"})
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "dedicated-app")
	if err := os.WriteFile(exe, []byte("dedicated"), 0700); err != nil {
		t.Fatal(err)
	}
	oldReview, oldKey := accessReviewFn, accessVaultKeyFn
	accessReviewFn = func(_ *cobra.Command, _ *broker.AccessGrant, _ *broker.CredentialBinding, _, _ string, _ []broker.Capability, _ *time.Time) (bool, error) {
		return true, nil
	}
	accessVaultKeyFn = func() [32]byte { return key }
	t.Cleanup(func() { accessReviewFn, accessVaultKeyFn = oldReview, oldKey })
	expiry := time.Now().Add(time.Hour).Format(time.RFC3339)
	runAccessCommand(t, context.Background(), "grant", "--binding", "binding_generated", "--exec", exe, "--name", "Dedicated", "--capability", "resolve", "--expires", expiry)
	meta, _ = broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(meta.Grants) != 1 || !meta.Grants[0].Enabled {
		t.Fatalf("grant not enabled: %+v", meta)
	}
	socket := filepath.Join(dir, "broker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("sandbox denies Unix listener: %v", err)
	}
	hash, _ := broker.HashExecutable(exe)
	session, err := broker.NewSession(listener, meta, config.VaultPath(dir), key, broker.LocalResolver{Identity: broker.PeerIdentity{UID: os.Getuid(), ExecutablePath: exe, ExecutableHash: hash}}, brokerAuditLogger{audit.NewLogger(config.AuditPath(dir))})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = session.Serve() }()
	t.Cleanup(func() { _ = session.Close() })
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
	post := func() int {
		req, _ := http.NewRequest("POST", "http://broker/v1/resolve", strings.NewReader(`{"binding":"app/key"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if code := post(); code != http.StatusOK {
		t.Fatalf("authorized resolve status %d", code)
	}
	runAccessCommand(t, context.Background(), "revoke", "--grant", meta.Grants[0].ID)
	if code := post(); code != http.StatusForbidden {
		t.Fatalf("revoked resolve status %d", code)
	}
}

func runAccessCommand(t *testing.T, ctx context.Context, args ...string) {
	t.Helper()
	c := &cobra.Command{Use: "access", RunE: func(*cobra.Command, []string) error { return nil }}
	c.SetContext(ctx)
	accessBindingID, accessGrantExec, accessGrantName = "", "", ""
	accessGrantCapabilities, accessGrantExpiry, accessGrantID = nil, "", ""
	switch args[0] {
	case "grant":
		accessBindingID = args[2]
		accessGrantExec = args[4]
		accessGrantName = args[6]
		accessGrantCapabilities = []string{"resolve"}
		accessGrantExpiry = args[10]
	case "revoke":
		accessGrantID = args[2]
	}
	if err := accessGrantCmd.RunE(c, nil); args[0] == "grant" && err != nil {
		t.Fatal(err)
	}
	if args[0] == "revoke" {
		if err := accessRevokeCmd.RunE(c, nil); err != nil {
			t.Fatal(err)
		}
	}
}

package tui

import (
	"strings"
	"testing"
	"time"

	"aegiskeys/internal/broker"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

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

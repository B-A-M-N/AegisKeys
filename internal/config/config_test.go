package config

import "testing"

func TestConfigDefaults_MigrateV1ButPreserveV2ZeroTTL(t *testing.T) {
	old := Config{Version: 1, Theme: "dark"}
	migrated := old.WithDefaults()
	if migrated.Version != 2 {
		t.Fatalf("expected schema migration to v2, got %d", migrated.Version)
	}
	if migrated.Theme != "vault" {
		t.Fatalf("expected legacy dark theme to normalize to vault, got %q", migrated.Theme)
	}
	if migrated.ClipboardTTLSeconds == 0 {
		t.Fatal("expected v1 config to receive clipboard TTL default")
	}

	current := Config{Version: 2, Theme: "vault", ClipboardTTLSeconds: 0, AdapterVerifyTimeoutSeconds: 30}
	current = current.WithDefaults()
	if current.ClipboardTTLSeconds != 0 {
		t.Fatalf("expected explicit v2 zero TTL to be preserved, got %d", current.ClipboardTTLSeconds)
	}
}

package secret

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadVaultWithKey_DerivesSameKey verifies the key derivation is deterministic.
func TestLoadVaultWithKey_DerivesSameKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "correct-horse-battery-staple"

	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}

	v1, key1, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}
	_, key2, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}

	// Same password + same salt = same derived key.
	if key1 != key2 {
		t.Error("key derivation should be deterministic for same salt")
	}
	_ = v1
}

// TestSaveVaultWithKey_Persists verifies WriteWithKey round-trips.
func TestSaveVaultWithKey_Persists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password"

	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}

	v, key, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}

	// Add a record and save with the key.
	rec := SecretRecord{ID: "key_test", Secret: "sk-test-secret-value", ProviderSlug: "openai"}
	if err := v.Add(rec); err != nil {
		t.Fatal(err)
	}
	if err := SaveVaultWithKey(path, key, v); err != nil {
		t.Fatal(err)
	}

	// Reload and verify.
	v2, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}
	got := v2.Get("key_test")
	if got == nil || got.Secret != "sk-test-secret-value" {
		t.Error("secret not persisted through WriteWithKey round-trip")
	}
}

// TestSaveVaultWithKey_0600 verifies the saved file has tight permissions.
func TestSaveVaultWithKey_0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password"

	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}
	v, key, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveVaultWithKey(path, key, v); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("vault file perm = %o, want 600", perm)
	}
}

func TestSaveVaultWithKeyRefusesStaleKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password"

	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}
	v, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}

	var staleKey [32]byte
	if err := SaveVaultWithKey(path, staleKey, v); err == nil {
		t.Fatal("expected SaveVaultWithKey to refuse overwriting an existing vault with a stale key")
	}
	if _, _, err := LoadVaultWithKey(path, pw); err != nil {
		t.Fatalf("vault should remain readable after refused stale-key save: %v", err)
	}
}

func TestSaveVaultRefusesWrongPasswordOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password"

	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}
	v, err := LoadVault(path, pw)
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveVault(path, "wrong-password", v); err == nil {
		t.Fatal("expected SaveVault to refuse overwriting an existing vault that cannot be opened")
	}
	if _, err := LoadVault(path, pw); err != nil {
		t.Fatalf("vault should remain readable after refused wrong-password save: %v", err)
	}
}

func TestMigrateToKeyringRequiredDisablesPasswordUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	const password = "test-password-keyring"
	if err := InitVault(path, password); err != nil {
		t.Fatal(err)
	}
	key, err := RandomVaultKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateToKeyringRequiredWithKey(path, password, key); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVault(path, password); err == nil {
		t.Fatal("password unlocked keyring-required vault")
	}
	if _, err := LoadVaultByKey(path, key); err != nil {
		t.Fatalf("keyring key did not unlock vault: %v", err)
	}
}

func TestSaveVaultWithKeyPreservesKeyringMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	const password = "test-password-keyring"
	if err := InitVault(path, password); err != nil {
		t.Fatal(err)
	}
	key, err := RandomVaultKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateToKeyringRequiredWithKey(path, password, key); err != nil {
		t.Fatal(err)
	}
	v, err := LoadVaultByKey(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Add(SecretRecord{ID: "key_after_migration", Secret: "sk-test", ProviderSlug: "openai"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveVaultWithKey(path, key, v); err != nil {
		t.Fatal(err)
	}
	if mode, err := VaultKeyMode(path); err != nil || mode != "keyring" {
		t.Fatalf("vault mode after save = %q, %v; want keyring", mode, err)
	}
	if _, err := LoadVault(path, password); err == nil {
		t.Fatal("password unlocked keyring-required vault after save")
	}
	if got, err := LoadVaultByKey(path, key); err != nil || got.Get("key_after_migration") == nil {
		t.Fatalf("keyring vault not readable after save: %v", err)
	}
}

// TestDeriveKey_KnownSalt verifies DeriveKey produces consistent output.
func TestDeriveKey_KnownSalt(t *testing.T) {
	salt := "c29tZS1zYWx0LTEyMw==" // base64 "some-salt-123"
	key1, err := DeriveKey("password", salt)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := DeriveKey("password", salt)
	if err != nil {
		t.Fatal(err)
	}
	if key1 != key2 {
		t.Error("DeriveKey should be deterministic")
	}
	if key1 == [32]byte{} {
		t.Error("derived key should not be all zeros")
	}
}

// TestSealOpenWithKey_RoundTrip verifies SealWithKey + OpenWithKey works.
func TestSealOpenWithKey_RoundTrip(t *testing.T) {
	key, err := DeriveKey("password", "c29tZS1zYWx0LTEyMw==")
	if err != nil {
		t.Fatal(err)
	}
	plaintext := `{"secret": "sk-abc123"}`
	env, err := SealWithKey(key, plaintext, "", KDFParams{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenWithKey(key, env)
	if err != nil {
		t.Fatal(err)
	}
	if got != plaintext {
		t.Errorf("round-trip failed: got %q", got)
	}
}

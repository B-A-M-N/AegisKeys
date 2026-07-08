package secret

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "<hidden>"},
		{"abc", "<hidden>"},
		{"12345678", "<hidden>"},
		{"123456789", "...6789"},
		{"sk-test-1234567890abcdef", "...cdef"},
		{"sk-or-v1-abcdef1234567890", "...7890"},
	}
	for _, c := range cases {
		if got := MaskSecret(c.in); got != c.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToMasked(t *testing.T) {
	rec := SecretRecord{
		ID:           "key_abc123",
		ProviderSlug: "openai",
		Label:        "main",
		Secret:       "sk-1234567890abcdef",
	}
	m := ToMasked(rec)
	if m.ID != rec.ID {
		t.Errorf("ID = %q, want %q", m.ID, rec.ID)
	}
	if m.MaskedSecret != "...cdef" {
		t.Errorf("MaskedSecret = %q, want %q", m.MaskedSecret, "sk-1...cdef")
	}
	if m.LastUsed != "never" {
		t.Errorf("LastUsed = %q, want never", m.LastUsed)
	}
}

func TestVaultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "correct-horse-battery-staple"

	// Init + add.
	if err := InitVault(path, pw); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	v, err := LoadVault(path, pw)
	if err != nil {
		t.Fatalf("LoadVault after init: %v", err)
	}
	if len(v.Keys) != 0 {
		t.Fatalf("expected empty vault, got %d keys", len(v.Keys))
	}

	// Add a key and re-save.
	rec := SecretRecord{ProviderSlug: "openai", Label: "main", Secret: "sk-live-abcdef123456"}
	if err := v.Add(rec); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if v.Keys[0].ID == "" {
		t.Fatal("Add did not assign an ID")
	}
	if err := SaveVault(path, pw, v); err != nil {
		t.Fatalf("SaveVault: %v", err)
	}

	// Reload and verify.
	v2, err := LoadVault(path, pw)
	if err != nil {
		t.Fatalf("LoadVault after save: %v", err)
	}
	if len(v2.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(v2.Keys))
	}
	if v2.Keys[0].Secret != rec.Secret {
		t.Errorf("secret = %q, want %q", v2.Keys[0].Secret, rec.Secret)
	}
	if v2.Keys[0].ProviderSlug != "openai" {
		t.Errorf("provider = %q, want openai", v2.Keys[0].ProviderSlug)
	}

	// Wrong password must fail.
	if _, err := LoadVault(path, "wrong-password"); err == nil {
		t.Error("expected error for wrong password, got nil")
	}

	// File permissions must be 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("vault perm = %o, want 600", info.Mode().Perm())
	}
}

func TestVaultCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password-123"

	if err := InitVault(path, pw); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	v, _ := LoadVault(path, pw)

	// Add two keys.
	v.Add(SecretRecord{ProviderSlug: "openai", Secret: "sk-aaaa"})
	v.Add(SecretRecord{ProviderSlug: "anthropic", Secret: "sk-bbbb"})
	id1 := v.Keys[0].ID
	id2 := v.Keys[1].ID

	// Get.
	if v.Get(id1) == nil {
		t.Error("Get(id1) returned nil")
	}
	if v.Get("nonexistent") != nil {
		t.Error("Get(nonexistent) should return nil")
	}

	// Rotate.
	if err := v.Rotate(id1, "sk-aaaa-rotated"); err != nil {
		t.Errorf("Rotate: %v", err)
	}
	if v.Get(id1).Secret != "sk-aaaa-rotated" {
		t.Errorf("after rotate, secret = %q", v.Get(id1).Secret)
	}

	// Remove.
	if err := v.Remove(id2); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if len(v.Keys) != 1 {
		t.Errorf("after remove, len = %d, want 1", len(v.Keys))
	}
	if err := v.Remove(id2); err == nil {
		t.Error("Remove of missing key should error")
	}

	// Duplicate add should fail.
	if err := v.Add(SecretRecord{ID: id1, Secret: "dup"}); err == nil {
		t.Error("Add with duplicate ID should error")
	}
}

func TestSecretNeverSerialized(t *testing.T) {
	// Vault.Serialize must omit the secret field.
	v := &Vault{Version: 1}
	v.Add(SecretRecord{ProviderSlug: "openai", Secret: "sk-should-not-appear"})
	data, err := v.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if contains(string(data), "sk-should-not-appear") {
		t.Errorf("Serialize leaked secret: %s", data)
	}
}

func TestNeedsRekey(t *testing.T) {
	// Current envelope with proper params → no rekey needed.
	good := &VaultEnvelope{KDF: "argon2id", KDFParams: DefaultArgon2Params}
	if NeedsRekey(good) {
		t.Error("should not need rekey with current params")
	}

	// Wrong KDF → rekey.
	wrongKDF := &VaultEnvelope{KDF: "argon2i", KDFParams: DefaultArgon2Params}
	if !NeedsRekey(wrongKDF) {
		t.Error("should need rekey when KDF is not argon2id")
	}

	// Old envelope (no params) → rekey.
	old := &VaultEnvelope{KDF: "argon2id"}
	if !NeedsRekey(old) {
		t.Error("should need rekey when params missing (old envelope)")
	}

	// Low time cost → rekey.
	lowTime := &VaultEnvelope{KDF: "argon2id", KDFParams: KDFParams{Time: 1, MemoryKiB: 64 * 1024, Threads: 2}}
	if !NeedsRekey(lowTime) {
		t.Error("should need rekey when time cost below policy")
	}

	// Nil → no rekey.
	if NeedsRekey(nil) {
		t.Error("nil envelope should not trigger rekey")
	}
}

func TestArgon2idKey_UsesIDKey(t *testing.T) {
	// Verify that the derived key matches argon2.IDKey output (not argon2.Key).
	salt := make([]byte, Argon2SaltLen)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	saltB64 := base64.StdEncoding.EncodeToString(salt)
	pw := "test-password"

	got, err := argon2idKey(pw, saltB64, DefaultArgon2Params)
	if err != nil {
		t.Fatalf("argon2idKey: %v", err)
	}

	want := argon2.IDKey([]byte(pw), salt,
		DefaultArgon2Params.Time,
		DefaultArgon2Params.MemoryKiB,
		DefaultArgon2Params.Threads,
		DefaultArgon2Params.KeyLen)

	if string(got) != string(want) {
		t.Error("argon2idKey output does not match argon2.IDKey — wrong function used")
	}
}

func TestValidateEnvelope(t *testing.T) {
	// Valid envelope.
	good := &VaultEnvelope{
		Version:    1,
		KDF:        "argon2id",
		KDFParams:  DefaultArgon2Params,
		Salt:       base64.StdEncoding.EncodeToString(make([]byte, 16)),
		Nonce:      base64.StdEncoding.EncodeToString(make([]byte, 12)),
		Ciphertext: "dGVzdA==",
	}
	if err := ValidateEnvelope(good); err != nil {
		t.Errorf("expected valid envelope: %v", err)
	}

	// Wrong version.
	badVersion := *good
	badVersion.Version = 2
	if err := ValidateEnvelope(&badVersion); err == nil {
		t.Error("expected error for wrong version")
	}

	// Wrong KDF.
	badKDF := *good
	badKDF.KDF = "argon2i"
	if err := ValidateEnvelope(&badKDF); err == nil {
		t.Error("expected error for wrong KDF")
	}

	// Bad nonce length.
	badNonce := *good
	badNonce.Nonce = base64.StdEncoding.EncodeToString(make([]byte, 8))
	if err := ValidateEnvelope(&badNonce); err == nil {
		t.Error("expected error for bad nonce length")
	}

	// Memory too high.
	badMem := *good
	badMem.KDFParams.MemoryKiB = 999 * 1024
	if err := ValidateEnvelope(&badMem); err == nil {
		t.Error("expected error for excessive memory")
	}

	// Nil envelope.
	if err := ValidateEnvelope(nil); err == nil {
		t.Error("expected error for nil envelope")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestTamperedCiphertextFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password-123"
	if err := InitVault(path, pw); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	// Tamper with the ciphertext bytes.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Flip some bytes near the end (ciphertext region).
	for i := len(data) - 10; i < len(data)-5; i++ {
		data[i] ^= 0xFF
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadVault(path, pw); err == nil {
		t.Error("expected tampered vault to fail decryption")
	}
}

func TestRekeyVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "test-password-rekey"

	if err := InitVault(path, pw); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	// Add a key so we verify the plaintext survives the rekey.
	v, key, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("LoadVaultWithKey: %v", err)
	}
	if err := v.Add(SecretRecord{Kind: SecretAPIKey, Label: "main", Secret: "sk-test-abcdefghijklmnop"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := SaveVaultWithKey(path, key, v); err != nil {
		t.Fatalf("SaveVaultWithKey: %v", err)
	}

	// Rekey with higher Argon2 costs.
	result, err := RekeyVault(path, pw, KDFParams{Time: 4, MemoryKiB: 128 * 1024, Threads: 2, KeyLen: 32})
	if err != nil {
		t.Fatalf("RekeyVault: %v", err)
	}
	if result.OldTime == 0 {
		t.Error("expected non-zero OldTime")
	}
	if result.NewTime != 4 {
		t.Errorf("NewTime = %d, want 4", result.NewTime)
	}
	if result.NewMemoryKiB != 128*1024 {
		t.Errorf("NewMemoryKiB = %d, want %d", result.NewMemoryKiB, 128*1024)
	}

	// Vault should load with the new password after rekey.
	v2, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("LoadVaultWithKey after rekey: %v", err)
	}
	if got := v2.Get(v.Keys[0].ID); got == nil || got.Secret != "sk-test-abcdefghijklmnop" {
		t.Errorf("secret did not survive rekey")
	}
}

func TestRekeyVault_WrongPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	if err := InitVault(path, "right-password"); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	if _, err := RekeyVault(path, "wrong-password", KDFParams{Time: 3}); err == nil {
		t.Error("expected rekey with wrong password to fail")
	}
}

func TestClampKDFParams(t *testing.T) {
	cases := []struct {
		in   KDFParams
		want KDFParams
	}{
		{KDFParams{Time: 0, MemoryKiB: 1, KeyLen: 99}, KDFParams{Time: 1, MemoryKiB: 4 * 1024, KeyLen: 32, Threads: 1}},
		{KDFParams{Time: 99, MemoryKiB: 999999, KeyLen: 32}, KDFParams{Time: 99, MemoryKiB: 512 * 1024, KeyLen: 32, Threads: 1}},
		{KDFParams{Time: 3, MemoryKiB: 64 * 1024, KeyLen: 32, Threads: 0}, KDFParams{Time: 3, MemoryKiB: 64 * 1024, KeyLen: 32, Threads: 1}},
	}
	for _, c := range cases {
		got := ClampKDFParams(c.in)
		if got != c.want {
			t.Errorf("ClampKDFParams(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSerialize_IncludesScratchPads(t *testing.T) {
	v := &Vault{
		Version: 2,
		Keys: []SecretRecord{
			{ID: "key_1", Label: "test", Secret: "sk-secret123", Kind: SecretAPIKey},
		},
		ScratchPads: []ScratchPadRecord{
			{ID: "sp_1", Title: "notes", Kind: ScratchPadGeneral, Body: "secret billing notes"},
		},
	}
	data, err := v.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "scratch_pads") {
		t.Error("Serialize must include scratch_pads section")
	}
	if !strings.Contains(s, "sp_1") {
		t.Error("Serialize must include scratchpad IDs")
	}
	if strings.Contains(s, "sk-secret123") {
		t.Error("Serialize must never contain raw secrets")
	}
}

package secret

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSealWithKey_PreservesLegacyKDFMetadata is the invariant that would have
// caught the vault brick bug: when resealing with an existing salt and legacy
// (Time==0) params, the envelope must keep Time==0 so the loader continues to
// use the Argon2i legacy derivation.
func TestSealWithKey_PreservesLegacyKDFMetadata(t *testing.T) {
	key, err := DeriveKey("password", "c29tZS1zYWx0LTEyMw==")
	if err != nil {
		t.Fatal(err)
	}
	plaintext := `{"secret":"legacy-value"}`
	salt := "c29tZS1zYWx0LTEyMw=="

	// Reseal reusing a salt with legacy (Time==0) params — the common TUI save
	// path for an old vault.
	env, err := SealWithKey(key, plaintext, salt, KDFParams{Time: 0})
	if err != nil {
		t.Fatal(err)
	}
	if env.KDFParams.Time != 0 {
		t.Errorf("SealWithKey upgraded legacy Time=0 to Time=%d; must preserve Time=0", env.KDFParams.Time)
	}
	// And the envelope must still open with the same key.
	got, err := OpenWithKey(key, env)
	if err != nil {
		t.Fatal(err)
	}
	if got != plaintext {
		t.Errorf("round-trip failed after legacy preserve: got %q", got)
	}
}

// TestSealWithKey_ResealRoundTripLegacyVault simulates the full dangerous path:
// a legacy vault (Argon2i, Time=0) is loaded, then saved via SaveVaultWithKey,
// then reloaded with the SAME password. The password must keep working.
func TestSealWithKey_ResealRoundTripLegacyVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "legacy-password"

	// Build a legacy envelope manually (Time==0, Argon2i-derived key).
	salt, err := generateSalt()
	if err != nil {
		t.Fatal(err)
	}
	keyRaw, err := argon2iKeyLegacy(pw, salt)
	if err != nil {
		t.Fatal(err)
	}
	var legacyKey [32]byte
	copy(legacyKey[:], keyRaw)
	plaintext := `{"keys":[],"version":1}`
	env, err := SealWithKey(legacyKey, plaintext, salt, KDFParams{Time: 0})
	if err != nil {
		t.Fatal(err)
	}
	rawEnv, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawEnv, 0600); err != nil {
		t.Fatal(err)
	}

	// Load the legacy vault — must succeed via the legacy derivation path.
	v, key, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("loading legacy vault failed: %v", err)
	}

	// Save it again (the TUI save path). This is where the brick bug lived.
	if err := SaveVaultWithKey(path, key, v); err != nil {
		t.Fatal(err)
	}

	// Reload with the SAME password — must still work.
	v2, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("REBRICKED: password stopped working after SaveVaultWithKey round-trip: %v", err)
	}
	if v2 == nil {
		t.Fatal("reloaded vault is nil")
	}
}

// TestSealWithKey_NewVaultUsesDefaults verifies that a genuinely new vault
// (empty salt) still gets DefaultArgon2Params written into the envelope.
func TestSealWithKey_NewVaultUsesDefaults(t *testing.T) {
	key, err := DeriveKey("password", "c29tZS1zYWx0LTEyMw==")
	if err != nil {
		t.Fatal(err)
	}
	env, err := SealWithKey(key, `{"keys":[]}`, "", KDFParams{})
	if err != nil {
		t.Fatal(err)
	}
	if env.KDFParams.Time == 0 {
		t.Error("new vault (empty salt) should get default KDF params, not Time=0")
	}
	if env.KDFParams.Time != DefaultArgon2Params.Time {
		t.Errorf("new vault KDF time = %d, want %d", env.KDFParams.Time, DefaultArgon2Params.Time)
	}
}

// TestSaveVaultWithKeyNeverWritesParamsThatCannotReopen is the meta-invariant:
// after SaveVaultWithKey, the password-derived key implied by the envelope
// metadata MUST decrypt the envelope. This is the core safety property.
func TestSaveVaultWithKeyNeverWritesParamsThatCannotReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "meta-invariant-password"

	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}
	v, key, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatal(err)
	}
	rec := SecretRecord{ID: "k1", Secret: "sk-meta", ProviderSlug: "openai"}
	if err := v.Add(rec); err != nil {
		t.Fatal(err)
	}
	if err := SaveVaultWithKey(path, key, v); err != nil {
		t.Fatal(err)
	}

	// Fresh load using ONLY the envelope's stored metadata + password.
	v2, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("envelope metadata no longer opens the vault: %v", err)
	}
	if got := v2.Get("k1"); got == nil || got.Secret != "sk-meta" {
		t.Fatal("secret lost after save")
	}
}

// TestDiagnoseUnlock_LegacyMismatch verifies the diagnostic detects the exact
// brick state: envelope claims Argon2id/Time=3 but key is Argon2i.
func TestDiagnoseUnlock_LegacyMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "mismatch-password"

	// Build an Argon2i-derived key but seal with Time=3 metadata (the brick state).
	salt := "c29tZS1zYWx0LTEyMw=="
	keyRaw, err := argon2iKeyLegacy(pw, salt)
	if err != nil {
		t.Fatal(err)
	}
	var arg2iKey [32]byte
	copy(arg2iKey[:], keyRaw)
	// Seal with an Argon2i key but label the envelope as Argon2id/Time=3.
	env, err := SealWithKey(arg2iKey, `{"keys":[]}`, salt, DefaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the metadata to mimic the brick state: the key is Argon2i but
	// envelope claims Argon2id Time=3.
	env.KDFParams = DefaultArgon2Params
	rawEnv, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawEnv, 0600); err != nil {
		t.Fatal(err)
	}

	canUnlock, legacyWorks, _, err := DiagnoseUnlock(path, pw)
	if err != nil {
		t.Fatal(err)
	}
	if canUnlock {
		t.Error("expected normal unlock to FAIL for mismatched envelope")
	}
	if !legacyWorks {
		t.Error("expected legacy derivation to detect the mismatch")
	}
}

// TestRepairVault_Preserve fixes the brick state with preserve mode.
func TestRepairVault_Preserve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "repair-password"

	salt, err := generateSalt()
	if err != nil {
		t.Fatal(err)
	}
	keyRaw, err := argon2iKeyLegacy(pw, salt)
	if err != nil {
		t.Fatal(err)
	}
	var arg2iKey [32]byte
	copy(arg2iKey[:], keyRaw)
	plaintext := `{"keys":[{"id":"k1","secret":"sk-preserve"}],"version":1}`
	// Seal with Argon2i key but claim Argon2id Time=3.
	env, err := SealWithKey(arg2iKey, plaintext, salt, DefaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	env.KDFParams = DefaultArgon2Params
	rawEnv, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawEnv, 0600); err != nil {
		t.Fatal(err)
	}

	// Confirm it's bricked: normal unlock fails.
	if _, _, err := LoadVaultWithKey(path, pw); err == nil {
		t.Fatal("expected bricked vault to fail normal unlock")
	}

	// Repair with preserve mode.
	result, err := RepairVault(path, pw, RepairPreserve)
	if err != nil {
		t.Fatalf("RepairVault failed: %v", err)
	}
	if !result.Repaired {
		t.Fatal("expected repair to be applied")
	}
	if !result.LegacyDerived {
		t.Fatal("expected legacy derivation to be the working one")
	}

	// Verify: backup file exists.
	matches, _ := filepath.Glob(path + ".bak.*")
	if len(matches) == 0 {
		t.Fatal("expected a backup file to be written")
	}

	// Verify: password now unlocks normally.
	v, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("vault still broken after repair: %v", err)
	}
	if got := v.Get("k1"); got == nil || got.Secret != "sk-preserve" {
		t.Fatal("secret not preserved through repair")
	}
}

// TestRepairVault_Upgrade fixes the brick state with upgrade mode.
func TestRepairVault_Upgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "upgrade-password"

	salt := "c29tZS1zYWx0LTEyMw=="
	keyRaw, err := argon2iKeyLegacy(pw, salt)
	if err != nil {
		t.Fatal(err)
	}
	var arg2iKey [32]byte
	copy(arg2iKey[:], keyRaw)
	plaintext := `{"keys":[{"id":"k1","secret":"sk-upgrade"}],"version":1}`
	env, err := SealWithKey(arg2iKey, plaintext, salt, DefaultArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	env.KDFParams = DefaultArgon2Params
	rawEnv, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawEnv, 0600); err != nil {
		t.Fatal(err)
	}

	result, err := RepairVault(path, pw, RepairUpgrade)
	if err != nil {
		t.Fatalf("RepairVault upgrade failed: %v", err)
	}
	if !result.Repaired {
		t.Fatal("expected repair to be applied")
	}

	// After upgrade the envelope should declare Argon2id/Time>=1 and open fine.
	var env2 VaultEnvelope
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &env2); err != nil {
		t.Fatal(err)
	}
	if env2.KDFParams.Time == 0 {
		t.Error("upgrade mode should move vault off legacy Time=0")
	}

	v, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("vault broken after upgrade: %v", err)
	}
	if got := v.Get("k1"); got == nil || got.Secret != "sk-upgrade" {
		t.Fatal("secret not preserved through upgrade")
	}
}

// TestRepairVault_NoRepairNeeded verifies a healthy vault is left untouched.
func TestRepairVault_NoRepairNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "healthy-password"
	if err := InitVault(path, pw); err != nil {
		t.Fatal(err)
	}
	result, err := RepairVault(path, pw, RepairPreserve)
	if err != nil {
		t.Fatal(err)
	}
	if result.Repaired {
		t.Error("healthy vault should not be repaired")
	}
}

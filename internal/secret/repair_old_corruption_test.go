package secret

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sealWithKeyOLD replicates the BUGGY SealWithKey from before the fix: it
// upgrades Time==0 to DefaultArgon2Params whenever the envelope is resealed,
// even when reusing an existing salt with an already-derived Argon2i key.
// This is the exact corruption that bricked existing vaults.
func sealWithKeyOLD(key [32]byte, plaintext, saltB64 string, originalParams KDFParams) (*VaultEnvelope, error) {
	var salt string
	if saltB64 != "" {
		salt = saltB64
	} else {
		var err error
		salt, err = generateSalt()
		if err != nil {
			return nil, err
		}
	}
	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}
	block, err := newGCMCipher(key[:])
	if err != nil {
		return nil, err
	}
	nonceBytes, err := decodeB64Strict(nonce)
	if err != nil {
		return nil, err
	}
	ciphertext := block.Seal(nil, nonceBytes, []byte(plaintext), nil)
	kdfParams := originalParams
	if kdfParams.Time == 0 { // BUG: upgrades legacy metadata
		kdfParams = DefaultArgon2Params
	}
	return &VaultEnvelope{
		Version:    1,
		KDF:        "argon2id",
		KDFParams:  kdfParams,
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

// TestRepairVault_UndoesExactOldCorruption proves the repair command undoes the
// precise corruption the old buggy SealWithKey inflicted on real vaults.
func TestRepairVault_UndoesExactOldCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := "user-real-password"

	// Step 1: user creates a vault the ORIGINAL way (Argon2i, Time=0).
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
	plaintext := `{"keys":[{"id":"real","secret":"sk-real-secret-value"}],"version":1}`
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

	// Step 2: user unlocks fine (legacy path works).
	v, key, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("initial unlock failed: %v", err)
	}

	// Step 3: user rotates a key (in-memory), then the TUI saves the vault
	// using the OLD buggy SealWithKey → brick. The save marshals the
	// in-memory vault (with the rotated secret), then seals with the buggy
	// path that upgrades Time==0 → Time=3.
	if err := v.Rotate("real", "sk-still-real"); err != nil {
		t.Fatal(err)
	}
	store := toStore(v)
	plaintext2, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	brickedEnv, err := sealWithKeyOLD(key, string(plaintext2), salt, env.KDFParams)
	if err != nil {
		t.Fatal(err)
	}
	rawBricked, err := json.MarshalIndent(brickedEnv, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawBricked, 0600); err != nil {
		t.Fatal(err)
	}

	// Step 4: NOW the user can't unlock — envelope claims Argon2id/Time=3.
	if _, _, err := LoadVaultWithKey(path, pw); err == nil {
		t.Fatal("expected bricked vault to FAIL normal unlock")
	}

	// Step 5: run the repair → unlock works again with the SAME password.
	result, err := RepairVault(path, pw, RepairPreserve)
	if err != nil {
		t.Fatalf("RepairVault failed: %v", err)
	}
	if !result.Repaired {
		t.Fatal("expected repair to be applied")
	}

	// Step 6: unlock with the original password → success.
	v2, _, err := LoadVaultWithKey(path, pw)
	if err != nil {
		t.Fatalf("STILL BRICKED after repair: %v", err)
	}
	if got := v2.Get("real"); got == nil || got.Secret != "sk-still-real" {
		t.Fatal("secret not preserved through repair")
	}
}

package secret

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestVaultFuzz_Envelope is adversarial: constructs malformed envelope
// inputs and asserts ValidateEnvelope fails closed.
func TestVaultFuzz_Envelope(t *testing.T) {
	valid := &VaultEnvelope{
		Version:    1,
		KDF:        "argon2id",
		KDFParams:  DefaultArgon2Params,
		Salt:       base64.StdEncoding.EncodeToString(make([]byte, 16)),
		Nonce:      base64.StdEncoding.EncodeToString(make([]byte, 12)),
		Ciphertext: "dGVzdA==",
	}

	mutations := []struct {
		name   string
		mutate func(e *VaultEnvelope)
	}{
		{"wrong version", func(e *VaultEnvelope) { e.Version = 2 }},
		{"version 0", func(e *VaultEnvelope) { e.Version = 0 }},
		{"wrong KDF", func(e *VaultEnvelope) { e.KDF = "argon2i" }},
		{"empty KDF", func(e *VaultEnvelope) { e.KDF = "" }},
		{"empty ciphertext", func(e *VaultEnvelope) { e.Ciphertext = "" }},
		{"bad base64 salt", func(e *VaultEnvelope) { e.Salt = "##not-base64" }},
		{"empty salt", func(e *VaultEnvelope) { e.Salt = "" }},
		{"short salt (8 bytes)", func(e *VaultEnvelope) {
			e.Salt = base64.StdEncoding.EncodeToString(make([]byte, 8))
		}},
		{"bad base64 nonce", func(e *VaultEnvelope) { e.Nonce = "##bad" }},
		{"wrong nonce length (16)", func(e *VaultEnvelope) {
			e.Nonce = base64.StdEncoding.EncodeToString(make([]byte, 16))
		}},
		{"huge KDF memory (1 GiB)", func(e *VaultEnvelope) {
			e.KDFParams.MemoryKiB = 1024 * 1024
		}},
		{"wrong key length", func(e *VaultEnvelope) { e.KDFParams.KeyLen = 24 }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			e := cloneEnvelope(valid)
			m.mutate(e)
			if err := ValidateEnvelope(e); err == nil {
				t.Errorf("ValidateEnvelope should reject %q, but accepted", m.name)
			}
		})
	}

	if err := ValidateEnvelope(valid); err != nil {
		t.Errorf("valid envelope rejected: %v", err)
	}
	if err := ValidateEnvelope(nil); err == nil {
		t.Error("nil envelope should be rejected")
	}
}

func TestVaultEnvelope_TamperedCiphertext(t *testing.T) {
	pw := "test-password-tamper"
	env, err := SealEnvelope(pw, `{"test":true}`)
	if err != nil {
		t.Fatalf("SealEnvelope: %v", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		t.Fatalf("decode ct: %v", err)
	}
	ct[len(ct)-1] ^= 0xFF
	env.Ciphertext = base64.StdEncoding.EncodeToString(ct)
	if _, err := OpenEnvelope(pw, env); err == nil {
		t.Error("tampered ciphertext should fail decryption, but succeeded")
	}
}

func TestVaultEnvelope_RandomBytes(t *testing.T) {
	pw := "test-password-random"
	randomJSON := make([]byte, 256)
	if _, err := rand.Read(randomJSON); err != nil {
		t.Fatalf("rand: %v", err)
	}
	var parsed VaultEnvelope
	if json.Unmarshal(randomJSON, &parsed) == nil {
		if _, err := OpenEnvelope(pw, &parsed); err == nil {
			t.Error("random bytes envelope should not open successfully")
		}
	}
	// No panic = pass regardless.
}

func cloneEnvelope(e *VaultEnvelope) *VaultEnvelope {
	if e == nil {
		return nil
	}
	c := *e
	return &c
}

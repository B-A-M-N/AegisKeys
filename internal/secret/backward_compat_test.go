package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/argon2"
)

// TestBackwardCompat_OldArgon2i_Unlocks is the real backward-compat proof.
// The original code used argon2.Key (Argon2i) with Time=1. The new code uses
// argon2.IDKey (Argon2id). Old vaults sealed with Argon2i must still unlock.
func TestBackwardCompat_OldArgon2i_Unlocks(t *testing.T) {
	pw := "my-correct-password"
	plaintext := `{"keys":[{"id":"key_old","provider_slug":"openai","label":"legacy","secret":"sk-legacy-123456"}]}`

	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}

	// Seal using the ORIGINAL algorithm: argon2.Key (Argon2i), Time=1.
	key := argon2.Key([]byte(pw), salt, 1, 64*1024, 2, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	// Build an envelope in the OLD format (no KDFParams stored).
	env := &VaultEnvelope{
		Version:    1,
		KDF:        "argon2id",
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		// KDFParams left as zero value (Time==0) = legacy
	}

	// Unlock with the new code path.
	got, err := OpenEnvelope(pw, env)
	if err != nil {
		t.Fatalf("CANNOT UNLOCK OLD VAULT — backward compat broken: %v", err)
	}
	if got != plaintext {
		t.Errorf("plaintext mismatch: got %q want %q", got, plaintext)
	}
}

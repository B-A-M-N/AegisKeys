package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

const (
	Argon2SaltLen = 16
	Argon2KeyLen  = 32
)

// DefaultArgon2Params holds the current KDF cost policy for NEW envelopes.
// NeedsRekey uses these values to detect outdated envelopes.
var DefaultArgon2Params = KDFParams{
	Time:      3,
	MemoryKiB: 64 * 1024,
	Threads:   2,
	KeyLen:    Argon2KeyLen,
}

// legacyArgon2Params matches the hardcoded values used by the original
// SealEnvelope before KDFParams were stored on the envelope. Old vaults
// sealed with Time=1 must fall back to these exact params for decryption.
var legacyArgon2Params = KDFParams{
	Time:      1,
	MemoryKiB: 64 * 1024,
	Threads:   2,
	KeyLen:    Argon2KeyLen,
}

// KDFParams captures the Argon2 cost parameters used to seal an envelope.
// Stored on the envelope so future code can detect stale costs and re-seal.
type KDFParams struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	KeyLen    uint32 `json:"key_len"`
}

type VaultEnvelope struct {
	Version    int       `json:"version"`
	KDF        string    `json:"kdf"` // "argon2id"
	KDFParams  KDFParams `json:"kdf_params"`
	Salt       string    `json:"salt"`
	Nonce      string    `json:"nonce"`
	Ciphertext string    `json:"ciphertext"`
}

func generateSalt() (string, error) {
	buf := make([]byte, Argon2SaltLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

func generateNonce() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

func argon2idKey(password, saltB64 string, params KDFParams) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %w", err)
	}
	if len(salt) == 0 {
		return nil, errors.New("empty salt")
	}
	return argon2.IDKey([]byte(password), salt, params.Time, params.MemoryKiB, params.Threads, params.KeyLen), nil
}

// argon2iKeyLegacy derives a key using Argon2i (the original algorithm used
// before KDFParams were stored on the envelope). Old vaults were sealed
// with argon2.Key (Argon2i), so decryption must use the same.
func argon2iKeyLegacy(password, saltB64 string) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %w", err)
	}
	if len(salt) == 0 {
		return nil, errors.New("empty salt")
	}
	return argon2.Key([]byte(password), salt, 1, 64*1024, 2, Argon2KeyLen), nil
}

func newGCMCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// DeriveKey derives a 32-byte AES key from the password and salt using Argon2id.
// The salt must be base64-encoded (as stored in VaultEnvelope.Salt).
// This enables callers to derive the key once and reuse it for multiple
// seal/unseal operations without retaining the password in memory.
func DeriveKey(password, saltB64 string) ([32]byte, error) {
	var key [32]byte
	raw, err := argon2idKey(password, saltB64, DefaultArgon2Params)
	if err != nil {
		return key, err
	}
	copy(key[:], raw)
	return key, nil
}

// DeriveKeyWithParams derives a key using explicit KDF params (read from an envelope).
// Falls back to Argon2i (legacy) when the envelope predates param storage (Time==0).
func DeriveKeyWithParams(password, saltB64 string, params KDFParams) ([32]byte, error) {
	var key [32]byte
	var raw []byte
	var err error
	if params.Time == 0 {
		// Legacy: original code used argon2.Key (Argon2i), Time=1.
		raw, err = argon2iKeyLegacy(password, saltB64)
	} else {
		raw, err = argon2idKey(password, saltB64, params)
	}
	if err != nil {
		return key, err
	}
	copy(key[:], raw)
	return key, nil
}

// SealWithKey encrypts plaintext using the given 32-byte key, generating a
// fresh nonce. If salt is empty, a fresh salt is generated; otherwise the
// provided salt is reused (needed for envelope updates so the same derived
// key continues to work). originalParams are written into the new envelope
// so that the KDF params used to derive key remain consistent — without this,
// a reseal could claim new params while the key was derived from old ones,
// making the vault undecryptable. If originalParams is zero, DefaultArgon2Params
// is used (new vault path).
func SealWithKey(key [32]byte, plaintext, saltB64 string, originalParams KDFParams) (*VaultEnvelope, error) {
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
	// Preserve the exact KDF params from the original envelope, including
	// zero-value legacy params (Time==0 signals Argon2i legacy derivation).
	// Never silently upgrade metadata here: the supplied key was derived
	// using these exact params, so the new envelope must claim the same
	// ones. Upgrading params without re-deriving the key makes the vault
	// undecryptable ("wrong password" on the next unlock).
	//
	// Only fall back to defaults for a genuinely new vault where no salt is
	// being reused and no params were supplied. SealEnvelope handles the
	// true brand-new path; this guard is defensive for any future caller.
	kdfParams := originalParams
	if saltB64 == "" && kdfParams.Time == 0 {
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

// OpenWithKey decrypts an envelope using the given 32-byte key.
func OpenWithKey(key [32]byte, env *VaultEnvelope) (string, error) {
	block, err := newGCMCipher(key[:])
	if err != nil {
		return "", err
	}
	nonceBytes, err := decodeB64Strict(env.Nonce)
	if err != nil {
		return "", err
	}
	ciphertext, err := decodeB64Strict(env.Ciphertext)
	if err != nil {
		return "", err
	}
	plaintext, err := block.Open(nil, nonceBytes, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (wrong password?): %w", err)
	}
	return string(plaintext), nil
}

// SealEnvelope encrypts plaintext using a key derived from password.
// Kept for backward compatibility. New code should use SealWithKey with a
// pre-derived key to avoid retaining the password in memory.
func SealEnvelope(password, plaintext string) (*VaultEnvelope, error) {
	salt, err := generateSalt()
	if err != nil {
		return nil, err
	}
	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}
	key, err := argon2idKey(password, salt, DefaultArgon2Params)
	if err != nil {
		return nil, err
	}
	block, err := newGCMCipher(key)
	if err != nil {
		return nil, err
	}
	nonceBytes, err := decodeB64Strict(nonce)
	if err != nil {
		return nil, err
	}
	ciphertext := block.Seal(nil, nonceBytes, []byte(plaintext), nil)
	return &VaultEnvelope{
		Version:    1,
		KDF:        "argon2id",
		KDFParams:  DefaultArgon2Params,
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func OpenEnvelope(password string, env *VaultEnvelope) (string, error) {
	if env == nil {
		return "", errors.New("nil envelope")
	}
	// Derive the key. For old envelopes (Time==0, no KDFParams stored),
	// the original code used argon2.Key (Argon2i) with Time=1. For new
	// envelopes, use argon2.IDKey (Argon2id) with stored params.
	var key []byte
	if env.KDFParams.Time == 0 {
		// Legacy: Argon2i, Time=1, 64 MiB, 2 threads.
		var err error
		key, err = argon2iKeyLegacy(password, env.Salt)
		if err != nil {
			return "", fmt.Errorf("invalid envelope: %w", err)
		}
	} else {
		var err error
		key, err = argon2idKey(password, env.Salt, env.KDFParams)
		if err != nil {
			return "", fmt.Errorf("invalid envelope: %w", err)
		}
	}
	block, err := newGCMCipher(key)
	if err != nil {
		return "", err
	}
	nonce, err := decodeB64Strict(env.Nonce)
	if err != nil {
		return "", fmt.Errorf("invalid nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext: %w", err)
	}
	plaintext, err := block.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (wrong password?): %w", err)
	}
	return string(plaintext), nil
}

// NeedsRekey reports whether an envelope was sealed with outdated KDF settings.
// Returns true when the KDF is not argon2id, KDF params are missing, or costs
// are below the current DefaultArgon2Params policy.
func NeedsRekey(env *VaultEnvelope) bool {
	if env == nil {
		return false
	}
	if env.KDF != "argon2id" {
		return true
	}
	p := env.KDFParams
	if p.Time == 0 {
		return true // old envelope predates param storage
	}
	def := DefaultArgon2Params
	if p.Time < def.Time || p.MemoryKiB < def.MemoryKiB || p.Threads < def.Threads {
		return true
	}
	return false
}

// ClampKDFParams ensures requested KDF parameters are within safe bounds,
// preventing a caller from requesting absurd memory or trivially weak time.
func ClampKDFParams(p KDFParams) KDFParams {
	if p.Time < minArgon2Time {
		p.Time = minArgon2Time
	}
	if p.MemoryKiB < minArgon2Memory {
		p.MemoryKiB = minArgon2Memory
	}
	if p.MemoryKiB > maxArgon2Memory {
		p.MemoryKiB = maxArgon2Memory
	}
	if p.KeyLen != Argon2KeyLen {
		p.KeyLen = Argon2KeyLen
	}
	if p.Threads < 1 {
		p.Threads = 1
	}
	if p.Threads > 8 {
		p.Threads = 8
	}
	return p
}

// RekeyResult summarizes the outcome of a rekey operation for audit logging.
type RekeyResult struct {
	Reason       string // why the rekey was requested
	OldTime      uint32 // previous Argon2 time (0 if legacy)
	NewTime      uint32 // new Argon2 time
	OldMemoryKiB uint32 // previous memory
	NewMemoryKiB uint32 // new memory
}

// RekeyVault re-encrypts the vault file with fresh KDF parameters without
// changing the master password. Flow:
//  1. Read current envelope
//  2. Derive old key from old params
//  3. Decrypt with old key
//  4. Generate new salt
//  5. Derive new key from password + new salt + new params
//  6. Seal envelope with new params (atomic write 0600)
//  7. Return RekeyResult (for audit — metadata only, no secrets)
//
// If the password does not match, it returns a decryption error without
// touching the file. The caller is responsible for audit logging.
func RekeyVault(path, password string, newParams KDFParams) (RekeyResult, error) {
	result := RekeyResult{
		NewTime:      newParams.Time,
		NewMemoryKiB: newParams.MemoryKiB,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read vault: %w", err)
	}
	var oldEnvelope VaultEnvelope
	if err := json.Unmarshal(data, &oldEnvelope); err != nil {
		return result, fmt.Errorf("invalid vault: %w", err)
	}
	result.OldTime = oldEnvelope.KDFParams.Time
	result.OldMemoryKiB = oldEnvelope.KDFParams.MemoryKiB

	// Determine rekey reason for audit.
	switch {
	case oldEnvelope.KDF != "argon2id":
		result.Reason = "legacy_kdf"
	case oldEnvelope.KDFParams.Time == 0:
		result.Reason = "legacy_params"
	default:
		result.Reason = "stale_params"
	}

	// 1. Derive old key from old params.
	var oldKey [32]byte
	if oldEnvelope.KDFParams.Time == 0 {
		raw, derr := argon2iKeyLegacy(password, oldEnvelope.Salt)
		if derr != nil {
			return result, deriveKeyError(derr)
		}
		copy(oldKey[:], raw)
	} else {
		raw, derr := argon2idKey(password, oldEnvelope.Salt, oldEnvelope.KDFParams)
		if derr != nil {
			return result, deriveKeyError(derr)
		}
		copy(oldKey[:], raw)
	}

	// 2. Decrypt with old key. Wrong password → GCM auth failure.
	if _, err := OpenWithKey(oldKey, &oldEnvelope); err != nil {
		return result, fmt.Errorf("rekey auth failed (wrong password?): %w", err)
	}

	// 3. Clamp new params to safe bounds.
	clamped := ClampKDFParams(newParams)

	// 4. Generate new salt.
	newSalt, err := generateSalt()
	if err != nil {
		return result, err
	}

	// 5. Derive new key using the clamped params.
	rawKey, err := argon2idKey(password, newSalt, clamped)
	if err != nil {
		return result, err
	}
	var newKey [32]byte
	copy(newKey[:], rawKey)

	// 6. Re-seal with the same plaintext — we don't have it independently,
	//    so decrypt-then-reseal inline.
	plaintext, err := OpenWithKey(oldKey, &oldEnvelope)
	if err != nil {
		return result, fmt.Errorf("rekey decrypt failed: %w", err)
	}

	newEnvelope, err := SealWithKey(newKey, plaintext, newSalt, clamped)
	if err != nil {
		return result, fmt.Errorf("rekey seal failed: %w", err)
	}
	_ = plaintext // intentionally not zeroed: Go strings cannot be reliably zeroized once allocated

	// 7. Atomic write 0600.
	tmpPath := path + ".rekey"
	out, err := json.Marshal(newEnvelope)
	if err != nil {
		return result, fmt.Errorf("marshal rekey: %w", err)
	}
	if err := os.WriteFile(tmpPath, out, 0600); err != nil {
		return result, fmt.Errorf("write rekey tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return result, fmt.Errorf("rename rekey: %w", err)
	}

	// Wipe old derived key bytes.
	for i := range oldKey {
		oldKey[i] = 0
	}

	result.NewTime = clamped.Time
	result.NewMemoryKiB = clamped.MemoryKiB
	return result, nil
}

func deriveKeyError(err error) error {
	return fmt.Errorf("derive key: %w", err)
}

// maxArgon2Memory and minArgon2Time define safe Argon2 parameter bounds.
// A malicious envelope can't request absurd memory or tiny time.
const maxArgon2Memory = 512 * 1024 // 512 MiB
const minArgon2Time = 1
const minArgon2Memory = 4 * 1024 // 4 MiB
const argon2ExpectedSaltLen = 16
const argon2ExpectedNonceLen = 12

// ValidateEnvelope checks that an envelope's cryptographic parameters are
// safe and well-formed before any key derivation or decryption attempt.
func ValidateEnvelope(env *VaultEnvelope) error {
	if env == nil {
		return errors.New("nil envelope")
	}
	if env.Version != 1 {
		return fmt.Errorf("unsupported envelope version %d", env.Version)
	}
	if env.KDF != "argon2id" {
		return fmt.Errorf("unsupported KDF %q", env.KDF)
	}
	if env.Ciphertext == "" {
		return errors.New("empty ciphertext")
	}
	// Validate salt length.
	if sb, err := base64.StdEncoding.DecodeString(env.Salt); err != nil {
		return fmt.Errorf("invalid salt: %w", err)
	} else if len(sb) != argon2ExpectedSaltLen {
		return fmt.Errorf("salt length %d (expected %d)", len(sb), argon2ExpectedSaltLen)
	}
	// Validate nonce length (GCM requires 12 bytes).
	if nb, err := base64.StdEncoding.DecodeString(env.Nonce); err != nil {
		return fmt.Errorf("invalid nonce: %w", err)
	} else if len(nb) != argon2ExpectedNonceLen {
		return fmt.Errorf("nonce length %d (expected %d)", len(nb), argon2ExpectedNonceLen)
	}
	// Validate KDF params are within safe bounds.
	p := env.KDFParams
	if p.Time != 0 {
		if p.Time < minArgon2Time {
			return fmt.Errorf("Argon2 time %d below minimum %d", p.Time, minArgon2Time)
		}
		if p.MemoryKiB < minArgon2Memory || p.MemoryKiB > maxArgon2Memory {
			return fmt.Errorf("Argon2 memory %d KiB outside safe range [%d, %d]", p.MemoryKiB, minArgon2Memory, maxArgon2Memory)
		}
		if p.KeyLen != Argon2KeyLen {
			return fmt.Errorf("key length %d (expected %d)", p.KeyLen, Argon2KeyLen)
		}
	}
	return nil
}

func decodeB64Strict(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	return b, nil
}

// decodeB64 is a lenient fallback for non-critical uses.
// Prefer decodeB64Strict for cryptographic inputs.
func decodeB64(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return []byte(s)
	}
	return b
}

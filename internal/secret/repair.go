package secret

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// RepairMode controls how RepairVault re-seals after detecting a mismatch.
type RepairMode string

const (
	// RepairPreserve rewrites the envelope preserving legacy Argon2i metadata
	// (Time==0). The vault stays decryptable with the same password and the
	// same Argon2i-derived key.
	RepairPreserve RepairMode = "preserve"
	// RepairUpgrade rekeys the vault to current Argon2id params with a fresh
	// salt. The password and the decrypted contents do not change.
	RepairUpgrade RepairMode = "upgrade"
)

// RepairResult describes what RepairVault did. Metadata-only — no secrets.
type RepairResult struct {
	Repaired      bool       // a mismatch was detected and fixed
	Mode          RepairMode // which repair was applied
	LegacyDerived bool       // the legacy Argon2i derivation succeeded
	PrevTime      uint32     // KDF time in the envelope before repair
}

// recoveryCandidates returns the KDF parameter shapes to try, in order of
// likelihood. Historical versions of the vault code used several different
// derivations, so a poisoned envelope may need more than one attempt:
//
//  1. the envelope's own stored params (correct/matching case)
//  2. zero-value params -> DeriveKeyWithParams interprets Time==0 as legacy
//     Argon2i (the original envelope marker)
//  3. a historical Argon2id candidate with Time=1 used by some early builds
//     before params were stored
//  4. the current default params, in case metadata is empty but the
//     ciphertext was sealed by a newer build
func recoveryCandidates(env *VaultEnvelope) []KDFParams {
	return []KDFParams{
		env.KDFParams,
		{},
		{Time: 1, MemoryKiB: 64 * 1024, Threads: 2, KeyLen: Argon2KeyLen},
		DefaultArgon2Params,
	}
}

// OpenEnvelopeWithRecovery tries to unlock an envelope by attempting multiple
// historical KDF parameter shapes. It returns the decrypted plaintext, the key
// used, which candidate succeeded, and whether recovery (non-first candidate)
// was needed. This is the robust path for vaults poisoned by older SealWithKey
// versions that wrote metadata inconsistent with the actual derivation.
func OpenEnvelopeWithRecovery(password string, env *VaultEnvelope) (plaintext string, key [32]byte, candidate int, recovered bool, err error) {
	var lastErr error
	for i, params := range recoveryCandidates(env) {
		derived, derr := DeriveKeyWithParams(password, env.Salt, params)
		if derr != nil {
			lastErr = derr
			continue
		}
		pt, oerr := OpenWithKey(derived, env)
		if oerr == nil {
			return pt, derived, i, i != 0, nil
		}
		lastErr = oerr
	}
	return "", [32]byte{}, -1, false, fmt.Errorf("decryption failed after recovery attempts: %w", lastErr)
}

// DiagnoseUnlock tries to unlock the vault and reports whether the password
// works with the current envelope metadata. If it does not, it tries recovery
// candidates (including legacy KDF shapes); if one succeeds, a metadata/key
// mismatch is present. It never modifies the vault file.
func DiagnoseUnlock(path, password string) (canUnlock, legacyWorks bool, env *VaultEnvelope, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false, nil, fmt.Errorf("read vault: %w", err)
	}
	var envelope VaultEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false, false, nil, fmt.Errorf("parse envelope: %w", err)
	}
	env = &envelope

	// Try the most likely candidate first (the envelope's stored params).
	if key, derr := DeriveKeyWithParams(password, envelope.Salt, envelope.KDFParams); derr == nil {
		if _, oerr := OpenWithKey(key, &envelope); oerr == nil {
			return true, false, env, nil
		}
	}

	// Recovery: try all historical KDF shapes (legacy Argon2i marker, early
	// Argon2id Time=1, current default).
	_, _, _, recovered, rerr := OpenEnvelopeWithRecovery(password, &envelope)
	if rerr == nil && recovered {
		return false, true, env, nil
	}

	return false, false, env, nil // wrong password (all candidates failed)
}

// RepairVault detects a KDF metadata / key derivation mismatch and fixes it.
//
// When the envelope's stored KDF params do not match the derivation that
// actually produced the sealed key (a state the legacy SealWithKey could
// create), the normal unlock fails even with the correct password.
//
// RepairVault:
//  1. diagnoses the mismatch (without modifying the file)
//  2. writes a backup to <path>.bak.<timestamp>
//  3. decrypts with the correct key
//  4. re-seals per mode (preserve legacy metadata, or upgrade to Argon2id)
//
// The password is never changed. If no mismatch is found (the password unlocks
// normally) it returns Repaired=false and does not touch the file.
func RepairVault(path, password string, mode RepairMode) (*RepairResult, error) {
	result := &RepairResult{Mode: mode}

	canUnlock, legacyWorks, env, err := DiagnoseUnlock(path, password)
	if err != nil {
		return nil, err
	}
	if canUnlock {
		// Vault unlocks fine. No mismatch to repair.
		result.Repaired = false
		return result, nil
	}
	if !legacyWorks {
		// Neither derivation worked → wrong password, not a metadata problem.
		return nil, fmt.Errorf("password incorrect: neither current nor legacy derivation unlocks the vault")
	}

	// Mismatch confirmed: envelope metadata does not match the key.
	result.Repaired = true
	result.LegacyDerived = true
	result.PrevTime = env.KDFParams.Time

	// Decrypt with the legacy Argon2i key.
	var legacyKey [32]byte
	raw, derr := argon2iKeyLegacy(password, env.Salt)
	if derr != nil {
		return nil, fmt.Errorf("legacy derive failed: %w", derr)
	}
	copy(legacyKey[:], raw)
	plaintext, derr := OpenWithKey(legacyKey, env)
	if derr != nil {
		return nil, fmt.Errorf("decrypt for repair failed: %w", derr)
	}

	// Write a backup before re-sealing.
	if err := writeBackup(path); err != nil {
		return nil, fmt.Errorf("backup failed: %w", err)
	}

	switch mode {
	case RepairPreserve:
		// Re-seal with the SAME key and SAME salt, but fix the metadata to
		// Time==0 so the loader falls back to Argon2i on next unlock.
		newEnv, serr := SealWithKey(legacyKey, plaintext, env.Salt, KDFParams{Time: 0})
		if serr != nil {
			return nil, fmt.Errorf("reseal (preserve) failed: %w", serr)
		}
		if serr := writeEnvelope(path, newEnv); serr != nil {
			return nil, fmt.Errorf("write repaired vault failed: %w", serr)
		}

	case RepairUpgrade:
		// Derive a fresh Argon2id key with a new salt and re-seal. This moves
		// the vault off the legacy Argon2i path entirely.
		newSalt, serr := generateSalt()
		if serr != nil {
			return nil, fmt.Errorf("generate salt: %w", serr)
		}
		newKeyRaw, serr := argon2idKey(password, newSalt, DefaultArgon2Params)
		if serr != nil {
			return nil, fmt.Errorf("derive new key: %w", serr)
		}
		var newKey [32]byte
		copy(newKey[:], newKeyRaw)
		newEnv, serr := SealWithKey(newKey, plaintext, newSalt, DefaultArgon2Params)
		if serr != nil {
			return nil, fmt.Errorf("reseal (upgrade) failed: %w", serr)
		}
		if serr := writeEnvelope(path, newEnv); serr != nil {
			return nil, fmt.Errorf("write repaired vault failed: %w", serr)
		}

	default:
		return nil, fmt.Errorf("unknown repair mode %q", mode)
	}

	return result, nil
}

// writeBackup copies the vault file to <path>.bak.<utc-timestamp>.
func writeBackup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	ts := time.Now().UTC().Format("20060102-150405")
	return os.WriteFile(path+".bak."+ts, data, 0600)
}

// WriteVaultBackup is the exported entry point: copies the vault file to
// <path>.bak.<utc-timestamp> and returns the backup path.
func WriteVaultBackup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format("20060102-150405")
	backupPath := path + ".bak." + ts
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return "", err
	}
	return backupPath, nil
}

// writeEnvelope atomically writes an envelope to path (0600).
func writeEnvelope(path string, env *VaultEnvelope) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".repair"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

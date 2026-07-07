package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"aegiskeys/internal/fsutil"
)

// LoadVault reads the encrypted vault at path, decrypts it with password,
// and returns the deserialized Vault. Returns a wrapped error if the
// password is wrong or the file is malformed.
// LoadVaultWithKey loads and decrypts the vault at path, returning the vault
// along with the derived 32-byte encryption key. The key is derived from the
// password and the salt stored in the envelope, enabling SaveVaultWithKey to
// persist changes without retaining the password in memory.
func LoadVaultWithKey(path, password string) (*Vault, [32]byte, error) {
	var zero [32]byte
	if password == "" {
		return nil, zero, errors.New("master password is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, zero, fmt.Errorf("read vault: %w", err)
	}
	var env VaultEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, zero, fmt.Errorf("parse vault envelope: %w", err)
	}
	if err := ValidateEnvelope(&env); err != nil {
		return nil, zero, fmt.Errorf("invalid vault envelope: %w", err)
	}
	key, err := DeriveKeyWithParams(password, env.Salt, env.KDFParams)
	if err != nil {
		return nil, zero, fmt.Errorf("derive key: %w", err)
	}
	plaintext, err := OpenWithKey(key, &env)
	if err != nil {
		return nil, zero, err
	}
	var store vaultStore
	if err := json.Unmarshal([]byte(plaintext), &store); err != nil {
		return nil, zero, fmt.Errorf("parse vault contents: %w", err)
	}
	v := fromStore(&store)
	if v.Version == 0 {
		v.Version = 1
	}
	migrateVaultRecords(v)
	return v, key, nil
}

// LoadVault loads and decrypts the vault at path using the given password.
// It fails closed on wrong password or malformed data.
func LoadVault(path, password string) (*Vault, error) {
	if password == "" {
		return nil, errors.New("master password is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vault: %w", err)
	}
	var env VaultEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse vault envelope: %w", err)
	}
	// Validate envelope metadata before KDF so hostile params are rejected.
	if err := ValidateEnvelope(&env); err != nil {
		return nil, fmt.Errorf("invalid vault envelope: %w", err)
	}
	// Use envelope's stored KDF params, falling back to defaults for old envelopes.
	key, err := DeriveKeyWithParams(password, env.Salt, env.KDFParams)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	plaintext, err := OpenWithKey(key, &env)
	if err != nil {
		return nil, err
	}
	var store vaultStore
	if err := json.Unmarshal([]byte(plaintext), &store); err != nil {
		return nil, fmt.Errorf("parse vault contents: %w", err)
	}
	v := fromStore(&store)
	if v.Version == 0 {
		v.Version = 1
	}
	migrateVaultRecords(v)
	return v, nil
}

// migrateVaultRecords fills defaults for records loaded from older vault files.
func migrateVaultRecords(v *Vault) {
	for i := range v.Keys {
		migrateSecretV1ToV2(&v.Keys[i])
	}
}

// withVaultWriteLock serializes vault writes so concurrent saves cannot
// clobber each other's keys. It takes an exclusive flock on a sidecar lock
// file (cross-process safe) and runs fn while the lock is held.
func withVaultWriteLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire vault lock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return fn()
}

// loadVaultByKey decrypts the on-disk vault with a pre-derived key (no
// password required). Used during save to merge keys written by a concurrent
// process that are not present in the in-memory vault.
func loadVaultByKey(path string, key [32]byte) (*Vault, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env VaultEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse vault envelope: %w", err)
	}
	if err := ValidateEnvelope(&env); err != nil {
		return nil, fmt.Errorf("invalid vault envelope: %w", err)
	}
	plaintext, err := OpenWithKey(key, &env)
	if err != nil {
		return nil, err
	}
	var store vaultStore
	if err := json.Unmarshal([]byte(plaintext), &store); err != nil {
		return nil, fmt.Errorf("parse vault contents: %w", err)
	}
	v := fromStore(&store)
	if v.Version == 0 {
		v.Version = 1
	}
	migrateVaultRecords(v)
	return v, nil
}

// mergeOnDiskKeys preserves keys that were committed by a concurrent save and
// are not already present in the working set, preventing silent data loss.
// The working set wins for matching IDs (it reflects the latest in-memory edit).
func mergeOnDiskKeys(working *Vault, onDisk *Vault) *Vault {
	if onDisk == nil {
		return working
	}
	have := make(map[string]bool, len(working.Keys))
	for _, k := range working.Keys {
		have[k.ID] = true
	}
	merged := &Vault{Version: working.Version, Keys: append([]SecretRecord{}, working.Keys...)}
	for _, k := range onDisk.Keys {
		if !have[k.ID] {
			merged.Keys = append(merged.Keys, k)
			have[k.ID] = true
		}
	}
	return merged
}

// SaveVault serializes the vault (including secrets), encrypts it with
// password, and writes it to path atomically with 0600 permissions.
func SaveVault(path, password string, v *Vault) error {
	if password == "" {
		return errors.New("master password is required")
	}
	if v == nil {
		return errors.New("nil vault")
	}
	if v.Version == 0 {
		v.Version = 1
	}
	return withVaultWriteLock(path, func() error {
		// Reload the on-disk vault under the lock so a concurrent writer's
		// keys are preserved instead of overwritten.
		if _, statErr := os.Stat(path); statErr == nil {
			onDisk, lerr := LoadVault(path, password)
			if lerr != nil {
				return fmt.Errorf("refusing to overwrite existing vault that cannot be opened: %w", lerr)
			}
			v = mergeOnDiskKeys(v, onDisk)
		}
		store := toStore(v)
		plaintext, err := json.MarshalIndent(store, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal vault: %w", err)
		}
		env, err := SealEnvelope(password, string(plaintext))
		if err != nil {
			return fmt.Errorf("encrypt vault: %w", err)
		}
		data, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := fsutil.AtomicWriteFile(path, data); err != nil {
			return err
		}
		// Enforce 0600 explicitly — atomic writes may create with broader mode.
		return os.Chmod(path, 0600)
	})
}

// SaveVaultWithKey is like SaveVault but uses a pre-derived 32-byte key
// instead of a password. This allows the TUI to save the vault without
// retaining the master password in memory. To keep the same derived key valid
// across saves, the existing salt is reused when the vault already exists.
func SaveVaultWithKey(path string, key [32]byte, v *Vault) error {
	if v == nil {
		return errors.New("nil vault")
	}
	if v.Version == 0 {
		v.Version = 1
	}
	return withVaultWriteLock(path, func() error {
		// Reload the on-disk vault under the lock so a concurrent writer's
		// keys are preserved instead of overwritten.
		if _, statErr := os.Stat(path); statErr == nil {
			onDisk, lerr := loadVaultByKey(path, key)
			if lerr != nil {
				return fmt.Errorf("refusing to save with stale vault key; re-unlock required: %w", lerr)
			}
			v = mergeOnDiskKeys(v, onDisk)
		}
		store := toStore(v)
		plaintext, err := json.MarshalIndent(store, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal vault: %w", err)
		}
		// Reuse existing salt if the vault file already exists, so the same
		// derived key remains valid after save.
		salt := ""
		var existingParams KDFParams
		if raw, err := os.ReadFile(path); err == nil {
			var existing VaultEnvelope
			if err := json.Unmarshal(raw, &existing); err == nil {
				salt = existing.Salt
				existingParams = existing.KDFParams
			}
		}
		env, err := SealWithKey(key, string(plaintext), salt, existingParams)
		if err != nil {
			return fmt.Errorf("encrypt vault: %w", err)
		}
		data, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := fsutil.AtomicWriteFile(path, data); err != nil {
			return err
		}
		return os.Chmod(path, 0600)
	})
}

// AtomicWriteFile is an alias for fsutil.AtomicWriteFile for backward compatibility.
// Callers should use fsutil.AtomicWriteFile directly.
func AtomicWriteFile(path string, data []byte) error {
	return fsutil.AtomicWriteFile(path, data)
}

// InitVault creates a new empty encrypted vault at path. It is an error
// to call this when a vault already exists (call VaultExists first).
func InitVault(path, password string) error {
	if VaultExists(path) {
		return errors.New("vault already exists: " + path)
	}
	return SaveVault(path, password, &Vault{Version: 1})
}

// VaultExists reports whether an encrypted vault file is present at path.
func VaultExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Add appends a new secret record, generating an ID and timestamps if unset.
// Returns an error if a record with the same ID already exists.
func (v *Vault) Add(rec SecretRecord) error {
	if rec.ID == "" {
		id, err := NewID()
		if err != nil {
			return fmt.Errorf("generate key id: %w", err)
		}
		rec.ID = id
	}
	for _, k := range v.Keys {
		if k.ID == rec.ID {
			return fmt.Errorf("key %s already exists", rec.ID)
		}
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	v.Keys = append(v.Keys, rec)
	return nil
}

// Get returns a pointer to the record with the given ID, or nil.
// The caller must not retain the pointer across mutations.
func (v *Vault) Get(id string) *SecretRecord {
	for i := range v.Keys {
		if v.Keys[i].ID == id {
			return &v.Keys[i]
		}
	}
	return nil
}

// FindByLabel returns the first record for the given provider whose label
// matches (case-insensitive). Used as a convenience when a profile refers
// to a key by label rather than id.
func (v *Vault) FindByLabel(providerSlug, label string) *SecretRecord {
	for i := range v.Keys {
		if v.Keys[i].ProviderSlug == providerSlug && equalFold(v.Keys[i].Label, label) {
			return &v.Keys[i]
		}
	}
	return nil
}

// Remove deletes the record with the given id. Returns an error if absent.
func (v *Vault) Remove(id string) error {
	for i := range v.Keys {
		if v.Keys[i].ID == id {
			v.Keys = append(v.Keys[:i], v.Keys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("key not found: %s", id)
}

// Rotate replaces the secret value of the record with the given id and
// stamps UpdatedAt. The provider slug and label are preserved.
func (v *Vault) Rotate(id, newSecret string) error {
	rec := v.Get(id)
	if rec == nil {
		return fmt.Errorf("key not found: %s", id)
	}
	rec.Secret = newSecret
	now := time.Now()
	rec.UpdatedAt = now
	rec.LastRotatedAt = &now
	return nil
}

// Touch marks the record with id as used at the current time.
func (v *Vault) Touch(id string) {
	rec := v.Get(id)
	if rec == nil {
		return
	}
	now := time.Now()
	rec.LastUsedAt = &now
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Encrypted-at-rest serialization.
//
// SecretRecord.Secret carries json:"-" so it can never be accidentally
// serialized into logs, audit entries, or display output. But the vault must
// persist secrets inside its encrypted envelope. vaultStore / secretRecordStore
// are the on-disk shapes used ONLY for that encrypted blob — they deliberately
// include the Secret field. Everywhere else, SecretRecord (with json:"-") is
// the in-memory type, and callers must route serialization through these
// helpers.
// ---------------------------------------------------------------------------

type vaultStore struct {
	Version int                 `json:"version"`
	Keys    []secretRecordStore `json:"keys"`
}

type secretRecordStore struct {
	ID           string   `json:"id"`
	ProviderSlug string   `json:"provider_slug"`
	Kind         string   `json:"kind"`
	Label        string   `json:"label"`
	Description  string   `json:"description,omitempty"`
	Account      string   `json:"account,omitempty"`
	Project      string   `json:"project,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Secret       string   `json:"secret"`

	// Non-secret usage hints.
	EnvVarHint  string `json:"env_var_hint,omitempty"`
	HeaderHint  string `json:"header_hint,omitempty"`
	BaseURLHint string `json:"base_url_hint,omitempty"`
	DocsURL     string `json:"docs_url,omitempty"`

	// Private note — encrypted with vault payload.
	PrivateNote string `json:"private_note,omitempty"`

	// Lifecycle.
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RotatesAt     *time.Time `json:"rotates_at,omitempty"`
	LastRotatedAt *time.Time `json:"last_rotated_at,omitempty"`

	// Policy.
	RevealPolicy string       `json:"reveal_policy,omitempty"`
	Exportable   bool         `json:"exportable,omitempty"`
	Archived     bool         `json:"archived,omitempty"`
	Policy       SecretPolicy `json:"policy,omitempty"`
}

func toStore(v *Vault) vaultStore {
	store := vaultStore{Version: v.Version}
	for _, k := range v.Keys {
		store.Keys = append(store.Keys, secretRecordStore{
			ID:            k.ID,
			ProviderSlug:  k.ProviderSlug,
			Kind:          string(k.Kind),
			Label:         k.Label,
			Description:   k.Description,
			Account:       k.Account,
			Project:       k.Project,
			Tags:          k.Tags,
			Secret:        k.Secret,
			EnvVarHint:    k.EnvVarHint,
			HeaderHint:    k.HeaderHint,
			BaseURLHint:   k.BaseURLHint,
			DocsURL:       k.DocsURL,
			PrivateNote:   k.PrivateNote,
			CreatedAt:     k.CreatedAt,
			UpdatedAt:     k.UpdatedAt,
			LastUsedAt:    k.LastUsedAt,
			ExpiresAt:     k.ExpiresAt,
			RotatesAt:     k.RotatesAt,
			LastRotatedAt: k.LastRotatedAt,
			RevealPolicy:  string(k.RevealPolicy),
			Exportable:    k.Exportable,
			Archived:      k.Archived,
			Policy:        k.Policy,
		})
	}
	return store
}

func fromStore(store *vaultStore) *Vault {
	v := &Vault{Version: store.Version}
	for _, k := range store.Keys {
		v.Keys = append(v.Keys, SecretRecord{
			ID:            k.ID,
			ProviderSlug:  k.ProviderSlug,
			Kind:          SecretKind(k.Kind),
			Label:         k.Label,
			Description:   k.Description,
			Account:       k.Account,
			Project:       k.Project,
			Tags:          k.Tags,
			Secret:        k.Secret,
			EnvVarHint:    k.EnvVarHint,
			HeaderHint:    k.HeaderHint,
			BaseURLHint:   k.BaseURLHint,
			DocsURL:       k.DocsURL,
			PrivateNote:   k.PrivateNote,
			CreatedAt:     k.CreatedAt,
			UpdatedAt:     k.UpdatedAt,
			LastUsedAt:    k.LastUsedAt,
			ExpiresAt:     k.ExpiresAt,
			RotatesAt:     k.RotatesAt,
			LastRotatedAt: k.LastRotatedAt,
			RevealPolicy:  RevealPolicy(k.RevealPolicy),
			Exportable:    k.Exportable,
			Archived:      k.Archived,
			Policy:        k.Policy,
		})
	}
	return v
}

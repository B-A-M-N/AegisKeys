package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	_, env, err := readEnvelopeFile(path)
	if err != nil {
		return nil, zero, err
	}
	if err := ValidateEnvelopeForDecrypt(&env); err != nil {
		return nil, zero, fmt.Errorf("invalid vault envelope: %w", err)
	}
	if env.KeyMode == "keyring" {
		return nil, zero, errors.New("vault requires its OS keyring or recovery key; password unlock is disabled")
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

// LoadVaultByKey opens a vault using a previously derived key. The key is
// intended only for an OS-protected keyring entry created by explicit opt-in.
func LoadVaultByKey(path string, key [32]byte) (*Vault, error) {
	return loadVaultByKey(path, key)
}

// LoadVault loads and decrypts the vault at path using the given password.
// It fails closed on wrong password or malformed data.
func LoadVault(path, password string) (*Vault, error) {
	if password == "" {
		return nil, errors.New("master password is required")
	}
	_, env, err := readEnvelopeFile(path)
	if err != nil {
		return nil, err
	}
	if env.KeyMode == "keyring" {
		return nil, errors.New("vault requires its OS keyring or recovery key; password unlock is disabled")
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
	// ScratchPads were added after launch; existing vaults have nil slices.
	// Ensure a non-nil slice so callers can append without nil checks.
	if v.ScratchPads == nil {
		v.ScratchPads = []ScratchPadRecord{}
	}
	for i := range v.ScratchPads {
		if v.ScratchPads[i].Kind == "" {
			v.ScratchPads[i].Kind = ScratchPadGeneral
		}
	}
}

// withVaultWriteLock serializes vault writes so concurrent saves cannot
// clobber each other's keys. It takes an exclusive flock on a sidecar lock
// file (cross-process safe) and runs fn while the lock is held.
func withVaultWriteLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	lf, err := fsutil.OpenLockFile(lockPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := fsutil.LockFile(lf); err != nil {
		return fmt.Errorf("acquire vault lock: %w", err)
	}
	defer fsutil.UnlockFile(lf)
	return fn()
}

// loadVaultByKey decrypts the on-disk vault with a pre-derived key (no
// password required). Used during save to merge keys written by a concurrent
// process that are not present in the in-memory vault.
func loadVaultByKey(path string, key [32]byte) (*Vault, error) {
	_, env, err := readEnvelopeFile(path)
	if err != nil {
		return nil, err
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

// CloneVault returns an independent in-memory copy suitable for an
// asynchronous save. It intentionally uses the vault's private safe-store
// representation so new secret fields cannot accidentally be omitted from a
// hand-written copy routine.
func CloneVault(v *Vault) (*Vault, error) {
	if v == nil {
		return nil, errors.New("nil vault")
	}
	raw, err := json.Marshal(toStore(v))
	if err != nil {
		return nil, fmt.Errorf("clone vault: %w", err)
	}
	var store vaultStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, fmt.Errorf("clone vault: %w", err)
	}
	clone := fromStore(&store)
	migrateVaultRecords(clone)
	return clone, nil
}

// MutateVault loads the latest vault under the exclusive write lock, applies
// one mutation to that authoritative state, and atomically writes the result.
// It deliberately does not merge a caller's old in-memory snapshot back into
// the file: absence now means deletion, while concurrent additions and edits
// made after the caller loaded its snapshot remain intact.
func MutateVault(path, password string, mutate func(*Vault) error) error {
	if password == "" {
		return errors.New("master password is required")
	}
	if mutate == nil {
		return errors.New("nil vault mutation")
	}
	return withVaultWriteLock(path, func() error {
		v, err := LoadVault(path, password)
		if err != nil {
			return err
		}
		defer ZeroVault(v)
		if err := mutate(v); err != nil {
			return err
		}
		return writeVaultWithPasswordLocked(path, password, v)
	})
}

// MutateVaultWithKey is MutateVault for an already-derived vault key. It has
// the same latest-state transaction semantics and uses the same exclusive
// cross-process lock.
func MutateVaultWithKey(path string, key [32]byte, mutate func(*Vault) error) error {
	if mutate == nil {
		return errors.New("nil vault mutation")
	}
	return withVaultWriteLock(path, func() error {
		v, err := loadVaultByKey(path, key)
		if err != nil {
			return err
		}
		defer ZeroVault(v)
		if err := mutate(v); err != nil {
			return err
		}
		normalizeVaultBrokerPolicyProvenance(v)
		return writeVaultWithKeyLocked(path, key, v)
	})
}

func normalizeVaultBrokerPolicyProvenance(v *Vault) {
	if v == nil {
		return
	}
	for i := range v.Keys {
		NormalizeBrokerPolicyProvenance(&v.Keys[i].Policy)
	}
}

// SaveVault serializes the vault (including secrets), encrypts it with
// password, and writes it atomically with 0600 permissions.
//
// This is a full-snapshot authenticated replace, not a merge. Existing callers
// that perform a specific update should use MutateVault instead; init and
// recovery flows intentionally remain full-snapshot writes.
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
		if VaultExists(path) {
			if _, err := LoadVault(path, password); err != nil {
				return fmt.Errorf("refusing to overwrite existing vault that cannot be opened: %w", err)
			}
		}
		return writeVaultWithPasswordLocked(path, password, v)
	})
}

// SaveVaultWithKey is like SaveVault but uses a pre-derived 32-byte key
// instead of a password. To keep the same derived key valid across saves, the
// existing salt and envelope metadata are reused when the vault exists.
//
// This is a full-snapshot authenticated replace. Specific mutations should use
// MutateVaultWithKey so concurrent records are neither resurrected nor
// overwritten by a stale snapshot.
func SaveVaultWithKey(path string, key [32]byte, v *Vault) error {
	if v == nil {
		return errors.New("nil vault")
	}
	if v.Version == 0 {
		v.Version = 1
	}
	return withVaultWriteLock(path, func() error {
		if VaultExists(path) {
			if _, err := loadVaultByKey(path, key); err != nil {
				return fmt.Errorf("refusing to save with stale vault key; re-unlock required: %w", err)
			}
		}
		return writeVaultWithKeyLocked(path, key, v)
	})
}

// writeVaultWithPasswordLocked seals and replaces a vault. The caller must
// already hold withVaultWriteLock; locking here would deadlock.
func writeVaultWithPasswordLocked(path, password string, v *Vault) error {
	if v == nil {
		return errors.New("nil vault")
	}
	if v.Version == 0 {
		v.Version = 1
	}
	normalizeVaultBrokerPolicyProvenance(v)
	plaintext, err := json.MarshalIndent(toStore(v), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vault: %w", err)
	}
	env, err := SealEnvelope(password, string(plaintext))
	if err != nil {
		return fmt.Errorf("encrypt vault: %w", err)
	}
	return writeVaultEnvelopeLocked(path, env)
}

// writeVaultWithKeyLocked seals and replaces a vault with a derived key while
// preserving the existing salt/key mode. The caller must already hold the
// vault write lock.
func writeVaultWithKeyLocked(path string, key [32]byte, v *Vault) error {
	if v == nil {
		return errors.New("nil vault")
	}
	if v.Version == 0 {
		v.Version = 1
	}
	normalizeVaultBrokerPolicyProvenance(v)
	plaintext, err := json.MarshalIndent(toStore(v), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vault: %w", err)
	}
	salt := ""
	var existingParams KDFParams
	var existingMode, existingKDF string
	if raw, err := readSecureFile(path, maxEnvelopeBytes); err == nil {
		var existing VaultEnvelope
		if err := json.Unmarshal(raw, &existing); err == nil {
			salt = existing.Salt
			existingParams = existing.KDFParams
			existingMode = existing.KeyMode
			existingKDF = existing.KDF
		}
	}
	env, err := SealWithKey(key, string(plaintext), salt, existingParams)
	if err != nil {
		return fmt.Errorf("encrypt vault: %w", err)
	}
	if existingMode == "keyring" {
		env.KeyMode = "keyring"
		env.KDF = "keyring"
		env.KDFParams = KDFParams{}
	} else if existingKDF != "" {
		env.KeyMode = existingMode
		env.KDF = existingKDF
	}
	return writeVaultEnvelopeLocked(path, env)
}

func writeVaultEnvelopeLocked(path string, env *VaultEnvelope) error {
	env.Digest = envelopeDigest(*env)
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return fsutil.AtomicWriteFileMode(path, data, 0600)
}

// AtomicWriteFile is an alias for fsutil.AtomicWriteFile for backward compatibility.
// Callers should use fsutil.AtomicWriteFile directly.
func AtomicWriteFile(path string, data []byte) error {
	return fsutil.AtomicWriteFile(path, data)
}

// InitVault creates a new empty encrypted vault at path. It is an error
// to call this when a vault already exists (call VaultExists first).
func InitVault(path, password string) error {
	if password == "" {
		return errors.New("master password is required")
	}
	return withVaultWriteLock(path, func() error {
		if VaultExists(path) {
			return errors.New("vault already exists: " + path)
		}
		return writeVaultWithPasswordLocked(path, password, &Vault{Version: 1})
	})
}

// MigrateToKeyringRequiredWithKey atomically converts a password vault to a
// vault that can only be opened with key. Store key in the OS keyring before
// calling this function, and retain a recovery copy before removing password
// unlock. It fails without changing the vault if password verification fails.
func MigrateToKeyringRequiredWithKey(path, password string, key [32]byte) error {
	if key == ([32]byte{}) {
		return errors.New("keyring vault key must not be empty")
	}
	return withVaultWriteLock(path, func() error {
		// Authenticate and serialize under the write lock so concurrent edits
		// cannot be replaced by a stale pre-migration snapshot.
		v, _, err := LoadVaultWithKey(path, password)
		if err != nil {
			return fmt.Errorf("refusing keyring migration: %w", err)
		}
		plain, err := json.MarshalIndent(toStore(v), "", "  ")
		if err != nil {
			return err
		}
		env, err := SealKeyringEnvelope(key, string(plain))
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}
		return fsutil.AtomicWriteFileMode(path, data, 0600)
	})
}

// MigrateToKeyringRequired is retained for API compatibility. New callers
// should create/store a key first, then call MigrateToKeyringRequiredWithKey
// so a failed keyring write cannot strand the vault.
func MigrateToKeyringRequired(path, password string) ([32]byte, error) {
	var zero [32]byte
	key, err := RandomVaultKey()
	if err != nil {
		return zero, err
	}
	if err := MigrateToKeyringRequiredWithKey(path, password, key); err != nil {
		return zero, err
	}
	return key, nil
}

// VaultExists reports whether an encrypted vault file is present at path.
func VaultExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// VaultKeyMode returns the vault unlock mode without decrypting its contents.
// It is metadata only: "password" for legacy/password-derived envelopes and
// "keyring" for a keyring-required vault.
func VaultKeyMode(path string) (string, error) {
	raw, err := readSecureFile(path, maxEnvelopeBytes)
	if err != nil {
		return "", fmt.Errorf("read vault: %w", err)
	}
	var env VaultEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("parse vault envelope: %w", err)
	}
	if err := ValidateEnvelopeForDecrypt(&env); err != nil {
		return "", fmt.Errorf("invalid vault envelope: %w", err)
	}
	if env.KeyMode == "keyring" {
		return "keyring", nil
	}
	return "password", nil
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
			var zero SecretRecord
			v.Keys[i] = zero
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

// ScratchPad CRUD ---------------------------------------------------------

// AddScratchPad appends a new scratchpad, generating an ID and timestamps if
// unset. Returns an error if a record with the same ID already exists.
func (v *Vault) AddScratchPad(rec ScratchPadRecord) error {
	if rec.ID == "" {
		id, err := NewID()
		if err != nil {
			return fmt.Errorf("generate scratchpad id: %w", err)
		}
		rec.ID = id
	}
	for _, s := range v.ScratchPads {
		if s.ID == rec.ID {
			return fmt.Errorf("scratchpad %s already exists", rec.ID)
		}
	}
	now := time.Now()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	if rec.Kind == "" {
		rec.Kind = ScratchPadGeneral
	}
	v.ScratchPads = append(v.ScratchPads, rec)
	return nil
}

// UpdateScratchPad applies a partial update to the scratchpad with the given
// id. Empty/nil fields in update are ignored so callers can send only the
// fields they intend to change.
func (v *Vault) UpdateScratchPad(id string, update ScratchPadRecord) error {
	rec := v.GetScratchPad(id)
	if rec == nil {
		return fmt.Errorf("scratchpad not found: %s", id)
	}
	if update.Title != "" {
		rec.Title = update.Title
	}
	if update.Body != "" {
		rec.Body = update.Body
	}
	if update.Kind != "" {
		rec.Kind = update.Kind
	}
	if update.ProviderSlug != "" {
		rec.ProviderSlug = update.ProviderSlug
	}
	if update.KeyID != "" {
		rec.KeyID = update.KeyID
	}
	if update.Tags != nil {
		rec.Tags = update.Tags
	}
	// Archived is a bool — explicit updates only. Callers must set the field.
	rec.Archived = update.Archived
	rec.UpdatedAt = time.Now()
	return nil
}

// RemoveScratchPad deletes the scratchpad with the given id. Returns an error
// if absent.
func (v *Vault) RemoveScratchPad(id string) error {
	for i := range v.ScratchPads {
		if v.ScratchPads[i].ID == id {
			var zero ScratchPadRecord
			v.ScratchPads[i] = zero
			v.ScratchPads = append(v.ScratchPads[:i], v.ScratchPads[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("scratchpad not found: %s", id)
}

// GetScratchPad returns a pointer to the scratchpad with the given ID, or nil.
func (v *Vault) GetScratchPad(id string) *ScratchPadRecord {
	for i := range v.ScratchPads {
		if v.ScratchPads[i].ID == id {
			return &v.ScratchPads[i]
		}
	}
	return nil
}

// Key note helpers ----------------------------------------------------------

// UpdateKeyPrivateNote sets the encrypted private note on a key record.
func (v *Vault) UpdateKeyPrivateNote(id, note string) error {
	rec := v.Get(id)
	if rec == nil {
		return fmt.Errorf("key not found: %s", id)
	}
	rec.PrivateNote = note
	rec.UpdatedAt = time.Now()
	return nil
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
	Version     int                 `json:"version"`
	Keys        []secretRecordStore `json:"keys"`
	ScratchPads []scratchPadStore   `json:"scratch_pads,omitempty"`
}

// scratchPadStore is the on-disk shape for a scratchpad. Like
// secretRecordStore, it deliberately includes the encrypted Body field.
type scratchPadStore struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	ProviderSlug string    `json:"provider_slug,omitempty"`
	KeyID        string    `json:"key_id,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Archived     bool      `json:"archived,omitempty"`
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

	// Non-secret provider setup values (resource, deployment, region, ...).
	Fields map[string]string `json:"fields,omitempty"`

	// Additional secret components (e.g. AWS secret access key). Deliberately
	// included in the encrypted store shape; never in the metadata export.
	ExtraSecrets []namedSecretStore `json:"extra_secrets,omitempty"`

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
	Policy       SecretPolicy `json:"policy,omitzero"`
}

// namedSecretStore is the on-disk shape for a secondary secret component. Like
// secretRecordStore, it deliberately includes the raw secret inside the
// encrypted envelope.
type namedSecretStore struct {
	Key    string `json:"key"`
	Label  string `json:"label,omitempty"`
	EnvVar string `json:"env_var"`
	Secret string `json:"secret"`
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
			Fields:        k.Fields,
			PrivateNote:   k.PrivateNote,
			ExtraSecrets:  namedSecretsToStore(k.ExtraSecrets),
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
	for _, s := range v.ScratchPads {
		store.ScratchPads = append(store.ScratchPads, scratchPadStore{
			ID:           s.ID,
			Kind:         string(s.Kind),
			Title:        s.Title,
			Body:         s.Body,
			ProviderSlug: s.ProviderSlug,
			KeyID:        s.KeyID,
			Tags:         s.Tags,
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
			Archived:     s.Archived,
		})
	}
	return store
}

func namedSecretsToStore(in []NamedSecret) []namedSecretStore {
	if len(in) == 0 {
		return nil
	}
	out := make([]namedSecretStore, 0, len(in))
	for _, s := range in {
		out = append(out, namedSecretStore{
			Key:    s.Key,
			Label:  s.Label,
			EnvVar: s.EnvVar,
			Secret: s.Secret,
		})
	}
	return out
}

func namedSecretsFromStore(in []namedSecretStore) []NamedSecret {
	if len(in) == 0 {
		return nil
	}
	out := make([]NamedSecret, 0, len(in))
	for _, s := range in {
		out = append(out, NamedSecret{
			Key:    s.Key,
			Label:  s.Label,
			EnvVar: s.EnvVar,
			Secret: s.Secret,
		})
	}
	return out
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
			Fields:        k.Fields,
			PrivateNote:   k.PrivateNote,
			ExtraSecrets:  namedSecretsFromStore(k.ExtraSecrets),
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
	for _, s := range store.ScratchPads {
		v.ScratchPads = append(v.ScratchPads, ScratchPadRecord{
			ID:           s.ID,
			Kind:         ScratchPadKind(s.Kind),
			Title:        s.Title,
			Body:         s.Body,
			ProviderSlug: s.ProviderSlug,
			KeyID:        s.KeyID,
			Tags:         s.Tags,
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
			Archived:     s.Archived,
		})
	}
	return v
}

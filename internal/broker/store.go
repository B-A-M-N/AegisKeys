package broker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"aegiskeys/internal/config"
	"aegiskeys/internal/fsutil"
)

var bindingNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// ValidBindingName rejects whitespace, control characters, traversal, and
// secret-looking values before they reach metadata or audit logs.
func ValidBindingName(name string) bool {
	return bindingNamePattern.MatchString(name) && !strings.Contains(name, "..")
}

func (f *File) Validate() error {
	if f == nil {
		return errors.New("nil broker file")
	}
	if f.Version != 1 {
		return fmt.Errorf("unsupported broker metadata version %d", f.Version)
	}
	bindingIDs := make(map[string]bool)
	bindingNames := make(map[string]bool)
	for _, b := range f.Bindings {
		if b.ID == "" || b.Name == "" || b.SecretID == "" || !ValidBindingName(b.Name) {
			return errors.New("invalid broker binding")
		}
		if bindingIDs[b.ID] || bindingNames[b.Name] {
			return errors.New("duplicate broker binding id or name")
		}
		bindingIDs[b.ID], bindingNames[b.Name] = true, true
	}
	grantIDs := make(map[string]bool)
	for _, g := range f.Grants {
		if g.ID == "" || g.Name == "" || !bindingIDs[g.BindingID] || g.Client.UID < 0 {
			return errors.New("invalid broker grant")
		}
		if grantIDs[g.ID] {
			return errors.New("duplicate broker grant id")
		}
		grantIDs[g.ID] = true
		if g.Client.ExecutablePath == "" && g.Client.ExecutableHash == "" {
			return errors.New("broker grant requires an executable pin")
		}
		if g.Client.IdentityScope != "" && g.Client.IdentityScope != IdentityDedicatedExecutable && g.Client.IdentityScope != IdentityInterpreterWide {
			return errors.New("invalid broker identity scope")
		}
		if g.Client.IdentityScope == IdentityInterpreterWide && !g.Client.InterpreterAcknowledged {
			return errors.New("interpreter-wide grant requires explicit acknowledgment")
		}
		if g.Client.IdentityScope == IdentityInterpreterWide && ClassifyExecutable(g.Client.ExecutablePath) != IdentityInterpreterWide {
			return errors.New("interpreter-wide scope does not match executable")
		}
		for _, c := range g.Capabilities {
			if !c.Valid() {
				return errors.New("invalid broker capability")
			}
		}
	}
	intentIDs := make(map[string]bool)
	for _, intent := range f.PendingApprovals {
		if intent.GrantID == "" || intent.BindingID == "" || intent.SecretID == "" || !bindingIDs[intent.BindingID] || intent.CreatedAt.IsZero() {
			return errors.New("invalid broker approval intent")
		}
		if intentIDs[intent.GrantID] {
			return errors.New("duplicate broker approval intent")
		}
		intentIDs[intent.GrantID] = true
		if len(intent.Capabilities) == 0 {
			return errors.New("broker approval intent requires capabilities")
		}
		seenCaps := map[Capability]bool{}
		for _, capability := range intent.Capabilities {
			if !capability.Valid() || seenCaps[capability] {
				return errors.New("invalid broker approval intent capabilities")
			}
			seenCaps[capability] = true
		}
		if !intent.PriorResolveSource.Valid() || !intent.PriorRotateSource.Valid() {
			return errors.New("invalid broker approval policy provenance")
		}
	}
	return nil
}

// LoadBrokerFile returns a new empty metadata file when broker.json is absent
// and fails closed on malformed JSON, unknown versions, or invalid metadata.
func LoadBrokerFile(path string) (*File, error) {
	raw, err := fsutil.ReadFile(path, 4<<20)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewFile(), nil
		}
		return nil, fmt.Errorf("read broker metadata: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse broker metadata: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, fmt.Errorf("parse broker metadata: %w", err)
	}
	if f.Version == 0 {
		f.Version = 1
	}
	if f.Bindings == nil {
		f.Bindings = []CredentialBinding{}
	}
	if f.Grants == nil {
		f.Grants = []AccessGrant{}
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("validate broker metadata: %w", err)
	}
	return &f, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// SaveBrokerFile atomically writes broker.json with 0600 permissions.
func SaveBrokerFile(path string, f *File) error {
	if f == nil {
		return errors.New("nil broker file")
	}
	if f.Version == 0 {
		f.Version = 1
	}
	if err := f.Validate(); err != nil {
		return err
	}
	f.Revision++
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return fsutil.AtomicWriteFileMode(path, data, 0600)
}

// MutateBrokerFile serializes load/modify/save under a cross-process lock.
func MutateBrokerFile(path string, mutate func(*File) error) error {
	return TransactBrokerFile(path, mutate)
}

// TransactBrokerFile holds the metadata lock while the callback runs. Callbacks
// that pair broker authorization state with another encrypted file can use the
// held lock to exclude competing broker transactions until the final metadata
// write completes. A callback error prevents the save.
// WithBrokerFileLocked holds the metadata lock while fn reads or coordinates a
// related encrypted-store operation. It does not rewrite metadata.
func WithBrokerFileLocked(path string, fn func(*File) error) error {
	if fn == nil {
		return errors.New("nil broker lock operation")
	}
	lockPath := path + ".lock"
	lf, err := fsutil.OpenLockFile(lockPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := fsutil.LockFile(lf); err != nil {
		return fmt.Errorf("acquire broker metadata lock: %w", err)
	}
	defer fsutil.UnlockFile(lf)
	f, err := LoadBrokerFile(path)
	if err != nil {
		return err
	}
	return fn(f)
}

func TransactBrokerFile(path string, mutate func(*File) error) error {
	if mutate == nil {
		return errors.New("nil broker mutation")
	}
	lockPath := path + ".lock"
	lf, err := fsutil.OpenLockFile(lockPath)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := fsutil.LockFile(lf); err != nil {
		return fmt.Errorf("acquire broker metadata lock: %w", err)
	}
	defer fsutil.UnlockFile(lf)
	f, err := LoadBrokerFile(path)
	if err != nil {
		return err
	}
	if err := mutate(f); err != nil {
		return err
	}
	return SaveBrokerFile(path, f)
}

// DefaultBrokerPath returns broker.json under the supplied config directory.
func DefaultBrokerPath(configDir string) string { return config.BrokerPath(configDir) }

package broker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

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
		for _, c := range g.Capabilities {
			if !c.Valid() {
				return errors.New("invalid broker capability")
			}
		}
	}
	return nil
}

// LoadBrokerFile returns a new empty metadata file when broker.json is absent
// and fails closed on malformed JSON, unknown versions, or invalid metadata.
func LoadBrokerFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewFile(), nil
		}
		return nil, fmt.Errorf("read broker metadata: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := fsutil.AtomicWriteFile(path, data); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

// MutateBrokerFile serializes load/modify/save under a cross-process lock.
func MutateBrokerFile(path string, mutate func(*File) error) error {
	if mutate == nil {
		return errors.New("nil broker mutation")
	}
	lockPath := path + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire broker metadata lock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
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

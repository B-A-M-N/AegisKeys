// Package keychain stores an optional copy of the already-derived vault key
// in the operating system's protected credential store. It never stores the
// master password and has no plaintext-file fallback.
package keychain

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"

	keyring "github.com/zalando/go-keyring"
)

const service = "aegiskeys.vault-key.v1"

type backend interface {
	Get(service, user string) (string, error)
	Set(service, user, value string) error
	Delete(service, user string) error
}

type systemBackend struct{}

func (systemBackend) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (systemBackend) Set(s, u, v string) error        { return keyring.Set(s, u, v) }
func (systemBackend) Delete(s, u string) error        { return keyring.Delete(s, u) }

var active backend = systemBackend{}

func account(configDir string) string {
	clean, err := filepath.Abs(configDir)
	if err != nil {
		clean = filepath.Clean(configDir)
	}
	sum := sha256.Sum256([]byte(clean))
	return fmt.Sprintf("vault:%x", sum[:16])
}

func Store(configDir string, key [32]byte) error {
	return active.Set(service, account(configDir), base64.RawStdEncoding.EncodeToString(key[:]))
}

func Load(configDir string) ([32]byte, error) {
	var out [32]byte
	raw, err := active.Get(service, account(configDir))
	if err != nil {
		return out, fmt.Errorf("OS keyring: %w", err)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil || len(decoded) != len(out) {
		return out, errors.New("OS keyring contains an invalid vault key")
	}
	copy(out[:], decoded)
	return out, nil
}

func Delete(configDir string) error { return active.Delete(service, account(configDir)) }

// BackendForTest replaces the OS backend and returns a restore function.
func BackendForTest(b backend) func() { old := active; active = b; return func() { active = old } }

// Package broker implements metadata and authorization for local application
// credential access. It intentionally stores bindings and grants only; raw
// credentials remain exclusively in the encrypted vault.
package broker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ProtocolVersion is the only broker IPC version understood by this build.
const ProtocolVersion = "v1"

// Capability is an explicitly granted broker operation.
type Capability string

const (
	CapabilityResolve Capability = "resolve"
	CapabilityRotate  Capability = "rotate"
)

// Valid reports whether c is a known capability.
func (c Capability) Valid() bool {
	switch c {
	case CapabilityResolve, CapabilityRotate:
		return true
	default:
		return false
	}
}

// CredentialBinding maps a stable, application-facing name to a vault record.
type CredentialBinding struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SecretID string `json:"secret_id"`
	// Empty means primary-only. Secondary NamedSecret keys must be listed
	// explicitly; "primary" may be listed to make the choice visible.
	ComponentAllowlist []string  `json:"component_allowlist,omitempty"`
	Description        string    `json:"description,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ClientConstraint identifies a same-user local executable. Either path or
// hash pinning is required; both may be supplied.
type ClientConstraint struct {
	UID            int    `json:"uid,omitempty"`
	ExecutablePath string `json:"executable_path,omitempty"`
	ExecutableHash string `json:"executable_sha256,omitempty"`
}

// AccessGrant authorizes one constrained client to invoke selected operations
// against one binding.
type AccessGrant struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	BindingID    string           `json:"binding_id"`
	Client       ClientConstraint `json:"client"`
	Capabilities []Capability     `json:"capabilities"`
	Enabled      bool             `json:"enabled"`
	CreatedAt    time.Time        `json:"created_at"`
	ExpiresAt    *time.Time       `json:"expires_at,omitempty"`
}

// ApprovalIntent durably records a cross-domain approval spanning
// broker.json and vault.enc. Recovery rolls it back when its staged grant is
// absent; an enabled grant is the committed terminal state.
type ApprovalIntent struct {
	GrantID           string    `json:"grant_id"`
	BindingID         string    `json:"binding_id"`
	SecretID          string    `json:"secret_id"`
	PriorAllowResolve bool      `json:"prior_allow_resolve"`
	PriorAllowRotate  bool      `json:"prior_allow_rotate"`
	CreatedAt         time.Time `json:"created_at"`
}

// File is the metadata-only broker store persisted as broker.json.
// Revision is a transactional mutation counter only. broker.json is neither
// cryptographically authenticated nor protected against rollback by this build.
type File struct {
	Version          int                 `json:"version"`
	Revision         uint64              `json:"revision"`
	Bindings         []CredentialBinding `json:"bindings"`
	Grants           []AccessGrant       `json:"grants,omitempty"`
	PendingApprovals []ApprovalIntent    `json:"pending_approvals,omitempty"`
}

// NewFile returns an empty current-version broker metadata file.
func NewFile() *File {
	return &File{Version: 1}
}

// FindBinding returns a pointer to the binding with the stable name.
func (f *File) FindBinding(name string) *CredentialBinding {
	for i := range f.Bindings {
		if f.Bindings[i].Name == name {
			return &f.Bindings[i]
		}
	}
	return nil
}

// FindGrant returns a pointer to an enabled, non-expired grant with the
// requested capability and client constraints.
func (f *File) FindGrant(bindingID string, client ClientConstraint, capability Capability) *AccessGrant {
	now := time.Now()
	for i := range f.Grants {
		g := &f.Grants[i]
		if g.BindingID != bindingID || !g.Enabled || !hasCapability(g.Capabilities, capability) {
			continue
		}
		if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
			continue
		}
		if !clientMatches(g.Client, client) {
			continue
		}
		return g
	}
	return nil
}

func hasCapability(caps []Capability, want Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

func clientMatches(granted, actual ClientConstraint) bool {
	if granted.UID != actual.UID {
		return false
	}
	pathOK := granted.ExecutablePath != "" && granted.ExecutablePath == actual.ExecutablePath
	hashOK := granted.ExecutableHash != "" && granted.ExecutableHash == actual.ExecutableHash
	// At least one pinned identifier must match; when both are present, both
	// must match so a moved/rebuilt binary cannot satisfy a partial pin.
	if granted.ExecutablePath != "" && granted.ExecutableHash != "" {
		return pathOK && hashOK
	}
	return pathOK || hashOK
}

// NewBindingID returns a collision-resistant metadata identifier.
func NewBindingID() (string, error) { return newID("binding_") }

// NewGrantID returns a collision-resistant grant identifier.
func NewGrantID() (string, error) { return newID("grant_") }

func newID(prefix string) (string, error) {
	buf := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate broker id: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

// HashExecutable canonicalizes path through /proc-style EvalSymlinks and
// returns a lowercase SHA-256 hex digest.
func HashExecutable(path string) (string, error) {
	if path == "" {
		return "", errors.New("executable path is required")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	f, err := os.Open(canonical)
	if err != nil {
		return "", fmt.Errorf("open executable: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash executable: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Package broker implements metadata and authorization for local application
// credential access. It intentionally stores bindings and grants only; raw
// credentials remain exclusively in the encrypted vault.
package broker

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"aegiskeys/internal/fsutil"
	"aegiskeys/internal/secret"
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
	// IdentityScope distinguishes a dedicated executable pin from an explicit
	// interpreter-wide grant. Interpreter grants do not identify an individual
	// script and must never be presented as per-application isolation.
	IdentityScope           IdentityScope `json:"identity_scope,omitempty"`
	InterpreterAcknowledged bool          `json:"interpreter_acknowledged,omitempty"`
}

type IdentityScope string

const (
	IdentityDedicatedExecutable IdentityScope = "dedicated_executable"
	IdentityInterpreterWide     IdentityScope = "interpreter_wide"
)

var interpreterExecutables = map[string]bool{
	"bash": true, "bun": true, "deno": true, "java": true, "node": true,
	"perl": true, "php": true, "python": true, "python2": true, "python3": true,
	"ruby": true, "sh": true, "zsh": true,
}

// ClassifyExecutable reports whether a peer pin identifies a dedicated binary
// or only a shared interpreter runtime. Script paths are intentionally not
// consulted: argv is attacker-controlled and is not a security boundary.
func ClassifyExecutable(path string) IdentityScope {
	base := strings.ToLower(filepath.Base(filepath.Clean(path)))
	if version := strings.TrimPrefix(base, "python"); version != base && version != "" && strings.Trim(version, "0123456789.") == "" {
		base = "python"
	}
	if interpreterExecutables[base] {
		return IdentityInterpreterWide
	}
	return IdentityDedicatedExecutable
}

// NewClientConstraint builds a grant client pin. Shared interpreters are
// refused unless the caller explicitly acknowledges interpreter-wide scope.
func NewClientConstraint(uid int, executablePath, executableHash string, allowInterpreterWide bool) (ClientConstraint, error) {
	scope := ClassifyExecutable(executablePath)
	if scope == IdentityInterpreterWide && !allowInterpreterWide {
		return ClientConstraint{}, fmt.Errorf("refusing interpreter-wide grant for %s; use a dedicated launcher or explicitly acknowledge interpreter-wide access", filepath.Base(executablePath))
	}
	return ClientConstraint{
		UID:                     uid,
		ExecutablePath:          executablePath,
		ExecutableHash:          executableHash,
		IdentityScope:           scope,
		InterpreterAcknowledged: scope == IdentityInterpreterWide,
	}, nil
}

// AccessGrant authorizes one constrained client to invoke selected operations
// against one binding.
type AccessGrant struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	BindingID          string           `json:"binding_id"`
	Client             ClientConstraint `json:"client"`
	Capabilities       []Capability     `json:"capabilities"`
	ComponentAllowlist []string         `json:"component_allowlist,omitempty"`
	Enabled            bool             `json:"enabled"`
	CreatedAt          time.Time        `json:"created_at"`
	ExpiresAt          *time.Time       `json:"expires_at,omitempty"`
}

// ApprovalIntent durably records a cross-domain approval spanning
// broker.json and vault.enc. Recovery rolls it back when its staged grant is
// absent; an enabled grant is the committed terminal state.
type ApprovalIntent struct {
	GrantID            string                    `json:"grant_id"`
	BindingID          string                    `json:"binding_id"`
	SecretID           string                    `json:"secret_id"`
	Capabilities       []Capability              `json:"capabilities"`
	PriorAllowResolve  bool                      `json:"prior_allow_resolve"`
	PriorAllowRotate   bool                      `json:"prior_allow_rotate"`
	PriorResolveSource secret.BrokerPolicySource `json:"prior_resolve_source,omitempty"`
	PriorRotateSource  secret.BrokerPolicySource `json:"prior_rotate_source,omitempty"`
	PriorPolicyKnown   bool                      `json:"prior_policy_known,omitempty"`
	AdminRevision      uint64                    `json:"admin_revision,omitempty"`
	CreatedAt          time.Time                 `json:"created_at"`
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

// FindBindingByID returns a pointer to the binding with the stable generated
// ID. Name and ID are intentionally separate lookup operations; callers doing
// transaction rechecks must use the exact identity they captured.
func (f *File) FindBindingByID(id string) *CredentialBinding {
	for i := range f.Bindings {
		if f.Bindings[i].ID == id {
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
		if !pathOK || !hashOK {
			return false
		}
	}
	if granted.ExecutablePath != "" && !pathOK {
		return false
	}
	if granted.ExecutableHash != "" && !hashOK {
		return false
	}
	actualScope := ClassifyExecutable(actual.ExecutablePath)
	grantedScope := granted.IdentityScope
	if grantedScope == "" {
		grantedScope = ClassifyExecutable(granted.ExecutablePath)
	}
	if actualScope == IdentityInterpreterWide {
		return grantedScope == IdentityInterpreterWide && granted.InterpreterAcknowledged
	}
	return grantedScope == "" || grantedScope == IdentityDedicatedExecutable
}

// SupportedInterpreter checks whether path is a recognized shared runtime.
// It is exported for CLI/TUI review copy without duplicating a list.
func SupportedInterpreter(path string) bool {
	return ClassifyExecutable(path) == IdentityInterpreterWide
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
	data, err := fsutil.ReadFile(canonical, 64<<20)
	if err != nil {
		return "", fmt.Errorf("open executable: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("hash executable: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

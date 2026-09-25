package broker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aegiskeys/internal/secret"
)

// Request is the parsed body of a broker operation. Secret values occur only
// in rotate requests and only over the protected local socket.
type Request struct {
	Binding string `json:"binding"`
	Value   string `json:"value,omitempty"`
}

// ResolvedCredential is the authorized resolve response. Components are
// non-empty named secret parts.
type ResolvedCredential struct {
	Binding    string            `json:"binding"`
	Kind       string            `json:"kind,omitempty"`
	EnvVar     string            `json:"env_var,omitempty"`
	Value      string            `json:"value,omitempty"`
	Components map[string]string `json:"components,omitempty"`
}

// Errors returned to clients are intentionally generic and never contain
// request values, vault paths, or record material.
var (
	ErrAccessDenied       = errors.New("access denied")
	ErrBindingUnavailable = errors.New("binding unavailable")
	ErrLocked             = errors.New("vault locked")
	ErrRequestInvalid     = errors.New("request invalid")
)

// Service binds the metadata store to the encrypted vault. It performs two
// independent authorization gates: executable grant and secret policy.
type Service struct {
	meta       *File
	vaultPath  string
	metaPath   string
	loadLatest func([32]byte) (*secret.Vault, error)
	mu         sync.Mutex
}

// NewService creates a broker service over one broker metadata snapshot.
func NewService(meta *File, vaultPath string) (*Service, error) {
	if meta == nil {
		return nil, errors.New("nil broker metadata")
	}
	service := &Service{meta: meta, vaultPath: vaultPath}
	// Production sessions receive the real broker.json. Test/in-memory services
	// may not have persisted metadata; retain their explicit snapshot.
	if _, err := os.Stat(metaPathForVault(vaultPath)); err == nil {
		service.metaPath = metaPathForVault(vaultPath)
	}
	return service, nil
}

func metaPathForVault(vaultPath string) string {
	dir := filepath.Dir(vaultPath)
	return filepath.Join(dir, "broker.json")
}

// currentMeta reloads and validates authorization metadata for every operation.
func (s *Service) currentMeta() (*File, error) {
	if s.metaPath == "" {
		return s.meta, nil
	}
	f, err := LoadBrokerFile(s.metaPath)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.meta = f
	s.mu.Unlock()
	return f, nil
}

// Resolve locates and returns only the credential components authorized by
// the binding's secret record. It decrypts only for the duration of this
// request and does not update LastUsedAt: an encrypted vault rewrite on every
// local read would add latency, disk churn, and writer contention; audit
// provides usage evidence.
func (s *Service) Resolve(name string, peer PeerIdentity) (ResolvedCredential, error) {
	if name == "" {
		return ResolvedCredential{}, ErrRequestInvalid
	}
	if s.metaPath == "" {
		return s.resolveSnapshot(s.meta, name, peer)
	}
	var out ResolvedCredential
	err := WithBrokerFileLocked(s.metaPath, func(meta *File) error {
		var err error
		out, err = s.resolveSnapshot(meta, name, peer)
		return err
	})
	if err != nil {
		return ResolvedCredential{}, err
	}
	return out, nil
}

func (s *Service) resolveSnapshot(meta *File, name string, peer PeerIdentity) (ResolvedCredential, error) {
	binding := meta.FindBinding(name)
	if binding == nil {
		return ResolvedCredential{}, ErrBindingUnavailable
	}
	grant := meta.FindGrant(binding.ID, peer.ClientConstraint(), CapabilityResolve)
	if grant == nil {
		return ResolvedCredential{}, ErrAccessDenied
	}
	v, err := s.load(peer.VaultKey)
	if err != nil {
		return ResolvedCredential{}, ErrLocked
	}
	defer secret.ZeroVault(v)
	rec := v.Get(binding.SecretID)
	if rec == nil || rec.Archived {
		return ResolvedCredential{}, ErrBindingUnavailable
	}
	if err := rec.AllowAccess(secret.AccessBrokerResolve); err != nil {
		return ResolvedCredential{}, ErrAccessDenied
	}

	out := ResolvedCredential{Binding: binding.Name, Kind: string(rec.Kind)}
	if rec.Secret != "" && componentAllowed(binding, "primary") && grantComponentAllowed(grant, "primary") {
		out.EnvVar = rec.EnvVarHint
		out.Value = rec.Secret
	}
	for _, component := range rec.ExtraSecrets {
		if !componentAllowed(binding, component.Key) || !grantComponentAllowed(grant, component.Key) {
			continue
		}
		if component.Secret == "" || component.EnvVar == "" {
			continue
		}
		if out.Components == nil {
			out.Components = make(map[string]string)
		}
		out.Components[component.EnvVar] = component.Secret
	}
	if out.Value == "" && len(out.Components) == 0 {
		return ResolvedCredential{}, ErrBindingUnavailable
	}
	return out, nil
}

func grantComponentAllowlist(components []string) []string {
	if len(components) == 0 {
		return []string{"primary"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(components))
	for _, c := range components {
		c = strings.TrimSpace(c)
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []string{"primary"}
	}
	return out
}
func grantComponentAllowed(g *AccessGrant, component string) bool {
	if g == nil {
		return false
	}
	if len(g.ComponentAllowlist) == 0 {
		return component == "primary"
	}
	for _, v := range g.ComponentAllowlist {
		if v == component {
			return true
		}
	}
	return false
}

// Rotate replaces only the primary secret material on the binding target.
// Labels, providers, policies, bindings, grants, and unrelated records are
// deliberately untouchable through this narrow endpoint.
func (s *Service) Rotate(req Request, peer PeerIdentity) error {
	if req.Binding == "" || req.Value == "" {
		return ErrRequestInvalid
	}
	if s.metaPath == "" {
		return s.rotateSnapshot(s.meta, req, peer)
	}
	return WithBrokerFileLocked(s.metaPath, func(meta *File) error { return s.rotateSnapshot(meta, req, peer) })
}

func (s *Service) rotateSnapshot(meta *File, req Request, peer PeerIdentity) error {
	binding := meta.FindBinding(req.Binding)
	if binding == nil {
		return ErrBindingUnavailable
	}
	if meta.FindGrant(binding.ID, peer.ClientConstraint(), CapabilityRotate) == nil {
		return ErrAccessDenied
	}
	return secret.MutateVaultWithKey(s.vaultPath, peer.VaultKey, func(v *secret.Vault) error {
		rec := v.Get(binding.SecretID)
		if rec == nil || rec.Archived {
			return ErrBindingUnavailable
		}
		if err := rec.AllowAccess(secret.AccessBrokerRotate); err != nil {
			return ErrAccessDenied
		}
		return v.Rotate(binding.SecretID, req.Value)
	})
}

func componentAllowed(binding *CredentialBinding, component string) bool {
	if len(binding.ComponentAllowlist) == 0 {
		return component == "primary"
	}
	for _, value := range binding.ComponentAllowlist {
		if value == component {
			return true
		}
	}
	return false
}

func (s *Service) load(key [32]byte) (*secret.Vault, error) {
	if s.loadLatest != nil {
		return s.loadLatest(key)
	}
	return secret.LoadVaultByKey(s.vaultPath, key)
}

// ClientConstraint converts the authenticated peer into grant-matching data.
func (p PeerIdentity) ClientConstraint() ClientConstraint {
	return ClientConstraint{
		UID:            p.UID,
		ExecutablePath: p.ExecutablePath,
		ExecutableHash: p.ExecutableHash,
	}
}

func nowPtr() *time.Time { t := time.Now(); return &t }

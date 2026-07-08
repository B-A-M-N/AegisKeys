package secret

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// SecretKind classifies the type of secret stored in the vault.
type SecretKind string

const (
	SecretAPIKey         SecretKind = "api_key"
	SecretBearerToken    SecretKind = "bearer_token"
	SecretWebhookSecret  SecretKind = "webhook_secret"
	SecretServiceAccount SecretKind = "service_account_json"
	SecretBasicAuth      SecretKind = "basic_auth"
	SecretGeneric        SecretKind = "generic_secret"
)

// RevealPolicy controls how a secret can be accessed.
type RevealPolicy string

const (
	RevealConfirm   RevealPolicy = "confirm"    // require confirmation before reveal
	RevealTypeLabel RevealPolicy = "type_label" // require typing the label to reveal
	RevealDeny      RevealPolicy = "deny"       // never reveal (launch-only)
)

// SecretPolicy controls what operations are permitted on a secret.
type SecretPolicy struct {
	AllowReveal       bool `json:"allow_reveal"`
	AllowClipboard    bool `json:"allow_clipboard"`
	AllowEnvExport    bool `json:"allow_env_export"`
	AllowLaunchInject bool `json:"allow_launch_injection"`
	AllowModelRefresh bool `json:"allow_model_refresh"`

	RequireConfirmForReveal bool `json:"require_confirm_for_reveal"`
	RequireConfirmForExport bool `json:"require_confirm_for_export"`

	MaxClipboardTTLSeconds int `json:"max_clipboard_ttl_seconds,omitempty"`
}

// DefaultSecretPolicy returns the default policy for a secret kind.
func DefaultSecretPolicy(kind SecretKind) SecretPolicy {
	switch kind {
	case SecretServiceAccount:
		return SecretPolicy{
			AllowReveal: true, AllowClipboard: true, AllowEnvExport: false, AllowLaunchInject: true,
			AllowModelRefresh:       true,
			RequireConfirmForReveal: true, RequireConfirmForExport: true,
			MaxClipboardTTLSeconds: 60,
		}
	case SecretGeneric:
		return SecretPolicy{
			AllowReveal: true, AllowClipboard: true, AllowEnvExport: false, AllowLaunchInject: true,
			AllowModelRefresh:       true,
			RequireConfirmForReveal: true, RequireConfirmForExport: true,
			MaxClipboardTTLSeconds: 60,
		}
	default:
		return SecretPolicy{
			AllowReveal: true, AllowClipboard: true, AllowEnvExport: false, AllowLaunchInject: true,
			AllowModelRefresh:       true,
			RequireConfirmForReveal: true, RequireConfirmForExport: true,
			MaxClipboardTTLSeconds: 60,
		}
	}
}

// AccessMode describes how a secret is being accessed.
type AccessMode string

const (
	AccessMaskedPreview AccessMode = "masked_preview"
	AccessCopyClipboard AccessMode = "copy_clipboard"
	AccessRevealStdout  AccessMode = "reveal_stdout"
	AccessInjectEnv     AccessMode = "inject_env"
	AccessRefreshModels AccessMode = "refresh_models"
)

// AccessError is returned when a secret's policy forbids an access mode.
type AccessError struct {
	Mode string
}

func (e *AccessError) Error() string {
	return "secret policy forbids access mode: " + e.Mode
}

// AllowAccess reports whether the secret's policy permits the given access
// mode. Use this at every access site (reveal, clipboard copy, env export,
// launch injection) so policy is actually enforced, not just stored.
func (r SecretRecord) AllowAccess(mode AccessMode) error {
	policy := r.Policy
	if policy == (SecretPolicy{}) {
		policy = DefaultSecretPolicy(r.Kind)
	}
	switch mode {
	case AccessMaskedPreview:
		return nil // always allowed
	case AccessCopyClipboard:
		if !policy.AllowClipboard {
			return &AccessError{Mode: string(mode)}
		}
	case AccessRevealStdout:
		if !policy.AllowReveal {
			return &AccessError{Mode: string(mode)}
		}
	case AccessInjectEnv:
		if !policy.AllowLaunchInject {
			return &AccessError{Mode: string(mode)}
		}
	case AccessRefreshModels:
		if !policy.AllowModelRefresh {
			return &AccessError{Mode: string(mode)}
		}
	}
	return nil
}

// SecretRecord is a single encrypted vault item. It can represent any kind of
// credential — API key, token, webhook secret, service account — with optional
// provider linkage, usage hints, rotation metadata, and access policies.
type SecretRecord struct {
	ID           string     `json:"id"`
	Kind         SecretKind `json:"kind"`
	ProviderSlug string     `json:"provider_slug,omitempty"`

	// User-facing identity.
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Account     string   `json:"account,omitempty"`
	Project     string   `json:"project,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// Secret payload. Never JSON-serialized directly.
	Secret string `json:"-"`

	// Non-secret usage hints.
	EnvVarHint  string `json:"env_var_hint,omitempty"`
	HeaderHint  string `json:"header_hint,omitempty"`
	BaseURLHint string `json:"base_url_hint,omitempty"`
	DocsURL     string `json:"docs_url,omitempty"`

	// Private note — encrypted with vault payload, excluded from Serialize.
	PrivateNote string `json:"-"`

	// Lifecycle.
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RotatesAt     *time.Time `json:"rotates_at,omitempty"`
	LastRotatedAt *time.Time `json:"last_rotated_at,omitempty"`

	// Policy.
	RevealPolicy RevealPolicy `json:"reveal_policy,omitempty"`
	Exportable   bool         `json:"exportable,omitempty"`
	Archived     bool         `json:"archived,omitempty"`
	Policy       SecretPolicy `json:"policy,omitzero"`
}

const SecretRecordVersion = 2

// migrateSecretV1ToV2 fills in defaults for records loaded from older vaults.
func migrateSecretV1ToV2(r *SecretRecord) {
	if r.Kind == "" {
		r.Kind = SecretAPIKey
	}
	if r.Policy == (SecretPolicy{}) {
		r.Policy = DefaultSecretPolicy(r.Kind)
	}
	if r.RevealPolicy == "" {
		r.RevealPolicy = RevealConfirm
	}
}

type Vault struct {
	Version     int                `json:"version"`
	Keys        []SecretRecord     `json:"keys"`
	ScratchPads []ScratchPadRecord `json:"scratch_pads,omitempty"`
}

// ScratchPadKind categorizes a scratchpad page.
type ScratchPadKind string

const (
	ScratchPadGeneral  ScratchPadKind = "general"
	ScratchPadProvider ScratchPadKind = "provider"
	ScratchPadKey      ScratchPadKind = "key"
)

// ScratchPadRecord is a free-form encrypted note page stored inside the vault.
// The Body is secret — it can contain billing notes, dashboard links, setup
// instructions, pasted credentials, or anything the user wants encrypted
// at rest. It is NEVER written to providers.json, profiles, audit logs, or
// app config files.
type ScratchPadRecord struct {
	ID           string         `json:"id"`
	Kind         ScratchPadKind `json:"kind"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	ProviderSlug string         `json:"provider_slug,omitempty"`
	KeyID        string         `json:"key_id,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Archived     bool           `json:"archived,omitempty"`
}

type MaskedKeyItem struct {
	ID           string   `json:"id"`
	ProviderSlug string   `json:"provider_slug"`
	Kind         string   `json:"kind"`
	Label        string   `json:"label"`
	Description  string   `json:"description,omitempty"`
	Account      string   `json:"account,omitempty"`
	Project      string   `json:"project,omitempty"`
	MaskedSecret string   `json:"masked_secret"`
	LastUsed     string   `json:"last_used"`
	Tags         []string `json:"tags"`
	EnvVarHint   string   `json:"env_var_hint,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	LastRotated  string   `json:"last_rotated,omitempty"`
	Archived     bool     `json:"archived"`
}

func NewID() (string, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate secret id: %w", err)
	}
	return fmt.Sprintf("key_%x", buf), nil
}

func MaskSecret(secret string) string {
	if len(secret) == 0 {
		return "<hidden>"
	}
	if len(secret) <= 8 {
		return "<hidden>"
	}
	suffix := secret[len(secret)-4:]
	return fmt.Sprintf("...%s", suffix)
}

func ToMasked(record SecretRecord) MaskedKeyItem {
	lastUsed := "never"
	if record.LastUsedAt != nil {
		lastUsed = record.LastUsedAt.Format("2006-01-02")
	}
	expiresAt := ""
	if record.ExpiresAt != nil {
		expiresAt = record.ExpiresAt.Format("2006-01-02")
	}
	createdAt := record.CreatedAt.Format("2006-01-02")
	lastRotated := ""
	if record.LastRotatedAt != nil {
		lastRotated = record.LastRotatedAt.Format("2006-01-02")
	}
	return MaskedKeyItem{
		ID:           record.ID,
		ProviderSlug: record.ProviderSlug,
		Kind:         string(record.Kind),
		Label:        record.Label,
		Description:  record.Description,
		Account:      record.Account,
		Project:      record.Project,
		MaskedSecret: MaskSecret(record.Secret),
		LastUsed:     lastUsed,
		Tags:         record.Tags,
		EnvVarHint:   record.EnvVarHint,
		ExpiresAt:    expiresAt,
		CreatedAt:    createdAt,
		LastRotated:  lastRotated,
		Archived:     record.Archived,
	}
}

func ToMaskedList(records []SecretRecord) []MaskedKeyItem {
	out := make([]MaskedKeyItem, len(records))
	for i, r := range records {
		out[i] = ToMasked(r)
	}
	return out
}

// Serialize returns JSON-safe representation (secrets removed, private notes removed).
func (v *Vault) Serialize() ([]byte, error) {
	type SafeRecord struct {
		ID           string       `json:"id"`
		Kind         SecretKind   `json:"kind"`
		ProviderSlug string       `json:"provider_slug,omitempty"`
		Label        string       `json:"label"`
		Description  string       `json:"description,omitempty"`
		Account      string       `json:"account,omitempty"`
		Project      string       `json:"project,omitempty"`
		Tags         []string     `json:"tags,omitempty"`
		EnvVarHint   string       `json:"env_var_hint,omitempty"`
		HeaderHint   string       `json:"header_hint,omitempty"`
		BaseURLHint  string       `json:"base_url_hint,omitempty"`
		DocsURL      string       `json:"docs_url,omitempty"`
		CreatedAt    time.Time    `json:"created_at"`
		UpdatedAt    time.Time    `json:"updated_at"`
		LastUsedAt   *time.Time   `json:"last_used_at,omitempty"`
		ExpiresAt    *time.Time   `json:"expires_at,omitempty"`
		RotatesAt    *time.Time   `json:"rotates_at,omitempty"`
		RevealPolicy RevealPolicy `json:"reveal_policy,omitempty"`
		Exportable   bool         `json:"exportable,omitempty"`
		Archived     bool         `json:"archived,omitempty"`
		Policy       SecretPolicy `json:"policy,omitzero"`
	}
	type SafeScratchPad struct {
		ID           string         `json:"id"`
		Kind         ScratchPadKind `json:"kind"`
		Title        string         `json:"title"`
		ProviderSlug string         `json:"provider_slug,omitempty"`
		KeyID        string         `json:"key_id,omitempty"`
		Tags         []string       `json:"tags,omitempty"`
		CreatedAt    time.Time      `json:"created_at"`
		UpdatedAt    time.Time      `json:"updated_at"`
		Archived     bool           `json:"archived,omitempty"`
	}
	type SafeVault struct {
		Version     int              `json:"version"`
		Keys        []SafeRecord     `json:"keys"`
		ScratchPads []SafeScratchPad `json:"scratch_pads,omitempty"`
	}
	sv := SafeVault{Version: v.Version}
	for _, k := range v.Keys {
		sv.Keys = append(sv.Keys, SafeRecord{
			ID: k.ID, Kind: k.Kind, ProviderSlug: k.ProviderSlug,
			Label: k.Label, Description: k.Description, Account: k.Account, Project: k.Project,
			Tags: k.Tags, EnvVarHint: k.EnvVarHint, HeaderHint: k.HeaderHint, BaseURLHint: k.BaseURLHint, DocsURL: k.DocsURL,
			CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt, LastUsedAt: k.LastUsedAt,
			ExpiresAt: k.ExpiresAt, RotatesAt: k.RotatesAt,
			RevealPolicy: k.RevealPolicy, Exportable: k.Exportable, Archived: k.Archived, Policy: k.Policy,
		})
	}
	for _, s := range v.ScratchPads {
		sv.ScratchPads = append(sv.ScratchPads, SafeScratchPad{
			ID: s.ID, Kind: s.Kind, Title: s.Title, ProviderSlug: s.ProviderSlug,
			KeyID: s.KeyID, Tags: s.Tags, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Archived: s.Archived,
		})
	}
	return json.MarshalIndent(sv, "", " ")
}

// RedactEnv redacts secret values in KEY=VALUE pairs.
// Deprecated: use redact.NewRedactor from internal/redact instead.
func RedactEnv(in string) string {
	parts := strings.SplitN(in, "=", 2)
	if len(parts) == 2 {
		return parts[0] + "=<redacted>"
	}
	return in
}

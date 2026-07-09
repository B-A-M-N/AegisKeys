package provider

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"aegiskeys/internal/sensitive"
)

// Protocol describes the API wire format a provider speaks.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
	ProtocolGoogle    Protocol = "google"
	ProtocolLocal     Protocol = "local"
)

// AuthSpec describes how to authenticate with a provider.
type AuthSpec struct {
	// Type is one of: "bearer", "header", "query", "none".
	Type string `json:"type"`
	// HeaderName is the HTTP header for "header"/"bearer" auth (e.g. "Authorization").
	HeaderName string `json:"header_name,omitempty"`
	// Prefix is the value prefix for bearer auth (e.g. "Bearer ").
	Prefix string `json:"prefix,omitempty"`
	// EnvVar is the environment variable that holds the secret.
	EnvVar string `json:"env_var"`
}

// EndpointSpec describes the API endpoints.
type EndpointSpec struct {
	BaseURL   string `json:"base_url"`
	APIPath   string `json:"api_path,omitempty"`
	Version   string `json:"version,omitempty"`
	ModelsURL string `json:"models_url,omitempty"`
	// URLTemplate is an optional endpoint template supporting {field}
	// substitution from a key's Setup values. Used when the endpoint embeds
	// a provider-specific parameter (e.g. Azure resource name, Bedrock
	// region). When empty, CanonicalBaseURL is authoritative.
	URLTemplate string `json:"url_template,omitempty"`
}

// ModelCatalogSpec describes how to discover and refresh models.
type ModelCatalogSpec struct {
	// Source is one of: "static", "dynamic", "manual", "local".
	Source string `json:"source"`
	// RefreshURL is the endpoint for dynamic model refresh.
	RefreshURL string `json:"refresh_url,omitempty"`
	// CacheTTLMinutes is how long to cache the model list.
	CacheTTLMinutes int `json:"cache_ttl_minutes,omitempty"`
	// AuthRequired indicates whether the models endpoint needs authentication.
	AuthRequired bool `json:"auth_required,omitempty"`
}

// Capabilities describes what a provider supports.
type Capabilities struct {
	ToolUse         bool `json:"tool_use"`
	Vision          bool `json:"vision"`
	Streaming       bool `json:"streaming"`
	FunctionCalling bool `json:"function_calling"`
	MaxContext      int  `json:"max_context,omitempty"`
}

// SetupParam declares a value a provider needs beyond the primary secret.
// Secret=false  -> a non-secret configuration value (e.g. Azure resource name,
//
//	deployment name, api-version; Bedrock region). Injected as EnvVar at
//	launch when present on the key.
//
// Secret=true   -> an additional secret component (e.g. an AWS secret access
//
//	key). Injected as EnvVar at launch from the key's ExtraSecrets.
type SetupParam struct {
	// Key is the stable identifier used as the key in SecretRecord.Fields
	// (for non-secret params) or to match a NamedSecret.Key (for secrets).
	Key string `json:"key"`
	// Label is the human-readable prompt shown when collecting the value.
	Label string `json:"label"`
	// EnvVar is the environment variable name injected at launch. Empty means
	// the value is collected/stored but not injected as a standalone env var
	// (e.g. an Azure deployment name, which is supplied as the profile model).
	EnvVar string `json:"env_var,omitempty"`
	// Default is an optional default value offered at collection time.
	Default string `json:"default,omitempty"`
	// Example is an optional example shown to the user.
	Example string `json:"example,omitempty"`
	// Required marks the param as mandatory for a usable credential.
	Required bool `json:"required"`
	// Secret marks the param as an additional secret component.
	Secret bool `json:"secret"`
	// Endpoint, when true, makes injection resolve the provider's endpoint
	// URL (Endpoints.URLTemplate with this param's value) and assign it to
	// EnvVar, rather than injecting the raw field value. Used for the Azure
	// resource name, whose env var (AZURE_OPENAI_ENDPOINT) expects the full
	// https URL, not just the resource string.
	Endpoint bool `json:"endpoint,omitempty"`
	// Help is an optional explanatory note shown during collection.
	Help string `json:"help,omitempty"`
}

// AppHint provides per-application configuration hints for a provider.
type AppHint struct {
	// App is the target application (e.g. "aider", "crush").
	App string `json:"app"`
	// EnvVars maps env var names to their descriptions.
	EnvVars map[string]string `json:"env_vars,omitempty"`
	// ConfigFiles lists config files this app needs.
	ConfigFiles []string `json:"config_files,omitempty"`
	// Notes contains human-readable setup notes.
	Notes string `json:"notes,omitempty"`
}

// Provider is an API provider's metadata. It never contains secrets.
type Provider struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Slug       string            `json:"slug"`
	BaseURL    string            `json:"base_url"`
	EnvVar     string            `json:"env_var"`
	AuthHeader string            `json:"auth_header"`
	ExtraEnv   map[string]string `json:"extra_env,omitempty"`

	// Compatibility is the API wire format this provider speaks.
	Compatibility CompatibilityMode `json:"compatibility,omitempty"`

	// Models is the curated/discovered model list. For dynamic providers
	// this may be refreshed from the API; for static providers it is fixed.
	Models []ProviderModel `json:"models,omitempty"`

	// ModelPolicy controls how the model catalog is managed.
	ModelPolicy ModelCatalogPolicy `json:"model_policy,omitempty"`

	// Protocol is the wire format (openai, anthropic, google, local).
	Protocol Protocol `json:"protocol,omitempty"`

	// Auth describes how to authenticate.
	Auth AuthSpec `json:"auth,omitempty"`

	// Endpoints describes the API endpoints.
	Endpoints EndpointSpec `json:"endpoints,omitempty"`

	// Catalog describes how to discover and refresh models.
	Catalog ModelCatalogSpec `json:"catalog,omitempty"`

	// Capabilities describes what the provider supports.
	Capabilities Capabilities `json:"capabilities,omitempty"`

	// AppHints provides per-application configuration hints.
	AppHints []AppHint `json:"app_hints,omitempty"`

	// Setup declares additional values (non-secret config and/or secondary
	// secrets) the provider needs beyond the primary secret. Collected at key
	// creation and injected at launch. Used by providers such as Azure OpenAI
	// (resource, deployment, api-version) and AWS Bedrock (secret access key,
	// region) whose requirements exceed a single API key.
	Setup []SetupParam `json:"setup,omitempty"`

	Tags      []string  `json:"tags,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ModelCatalogPolicy controls model discovery and refresh behavior.
type ModelCatalogPolicy struct {
	Source      ModelSource `json:"source"`                  // static, dynamic, manual, local
	RefreshURL  string      `json:"refresh_url,omitempty"`   // endpoint for dynamic refresh
	CacheTTLMin int         `json:"cache_ttl_min,omitempty"` // minutes between refreshes
}

// DisplayName returns the human-readable name with compatibility indicator.
func (p Provider) DisplayName() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Slug
}

// NeedsKey reports whether this provider requires an API key. Explicit auth
// types always need one regardless of metadata completeness; anything else
// needs one only if a credential env var is actually defined.
func (p Provider) NeedsKey() bool {
	switch p.Auth.Type {
	case "none":
		return false
	case "bearer", "header", "query", "aws":
		return true
	}
	// Legacy flat-field fallback.
	if p.AuthHeader == "none" {
		return false
	}
	return p.CanonicalEnvVar() != ""
}

// CanonicalEnvVar returns the environment variable name for the API key.
// Prefers the structured Auth.EnvVar, falls back to flat EnvVar.
func (p Provider) CanonicalEnvVar() string {
	if p.Auth.EnvVar != "" {
		return p.Auth.EnvVar
	}
	return p.EnvVar
}

// CanonicalBaseURL returns the provider's base URL.
// Prefers the structured Endpoints.BaseURL, falls back to flat BaseURL.
func (p Provider) CanonicalBaseURL() string {
	if p.Endpoints.BaseURL != "" {
		return p.Endpoints.BaseURL
	}
	return p.BaseURL
}

// ResolveEndpoint returns the launch-time endpoint URL, substituting {field}
// placeholders in Endpoints.URLTemplate with the provided setup values. When
// no template is declared, it returns CanonicalBaseURL unchanged. Unmatched
// placeholders are left as-is so the result is always a usable string.
func (p Provider) ResolveEndpoint(fields map[string]string) string {
	tpl := p.Endpoints.URLTemplate
	if tpl == "" {
		return p.CanonicalBaseURL()
	}
	var sb strings.Builder
	for i := 0; i < len(tpl); i++ {
		if tpl[i] == '{' {
			end := strings.IndexByte(tpl[i:], '}')
			if end > 0 {
				key := tpl[i+1 : i+end]
				if v, ok := fields[key]; ok && v != "" {
					sb.WriteString(v)
				} else {
					sb.WriteString(tpl[i : i+end+1])
				}
				i += end
				continue
			}
		}
		sb.WriteByte(tpl[i])
	}
	return sb.String()
}

// ModelRefreshURL resolves the best endpoint for fetching this provider's model
// catalog. It prefers explicit configuration (Catalog.RefreshURL, ModelPolicy.
// RefreshURL, Endpoints.ModelsURL) before deriving one from the base URL.
func (p Provider) ModelRefreshURL() string {
	if p.Catalog.RefreshURL != "" {
		return p.Catalog.RefreshURL
	}
	if p.ModelPolicy.RefreshURL != "" {
		return p.ModelPolicy.RefreshURL
	}
	if p.Endpoints.ModelsURL != "" {
		return p.Endpoints.ModelsURL
	}
	base := strings.TrimRight(p.CanonicalBaseURL(), "/")
	if base == "" {
		return ""
	}
	// Local loopback providers without an explicit models URL: do not guess.
	if u, err := url.Parse(base); err == nil && isLoopbackHost(u.Hostname()) {
		return ""
	}
	if p.Compatibility == CompatOpenAI || p.Compatibility == CompatLocal || p.Compatibility == "" {
		return base + "/models"
	}
	return ""
}

// CanRefreshModels reports whether this provider can have its model list
// refreshed from an external endpoint. Local providers (Ollama, LM Studio,
// vLLM) can always discover models via their local API. Remote providers
// need either an explicit refresh URL or a derivable base URL.
func (p Provider) CanRefreshModels() bool {
	if p.Catalog.Source == "local" || p.Protocol == ProtocolLocal {
		return true
	}
	return p.ModelRefreshURL() != ""
}

// Normalize migrates flat legacy fields into their structured counterparts
// and infers missing compatibility/protocol/auth metadata from the base URL.
// It only fills in EMPTY fields — user-set values are never overwritten.
// This makes a provider created with just name + slug + env var + base URL
// automatically usable by the adapter layer without manual enum selection.
func (p *Provider) Normalize() {
	if p.Slug == "" {
		p.Slug = p.ID
	}
	if p.ID == "" {
		p.ID = p.Slug
	}
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	p.Name = strings.TrimSpace(p.Name)
	p.EnvVar = strings.TrimSpace(p.EnvVar)
	p.BaseURL = strings.TrimSpace(p.BaseURL)

	// Migrate flat fields into structured counterparts.
	if p.Auth.EnvVar == "" && p.EnvVar != "" {
		p.Auth.EnvVar = p.EnvVar
	}
	if p.Endpoints.BaseURL == "" && p.BaseURL != "" {
		p.Endpoints.BaseURL = p.BaseURL
	}

	// Infer compatibility / protocol / auth from the base URL.
	base := p.CanonicalBaseURL()
	env := p.CanonicalEnvVar()
	lowerBase := strings.ToLower(base)

	switch {
	case strings.Contains(lowerBase, "api.anthropic.com"):
		if p.Compatibility == "" {
			p.Compatibility = CompatAnthropic
		}
		if p.Protocol == "" {
			p.Protocol = ProtocolAnthropic
		}
		if p.Auth.Type == "" && env != "" {
			p.Auth = AuthSpec{Type: "header", HeaderName: "x-api-key", EnvVar: env}
		}

	case strings.Contains(lowerBase, "generativelanguage.googleapis.com"):
		if p.Compatibility == "" {
			p.Compatibility = CompatGoogle
		}
		if p.Protocol == "" {
			p.Protocol = ProtocolGoogle
		}
		if p.Auth.Type == "" && env != "" {
			p.Auth = AuthSpec{Type: "query", EnvVar: env}
		}

	case strings.Contains(lowerBase, "localhost") ||
		strings.Contains(lowerBase, "127.0.0.1") ||
		strings.Contains(lowerBase, "0.0.0.0") ||
		strings.Contains(lowerBase, "::1"):
		if p.Compatibility == "" {
			p.Compatibility = CompatLocal
		}
		if p.Protocol == "" {
			p.Protocol = ProtocolLocal
		}
		if p.Auth.Type == "" {
			p.Auth = AuthSpec{Type: "none"}
		}

	default:
		// Most custom routers / remote providers are OpenAI-compatible.
		if p.Compatibility == "" {
			p.Compatibility = CompatOpenAI
		}
		if p.Protocol == "" {
			p.Protocol = ProtocolOpenAI
		}
		if p.Auth.Type == "" && env != "" {
			p.Auth.Type = "bearer"
		}
	}

	// Fill bearer header defaults when type is bearer and header is blank.
	if p.Auth.Type == "bearer" {
		if p.Auth.HeaderName == "" {
			p.Auth.HeaderName = "Authorization"
		}
		if p.Auth.Prefix == "" {
			p.Auth.Prefix = "Bearer "
		}
	}

	// Populate AuthHeader template for convenience when blank.
	if p.AuthHeader == "" && env != "" {
		switch p.Auth.Type {
		case "bearer":
			p.AuthHeader = "Authorization: Bearer ${KEY}"
		case "header":
			p.AuthHeader = p.Auth.HeaderName + ": ${KEY}"
		}
	}

	// Ensure Endpoints.BaseURL is set after URL detection.
	if p.Endpoints.BaseURL == "" && p.BaseURL != "" {
		p.Endpoints.BaseURL = p.BaseURL
	}

	// Default catalog/model-policy sources.
	if p.Catalog.Source == "" {
		if p.Compatibility == CompatLocal {
			p.Catalog.Source = "local"
		} else {
			p.Catalog.Source = "manual"
		}
	}
	if p.ModelPolicy.Source == "" {
		if p.Compatibility == CompatLocal {
			p.ModelPolicy.Source = ModelSourceLocal
		} else {
			p.ModelPolicy.Source = ModelSourceManual
		}
	}
}

// NormalizeAll normalizes every provider in the registry in place.
func (r *Registry) NormalizeAll() {
	for i := range r.Providers {
		r.Providers[i].Normalize()
	}
}

// ModelByID finds a model in the provider's catalog by ID.
func (p Provider) ModelByID(id string) *ProviderModel {
	for i := range p.Models {
		if p.Models[i].ID == id {
			return &p.Models[i]
		}
	}
	return nil
}

// ModelByName finds a model by name or alias (case-insensitive).
func (p Provider) ModelByName(name string) *ProviderModel {
	lower := toLower(name)
	for i := range p.Models {
		if toLower(p.Models[i].Name) == lower || toLower(p.Models[i].ID) == lower {
			return &p.Models[i]
		}
		for _, a := range p.Models[i].Aliases {
			if toLower(a) == lower {
				return &p.Models[i]
			}
		}
	}
	return nil
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

// Validate checks the provider for required fields.
func (p *Provider) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	if p.Slug == "" {
		return fmt.Errorf("provider slug is required")
	}
	return nil
}

// ValidateStrict performs deep validation that rejects dangerous metadata.
// Provider metadata is NOT encrypted, so it must never contain secrets.
func (p *Provider) ValidateStrict() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !validSlug(p.Slug) {
		return fmt.Errorf("invalid provider slug %q (must be alphanumeric, hyphens, underscores)", p.Slug)
	}
	// Auth provider must define an env var; local/none providers must not.
	if p.Auth.Type != "none" && p.CanonicalEnvVar() == "" {
		return fmt.Errorf("auth provider %q must define an env var", p.Slug)
	}
	// Validate env var name format.
	if !ValidEnvVar(p.CanonicalEnvVar()) {
		return fmt.Errorf("provider %q has invalid env var name %q", p.Slug, p.CanonicalEnvVar())
	}
	// Validate base URL format.
	if !ValidBaseURL(p.CanonicalBaseURL()) {
		return fmt.Errorf("provider %q has invalid base URL %q", p.Slug, p.CanonicalBaseURL())
	}
	// Enforce HTTPS for non-local providers.
	if err := p.validateBaseURLSecurity(); err != nil {
		return err
	}
	// Validate auth type is a known value.
	switch p.Auth.Type {
	case "bearer", "header", "query", "none", "aws":
		// ok
	default:
		return fmt.Errorf("provider %q has unknown auth type %q", p.Slug, p.Auth.Type)
	}
	// Reject secrets accidentally pasted into metadata fields.
	if sensitive.IsSecretValue(p.BaseURL) {
		return fmt.Errorf("provider %q base URL appears to contain a secret", p.Slug)
	}
	if sensitive.IsSecretValue(p.AuthHeader) {
		return fmt.Errorf("provider %q auth header appears to contain a secret", p.Slug)
	}
	// Reject auth headers that contain key material instead of ${KEY} template.
	if p.AuthHeader != "" && sensitive.IsSecretValue(p.AuthHeader) && !strings.Contains(p.AuthHeader, "${KEY}") {
		return fmt.Errorf("provider %q auth header appears to contain raw key material (use ${KEY} template)", p.Slug)
	}
	for k, v := range p.ExtraEnv {
		if sensitive.IsSecretName(k) || sensitive.IsSecretValue(v) {
			return fmt.Errorf("provider %q ExtraEnv %q looks secret; store it as a key/profile env instead", p.Slug, k)
		}
	}
	return nil
}

// validSlug reports whether a slug is safe for filesystem/URL use.
func validSlug(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// isLoopbackHost reports whether host refers to localhost or a link-local address.
func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.Split(host, ":")[0])
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0"
}

// validateBaseURLSecurity enforces that non-local providers use HTTPS.
func (p *Provider) validateBaseURLSecurity() error {
	raw := p.CanonicalBaseURL()
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHost(u.Hostname()) {
		return nil
	}
	if p.Compatibility == CompatLocal || p.Protocol == ProtocolLocal {
		return nil
	}
	return fmt.Errorf("remote provider %q must use https base URL", p.Slug)
}

// --- Compatibility & Model Types ---

// CompatibilityMode describes which API wire format the provider speaks.
type CompatibilityMode string

const (
	CompatOpenAI    CompatibilityMode = "openai"
	CompatAnthropic CompatibilityMode = "anthropic"
	CompatGoogle    CompatibilityMode = "google"
	CompatLocal     CompatibilityMode = "local"
)

// ModelSource describes how a provider's model catalog is managed.
type ModelSource string

const (
	ModelSourceStatic  ModelSource = "static"
	ModelSourceDynamic ModelSource = "dynamic"
	ModelSourceManual  ModelSource = "manual"
	ModelSourceLocal   ModelSource = "local"
)

// ProviderModel is a single model available from a provider.
type ProviderModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	ContextSize int      `json:"context_size,omitempty"`
	InputTypes  []string `json:"input_types,omitempty"`
	OutputTypes []string `json:"output_types,omitempty"`
	Static      bool     `json:"static,omitempty"`
}

// --- Registry ---

// Registry is the in-memory collection of providers.
type Registry struct {
	Providers []Provider `json:"providers"`
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{Providers: []Provider{}}
}

// defaultProviders is the curated list of known providers.
var defaultProviders = []Provider{
	{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL: "https://api.openai.com/v1", EnvVar: "OPENAI_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "OPENAI_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.openai.com/v1", APIPath: "/chat/completions"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "gpt-5", Name: "GPT-5", ContextSize: 128000},
			{ID: "gpt-5-mini", Name: "GPT-5 Mini", ContextSize: 128000},
			{ID: "gpt-4.1", Name: "GPT-4.1", ContextSize: 128000},
			{ID: "gpt-4o", Name: "GPT-4o", ContextSize: 128000},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "chat", "paid"},
	},
	{
		ID: "anthropic", Name: "Anthropic", Slug: "anthropic",
		BaseURL: "https://api.anthropic.com", EnvVar: "ANTHROPIC_API_KEY",
		AuthHeader:    "x-api-key: ${KEY}",
		Compatibility: CompatAnthropic, Protocol: ProtocolAnthropic,
		Auth:      AuthSpec{Type: "header", HeaderName: "x-api-key", EnvVar: "ANTHROPIC_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.anthropic.com", APIPath: "/v1/messages"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "claude-opus-4-5", Name: "Claude Opus 4.5", ContextSize: 200000},
			{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", ContextSize: 200000},
			{ID: "claude-haiku-4-5", Name: "Claude Haiku 4", ContextSize: 200000},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "chat", "paid"},
	},
	{
		ID: "openrouter", Name: "OpenRouter", Slug: "openrouter",
		BaseURL: "https://openrouter.ai/api/v1", EnvVar: "OPENROUTER_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:         AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "OPENROUTER_API_KEY"},
		Endpoints:    EndpointSpec{BaseURL: "https://openrouter.ai/api/v1", APIPath: "/chat/completions", ModelsURL: "https://openrouter.ai/api/v1/models"},
		Catalog:      ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://openrouter.ai/api/v1/models"},
		ExtraEnv:     map[string]string{"OPENAI_BASE_URL": "https://openrouter.ai/api/v1"},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://openrouter.ai/api/v1/models"},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"router", "coding", "paid", "free-tier"},
	},
	{
		ID: "mistral", Name: "Mistral", Slug: "mistral",
		BaseURL: "https://api.mistral.ai/v1", EnvVar: "MISTRAL_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "MISTRAL_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.mistral.ai/v1"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "mistral-large-latest", Name: "Mistral Large", ContextSize: 128000},
			{ID: "mistral-medium-latest", Name: "Mistral Medium", ContextSize: 32000},
			{ID: "mistral-small-latest", Name: "Mistral Small", ContextSize: 32000},
			{ID: "devstral-latest", Name: "Devstral", ContextSize: 128000},
		},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceStatic},
		Tags:        []string{"coding", "chat", "paid", "free-tier"},
	},
	{
		ID: "gemini", Name: "Google Gemini", Slug: "gemini",
		BaseURL: "https://generativelanguage.googleapis.com", EnvVar: "GEMINI_API_KEY",
		Compatibility: CompatGoogle, Protocol: ProtocolGoogle,
		Auth:      AuthSpec{Type: "query", EnvVar: "GEMINI_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://generativelanguage.googleapis.com"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "gemini-3-pro", Name: "Gemini 3 Pro", ContextSize: 1000000},
			{ID: "gemini-3-flash", Name: "Gemini 3 Flash", ContextSize: 1000000},
		},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceStatic},
		Tags:        []string{"coding", "chat", "multimodal", "paid", "free-tier"},
	},
	{
		ID: "groq", Name: "Groq", Slug: "groq",
		BaseURL: "https://api.groq.com/openai/v1", EnvVar: "GROQ_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "GROQ_API_KEY"},
		Endpoints:   EndpointSpec{BaseURL: "https://api.groq.com/openai/v1"},
		Catalog:     ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://api.groq.com/openai/v1/models"},
		ExtraEnv:    map[string]string{"OPENAI_BASE_URL": "https://api.groq.com/openai/v1"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://api.groq.com/openai/v1/models"},
		Tags:        []string{"fast", "coding", "paid", "free-tier"},
	},
	{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL: "http://localhost:11434/v1", AuthHeader: "none",
		Compatibility: CompatLocal, Protocol: ProtocolLocal,
		Auth:        AuthSpec{Type: "none"},
		Endpoints:   EndpointSpec{BaseURL: "http://localhost:11434/v1", ModelsURL: "http://localhost:11434/api/tags"},
		Catalog:     ModelCatalogSpec{Source: "local"},
		ExtraEnv:    map[string]string{"OPENAI_BASE_URL": "http://localhost:11434/v1"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceLocal},
		Tags:        []string{"local", "no-key", "openai-compatible"},
	},
	{
		ID: "lmstudio", Name: "LM Studio", Slug: "lmstudio",
		BaseURL: "http://localhost:1234/v1", AuthHeader: "none",
		Compatibility: CompatLocal, Protocol: ProtocolLocal,
		Auth:        AuthSpec{Type: "none"},
		Endpoints:   EndpointSpec{BaseURL: "http://localhost:1234/v1"},
		Catalog:     ModelCatalogSpec{Source: "local"},
		ExtraEnv:    map[string]string{"OPENAI_BASE_URL": "http://localhost:1234/v1"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceLocal},
		Tags:        []string{"local", "no-key", "openai-compatible"},
	},
	{
		ID: "huggingface", Name: "Hugging Face", Slug: "huggingface",
		BaseURL: "https://router.huggingface.co/v1", EnvVar: "HF_TOKEN",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "HF_TOKEN"},
		Endpoints:   EndpointSpec{BaseURL: "https://router.huggingface.co/v1"},
		Catalog:     ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://router.huggingface.co/v1/models"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://router.huggingface.co/v1/models"},
		Tags:        []string{"router", "coding", "paid", "free-tier"},
	},
	{
		ID: "cerebras", Name: "Cerebras", Slug: "cerebras",
		BaseURL: "https://api.cerebras.ai/v1", EnvVar: "CEREBRAS_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "CEREBRAS_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.cerebras.ai/v1"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "llama3.1-8b", Name: "Llama 3.1 8B", ContextSize: 8192},
			{ID: "llama3.3-70b", Name: "Llama 3.3 70B", ContextSize: 8192},
			{ID: "qwen-3-235b", Name: "Qwen 3 235B", ContextSize: 128000},
		},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceStatic},
		Tags:        []string{"fast", "coding"},
	},
	{
		ID: "together", Name: "Together", Slug: "together",
		BaseURL: "https://api.together.xyz/v1", EnvVar: "TOGETHER_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "TOGETHER_API_KEY"},
		Endpoints:   EndpointSpec{BaseURL: "https://api.together.xyz/v1"},
		Catalog:     ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://api.together.xyz/v1/models"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://api.together.xyz/v1/models"},
		Tags:        []string{"coding", "paid"},
	},
	{
		ID: "bedrock", Name: "AWS Bedrock", Slug: "bedrock",
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com", EnvVar: "AWS_ACCESS_KEY_ID",
		AuthHeader:    "AWS Signature v4",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "aws", EnvVar: "AWS_ACCESS_KEY_ID"},
		Endpoints:   EndpointSpec{BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com", URLTemplate: "https://bedrock-runtime.{region}.amazonaws.com"},
		Catalog:     ModelCatalogSpec{Source: "manual"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceManual},
		Tags:        []string{"enterprise", "coding", "paid"},
		// Bedrock needs TWO credentials (access key id + secret access key)
		// plus a region. The primary secret is the access key id; the secret
		// access key is collected as a secondary secret and the region as a
		// non-secret field. Bedrock does not speak the OpenAI wire format —
		// agents call it through their own AWS SDK using these AWS_* env vars.
		Setup: []SetupParam{
			{Key: "secret_access_key", Label: "AWS Secret Access Key", EnvVar: "AWS_SECRET_ACCESS_KEY", Secret: true, Required: true, Help: "The secret access key paired with the access key id above."},
			{Key: "region", Label: "AWS Region", EnvVar: "AWS_REGION", Default: "us-east-1", Required: true, Help: "Bedrock region, e.g. us-east-1."},
		},
		Notes: "Bedrock authenticates via AWS SigV4 (the agent's AWS SDK), not an OpenAI key. AegisKeys injects AWS_ACCESS_KEY_ID (primary secret), AWS_SECRET_ACCESS_KEY (secondary secret), and AWS_REGION. Set the profile model to the Bedrock model ID (e.g. anthropic.claude-v2 or amazon.titan-text-express-v1).",
	},
	{
		ID: "fireworks", Name: "Fireworks", Slug: "fireworks",
		BaseURL: "https://api.fireworks.ai/inference/v1", EnvVar: "FIREWORKS_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "FIREWORKS_API_KEY"},
		Endpoints:   EndpointSpec{BaseURL: "https://api.fireworks.ai/inference/v1"},
		Catalog:     ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://api.fireworks.ai/inference/v1/models"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://api.fireworks.ai/inference/v1/models"},
		Tags:        []string{"fast", "coding", "paid", "free-tier"},
	},
	{
		ID: "deepseek", Name: "DeepSeek", Slug: "deepseek",
		BaseURL: "https://api.deepseek.com/v1", EnvVar: "DEEPSEEK_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "DEEPSEEK_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.deepseek.com/v1", APIPath: "/chat/completions"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "deepseek-chat", Name: "DeepSeek V3", ContextSize: 64000},
			{ID: "deepseek-reasoner", Name: "DeepSeek R1", ContextSize: 64000},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: false, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "reasoning", "paid"},
	},
	{
		ID: "moonshot", Name: "Moonshot (Kim)", Slug: "moonshot",
		BaseURL: "https://api.moonshot.cn/v1", EnvVar: "MOONSHOT_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "MOONSHOT_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.moonshot.cn/v1"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "moonshot-v1-8k", Name: "Moonshot V1 8K", ContextSize: 8192},
			{ID: "moonshot-v1-32k", Name: "Moonshot V1 32K", ContextSize: 32768},
			{ID: "moonshot-v1-128k", Name: "Moonshot V1 128K", ContextSize: 131072},
		},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceStatic},
		Tags:        []string{"long-context", "paid"},
	},
	{
		ID: "qwen", Name: "Alibaba Qwen", Slug: "qwen",
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", EnvVar: "DASHSCOPE_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "DASHSCOPE_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "qwen-max", Name: "Qwen Max", ContextSize: 32768},
			{ID: "qwen-plus", Name: "Qwen Plus", ContextSize: 131072},
			{ID: "qwen-turbo", Name: "Qwen Turbo", ContextSize: 131072},
			{ID: "qwen3-coder", Name: "Qwen3 Coder", ContextSize: 131072},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "reasoning", "paid", "free-tier"},
	},
	{
		ID: "nvidia-nim", Name: "Nvidia NIM", Slug: "nvidia-nim",
		BaseURL: "https://integrate.api.nvidia.com/v1", EnvVar: "NVIDIA_NIM_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "NVIDIA_NIM_API_KEY"},
		Endpoints:   EndpointSpec{BaseURL: "https://integrate.api.nvidia.com/v1"},
		Catalog:     ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://integrate.api.nvidia.com/v1/models"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://integrate.api.nvidia.com/v1/models"},
		Tags:        []string{"coding", "paid", "free-tier"},
	},
	{
		ID: "modelscope", Name: "ModelScope (DashScope)", Slug: "modelscope",
		BaseURL: "https://api-inference.modelscope.cn/v1", EnvVar: "MODELSCOPE_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:        AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "MODELSCOPE_API_KEY"},
		Endpoints:   EndpointSpec{BaseURL: "https://api-inference.modelscope.cn/v1"},
		Catalog:     ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://api-inference.modelscope.cn/v1/models"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://api-inference.modelscope.cn/v1/models"},
		Tags:        []string{"open-source", "multilingual", "paid", "free-tier"},
	},
	{
		ID: "vllm", Name: "vLLM / Custom", Slug: "vllm",
		BaseURL: "http://localhost:8000/v1", AuthHeader: "none",
		Compatibility: CompatLocal, Protocol: ProtocolLocal,
		Auth:        AuthSpec{Type: "none"},
		Endpoints:   EndpointSpec{BaseURL: "http://localhost:8000/v1"},
		Catalog:     ModelCatalogSpec{Source: "manual"},
		ModelPolicy: ModelCatalogPolicy{Source: ModelSourceManual},
		ExtraEnv:    map[string]string{"OPENAI_BASE_URL": "http://localhost:8000/v1"},
		Tags:        []string{"local", "manual"},
	},
	{
		ID: "zen", Name: "OpenCode Zen", Slug: "zen",
		BaseURL: "https://opencode.ai/zen/v1", EnvVar: "OPENCODE_ZEN_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "OPENCODE_ZEN_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://opencode.ai/zen/v1"},
		Catalog:   ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://opencode.ai/zen/v1/models"},
		Models: []ProviderModel{
			{ID: "opencode/gpt-5.5", Name: "GPT 5.5", ContextSize: 272000},
			{ID: "opencode/gpt-5.5-pro", Name: "GPT 5.5 Pro", ContextSize: 272000},
			{ID: "opencode/gpt-5.4", Name: "GPT 5.4", ContextSize: 272000},
			{ID: "opencode/gpt-5.4-mini", Name: "GPT 5.4 Mini", ContextSize: 272000},
			{ID: "opencode/claude-opus-4-8", Name: "Claude Opus 4.8", ContextSize: 200000},
			{ID: "opencode/claude-sonnet-4-5", Name: "Claude Sonnet 4.5", ContextSize: 200000},
			{ID: "opencode/claude-haiku-4-5", Name: "Claude Haiku 4.5", ContextSize: 200000},
			{ID: "opencode/deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextSize: 131072},
			{ID: "opencode/deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextSize: 131072},
			{ID: "opencode/qwen3.7-max", Name: "Qwen3.7 Max", ContextSize: 131072},
			{ID: "opencode/qwen3.7-plus", Name: "Qwen3.7 Plus", ContextSize: 131072},
			{ID: "opencode/kimi-k2.7-code", Name: "Kimi K2.7 Code", ContextSize: 131072},
			{ID: "opencode/minimax-m3", Name: "MiniMax M3", ContextSize: 131072},
			{ID: "opencode/glm-5.2", Name: "GLM 5.2", ContextSize: 128000},
			{ID: "opencode/grok-build-0.1", Name: "Grok Build 0.1", ContextSize: 131072},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://opencode.ai/zen/v1/models"},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"gateway", "coding", "paid", "curated"},
	},
	{
		ID: "opencode-go", Name: "OpenCode Go", Slug: "opencode-go",
		BaseURL: "https://opencode.ai/zen/v1", EnvVar: "OPENCODE_GO_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "OPENCODE_GO_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://opencode.ai/zen/v1"},
		Catalog:   ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://opencode.ai/zen/v1/models"},
		Models: []ProviderModel{
			{ID: "opencode/deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextSize: 131072},
			{ID: "opencode/deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextSize: 131072},
			{ID: "opencode/qwen3.7-max", Name: "Qwen3.7 Max", ContextSize: 131072},
			{ID: "opencode/qwen3.7-plus", Name: "Qwen3.7 Plus", ContextSize: 131072},
			{ID: "opencode/kimi-k2.7-code", Name: "Kimi K2.7 Code", ContextSize: 131072},
			{ID: "opencode/minimax-m3", Name: "MiniMax M3", ContextSize: 131072},
			{ID: "opencode/glm-5.2", Name: "GLM 5.2", ContextSize: 128000},
			{ID: "opencode/mimo-v2.5-pro", Name: "MiMo V2.5 Pro", ContextSize: 131072},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://opencode.ai/zen/v1/models"},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"gateway", "coding", "subscription"},
	},
	{
		ID: "longcat", Name: "LongCat", Slug: "longcat",
		BaseURL: "https://api.longcat.chat/openai", EnvVar: "LONGCAT_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "LONGCAT_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.longcat.chat/openai"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "LongCat-2.0", Name: "LongCat 2.0", ContextSize: 1000000},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "agent", "long-context"},
		Notes:        "OpenAI-format: https://api.longcat.chat/openai  Anthropic-format: https://api.longcat.chat/anthropic",
	},
	{
		ID: "anyrouter", Name: "AnyRouter", Slug: "anyrouter",
		BaseURL: "https://anyrouter.top", EnvVar: "ANTHROPIC_AUTH_TOKEN",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatAnthropic, Protocol: ProtocolAnthropic,
		Auth:         AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "ANTHROPIC_AUTH_TOKEN"},
		Endpoints:    EndpointSpec{BaseURL: "https://anyrouter.top"},
		Catalog:      ModelCatalogSpec{Source: "manual"},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceManual},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"router", "proxy", "anthropic-compat"},
		Notes:        "Proxy routed to Claude Code. Set ANTHROPIC_AUTH_TOKEN and ANTHROPIC_BASE_URL=https://anyrouter.top",
	},
	{
		ID: "azure-openai", Name: "Azure OpenAI", Slug: "azure-openai",
		BaseURL: "https://example.openai.azure.com", EnvVar: "AZURE_OPENAI_API_KEY",
		AuthHeader:    "api-key: ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:         AuthSpec{Type: "header", HeaderName: "api-key", EnvVar: "AZURE_OPENAI_API_KEY"},
		Endpoints:    EndpointSpec{BaseURL: "https://example.openai.azure.com", URLTemplate: "https://{resource}.openai.azure.com"},
		Catalog:      ModelCatalogSpec{Source: "manual"},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceManual},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"enterprise", "coding", "paid"},
		// Azure OpenAI needs the resource name (endpoint), the deployment name
		// (the model identity, supplied as the profile model), and the API
		// version. The primary secret is the api-key.
		Setup: []SetupParam{
			{Key: "resource", Label: "Resource name", EnvVar: "AZURE_OPENAI_ENDPOINT", Endpoint: true, Required: true, Example: "my-resource", Help: "Your Azure OpenAI resource name — the {resource} in https://{resource}.openai.azure.com."},
			{Key: "deployment", Label: "Deployment name", Required: true, Help: "Azure deployments are created per-model and are NOT the model ID. Set the profile model to this deployment name for OpenAI-SDK agents."},
			{Key: "api_version", Label: "API version", EnvVar: "AZURE_OPENAI_API_VERSION", Default: "2024-06-01", Required: true, Help: "Azure API version, e.g. 2024-06-01."},
		},
		Notes: "Azure OpenAI authenticates with an api-key header. The model identity is the deployment name (set it as the profile model). Provide resource, deployment, and api-version at key setup; AegisKeys injects AZURE_OPENAI_ENDPOINT and AZURE_OPENAI_API_VERSION and uses the deployment as the model.",
	},
	{
		ID: "alibaba-cloud", Name: "Alibaba Cloud", Slug: "alibaba-cloud",
		BaseURL: "https://dashscope.aliyuncs.com/api/v1", EnvVar: "DASHSCOPE_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "DASHSCOPE_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://dashscope.aliyuncs.com/api/v1"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "qwen-max", Name: "Qwen Max", ContextSize: 32768},
			{ID: "qwen-plus", Name: "Qwen Plus", ContextSize: 131072},
			{ID: "qwen-turbo", Name: "Qwen Turbo", ContextSize: 131072},
			{ID: "qwen3-coder", Name: "Qwen3 Coder", ContextSize: 131072},
			{ID: "qwq-32b", Name: "QwQ 32B", ContextSize: 131072},
			{ID: "qwen3-235b-a22b", Name: "Qwen3 235B", ContextSize: 131072},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "paid", "free-tier", "multilingual"},
	},
	{
		ID: "commandcode", Name: "Command Code", Slug: "commandcode",
		BaseURL: "https://api.commandcode.ai/provider/v1", EnvVar: "COMMANDCODE_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:         AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "COMMANDCODE_API_KEY"},
		Endpoints:    EndpointSpec{BaseURL: "https://api.commandcode.ai/provider/v1"},
		Catalog:      ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://api.commandcode.ai/provider/v1/models"},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://api.commandcode.ai/provider/v1/models"},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"gateway", "coding", "paid", "curated"},
		Notes:        "OpenAI: /provider/v1/chat/completions  Anthropic: /provider/v1/messages  Model IDs use vendor/prefix form (e.g. deepseek/deepseek-v4-flash). Also accepts CMD_API_KEY.",
	},
	{
		ID: "cline", Name: "Cline", Slug: "cline",
		BaseURL: "https://api.cline.bot/api/v1", EnvVar: "CLINE_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "CLINE_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.cline.bot/api/v1", APIPath: "/chat/completions"},
		Catalog:   ModelCatalogSpec{Source: "dynamic", RefreshURL: "https://api.cline.bot/api/v1/models"},
		Models: []ProviderModel{
			{ID: "cline-pass/glm-5.2", Name: "GLM 5.2 (ClinePass)", ContextSize: 128000},
			{ID: "cline-pass/kimi-k2.7-code", Name: "Kimi K2.7 Code (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/kimi-k2.6", Name: "Kimi K2.6 (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/deepseek-v4-pro", Name: "DeepSeek V4 Pro (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/deepseek-v4-flash", Name: "DeepSeek V4 Flash (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/mimo-v2.5", Name: "MiMo V2.5 (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/mimo-v2.5-pro", Name: "MiMo V2.5 Pro (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/minimax-m3", Name: "MiniMax M3 (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/qwen3.7-max", Name: "Qwen3.7 Max (ClinePass)", ContextSize: 131072},
			{ID: "cline-pass/qwen3.7-plus", Name: "Qwen3.7 Plus (ClinePass)", ContextSize: 131072},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceDynamic, RefreshURL: "https://api.cline.bot/api/v1/models"},
		Capabilities: Capabilities{ToolUse: true, Vision: true, Streaming: true, FunctionCalling: true},
		Tags:         []string{"gateway", "coding", "paid", "cline-pass"},
		Notes:        "Single provider for both Cline usage-billing and ClinePass. Same CLINE_API_KEY and endpoint. ClinePass models (marked) use cline-pass/ prefix and require $9.99/month sub for 2-5x rate limits. Free/credit models use bare names and dynamic catalog.",
	},
	{
		ID: "tencent", Name: "Tencent Cloud", Slug: "tencent",
		BaseURL: "https://api.hunyuan.cloud.tencent.com/v1", EnvVar: "HUNYUAN_API_KEY",
		AuthHeader:    "Authorization: Bearer ${KEY}",
		Compatibility: CompatOpenAI, Protocol: ProtocolOpenAI,
		Auth:      AuthSpec{Type: "bearer", HeaderName: "Authorization", Prefix: "Bearer ", EnvVar: "HUNYUAN_API_KEY"},
		Endpoints: EndpointSpec{BaseURL: "https://api.hunyuan.cloud.tencent.com/v1"},
		Catalog:   ModelCatalogSpec{Source: "static"},
		Models: []ProviderModel{
			{ID: "hunyuan-t1", Name: "Hunyuan T1", ContextSize: 32768},
			{ID: "hunyuan-turbos", Name: "Hunyuan TurboS", ContextSize: 32768},
		},
		ModelPolicy:  ModelCatalogPolicy{Source: ModelSourceStatic},
		Capabilities: Capabilities{ToolUse: true, Vision: false, Streaming: true, FunctionCalling: true},
		Tags:         []string{"coding", "paid"},
	},
}

// DefaultProviders returns the curated list of known providers.
func DefaultProviders() []Provider {
	return defaultProviders
}

package provider

import (
	"testing"
)

// TestNormalize_DetectsOpenAIFromURL verifies that a provider created with only
// flat fields (name + slug + env var + base URL) gets auto-detected as
// OpenAI-compatible. This is the core fix for the broken profile wizard flow:
// a TUI-created provider must not remain adapter-invisible.
func TestNormalize_DetectsOpenAIFromURL(t *testing.T) {
	p := Provider{
		Name: "beepboop", Slug: "beepboop",
		EnvVar: "BEEPBOOP_API_KEY", BaseURL: "https://api.beepboop.com/v1",
	}
	p.Normalize()

	if p.Compatibility != CompatOpenAI {
		t.Errorf("Compatibility = %q, want %q", p.Compatibility, CompatOpenAI)
	}
	if p.Protocol != ProtocolOpenAI {
		t.Errorf("Protocol = %q, want %q", p.Protocol, ProtocolOpenAI)
	}
	if p.Auth.Type != "bearer" {
		t.Errorf("Auth.Type = %q, want bearer", p.Auth.Type)
	}
	if p.Auth.EnvVar != "BEEPBOOP_API_KEY" {
		t.Errorf("Auth.EnvVar = %q, want BEEPBOOP_API_KEY", p.Auth.EnvVar)
	}
	if p.Endpoints.BaseURL != "https://api.beepboop.com/v1" {
		t.Errorf("Endpoints.BaseURL = %q, want https://api.beepboop.com/v1", p.Endpoints.BaseURL)
	}
	if p.Catalog.Source != "manual" {
		t.Errorf("Catalog.Source = %q, want manual", p.Catalog.Source)
	}
	if p.ModelPolicy.Source != ModelSourceManual {
		t.Errorf("ModelPolicy.Source = %q, want %q", p.ModelPolicy.Source, ModelSourceManual)
	}
	// A normalized custom provider must pass strict validation now.
	if err := p.ValidateStrict(); err != nil {
		t.Errorf("ValidateStrict failed after Normalize: %v", err)
	}
}

// TestNormalize_DetectsAnthropicFromURL verifies anthropic URL detection.
func TestNormalize_DetectsAnthropicFromURL(t *testing.T) {
	p := Provider{
		Name: "Anthropic", Slug: "anthropic",
		EnvVar: "ANTHROPIC_API_KEY", BaseURL: "https://api.anthropic.com",
	}
	p.Normalize()

	if p.Compatibility != CompatAnthropic {
		t.Errorf("Compatibility = %q, want anthropic", p.Compatibility)
	}
	if p.Auth.Type != "header" {
		t.Errorf("Auth.Type = %q, want header", p.Auth.Type)
	}
	if p.Auth.HeaderName != "x-api-key" {
		t.Errorf("Auth.HeaderName = %q, want x-api-key", p.Auth.HeaderName)
	}
}

// TestNormalize_DetectsGoogleFromURL verifies google URL detection.
func TestNormalize_DetectsGoogleFromURL(t *testing.T) {
	p := Provider{
		Name: "Gemini", Slug: "gemini",
		EnvVar: "GEMINI_API_KEY", BaseURL: "https://generativelanguage.googleapis.com",
	}
	p.Normalize()

	if p.Compatibility != CompatGoogle {
		t.Errorf("Compatibility = %q, want google", p.Compatibility)
	}
	if p.Auth.Type != "query" {
		t.Errorf("Auth.Type = %q, want query", p.Auth.Type)
	}
}

// TestNormalize_DetectsLocalFromURL verifies localhost URL detection.
func TestNormalize_DetectsLocalFromURL(t *testing.T) {
	p := Provider{
		Name: "Ollama", Slug: "ollama",
		BaseURL: "http://localhost:11434/v1",
	}
	p.Normalize()

	if p.Compatibility != CompatLocal {
		t.Errorf("Compatibility = %q, want local", p.Compatibility)
	}
	if p.Protocol != ProtocolLocal {
		t.Errorf("Protocol = %q, want local", p.Protocol)
	}
	if p.Auth.Type != "none" {
		t.Errorf("Auth.Type = %q, want none", p.Auth.Type)
	}
}

// TestNormalize_PreservesUserValues verifies Normalize only fills EMPTY fields.
func TestNormalize_PreservesUserValues(t *testing.T) {
	p := Provider{
		Name: "Custom", Slug: "custom",
		EnvVar: "MY_KEY", BaseURL: "https://api.example.com/v1",
		Compatibility: CompatAnthropic, Protocol: ProtocolAnthropic,
		Auth: AuthSpec{Type: "header", HeaderName: "x-api-key", EnvVar: "MY_KEY"},
	}
	p.Normalize()

	// Must not overwrite explicitly-set values.
	if p.Compatibility != CompatAnthropic {
		t.Errorf("Compatibility overwritten: %q", p.Compatibility)
	}
	if p.Auth.Type != "header" {
		t.Errorf("Auth.Type overwritten: %q", p.Auth.Type)
	}
	if p.Auth.HeaderName != "x-api-key" {
		t.Errorf("Auth.HeaderName overwritten: %q", p.Auth.HeaderName)
	}
}

// TestNormalize_EmptySlugFromID verifies slug derivation when only id is meant.
func TestNormalize_EmptySlugFromID(t *testing.T) {
	p := Provider{Name: "X", ID: "x-id", EnvVar: "KEY", BaseURL: "https://api.example.com/v1"}
	p.Normalize()
	if p.Slug != "x-id" {
		t.Errorf("Slug = %q, want x-id", p.Slug)
	}
}

// TestMergeDefaults_AppendsMissing verifies missing default providers are added.
func TestMergeDefaults_AppendsMissing(t *testing.T) {
	r := &Registry{Providers: []Provider{
		// A user's custom provider.
		{Name: "beepboop", Slug: "beepboop", EnvVar: "KEY", BaseURL: "https://api.beepboop.com/v1"},
	}}

	changed := r.MergeDefaults(DefaultProviders())
	if !changed {
		t.Error("expected MergeDefaults to report changed=true")
	}
	if len(r.Providers) != 1+len(DefaultProviders()) {
		t.Errorf("got %d providers, want %d", len(r.Providers), 1+len(DefaultProviders()))
	}
}

// TestMergeDefaults_BackfillsStructuralFields verifies existing providers get
// backfilled compatibility/protocol/etc without overwriting user values.
func TestMergeDefaults_BackfillsStructuralFields(t *testing.T) {
	r := &Registry{Providers: []Provider{
		// A partial anthropic provider missing compat/protocol/auth.
		{Name: "My Anthropic", Slug: "anthropic", EnvVar: "MY_ANTHROPIC_KEY",
			BaseURL: "https://api.anthropic.com"},
	}}

	changed := r.MergeDefaults(DefaultProviders())
	if !changed {
		t.Error("expected changed=true for backfill")
	}
	p := r.Find("anthropic")
	if p == nil {
		t.Fatal("anthropic not found")
	}
	// Name must be preserved (user-editable field).
	if p.Name != "My Anthropic" {
		t.Errorf("Name = %q, want My Anthropic (user value overwritten)", p.Name)
	}
	// Compatibility must be backfilled from the default.
	if p.Compatibility != CompatAnthropic {
		t.Errorf("Compatibility = %q, want anthropic (not backfilled)", p.Compatibility)
	}
	if p.Protocol != ProtocolAnthropic {
		t.Errorf("Protocol = %q, want anthropic (not backfilled)", p.Protocol)
	}
}

// TestMergeDefaults_Idempotent verifies re-running is a no-op.
func TestMergeDefaults_Idempotent(t *testing.T) {
	r := &Registry{Providers: []Provider{}}
	r.MergeDefaults(DefaultProviders())
	firstLen := len(r.Providers)

	changed := r.MergeDefaults(DefaultProviders())
	if changed {
		t.Error("expected changed=false on second run")
	}
	if len(r.Providers) != firstLen {
		t.Errorf("provider count changed on second run: %d vs %d", len(r.Providers), firstLen)
	}
}

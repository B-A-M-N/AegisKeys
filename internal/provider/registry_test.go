package provider

import "testing"

func TestValidEnvVar(t *testing.T) {
	cases := map[string]bool{
		"":               true,
		"OPENAI_API_KEY": true,
		"X":              true,
		"ABC123":         true,
		"openai_api_key": false,
		"1API":           false,
		"API-KEY":        false,
		"API KEY":        false,
	}
	for in, want := range cases {
		if got := ValidEnvVar(in); got != want {
			t.Errorf("ValidEnvVar(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidBaseURL(t *testing.T) {
	cases := map[string]bool{
		"":                          true,
		"https://api.openai.com/v1": true,
		"http://localhost:11434/v1": true,
		"ftp://example.com":         false,
		"not a url":                 false,
	}
	for in, want := range cases {
		if got := ValidBaseURL(in); got != want {
			t.Errorf("ValidBaseURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLooksLikeSecret(t *testing.T) {
	if !LooksLikeSecret("sk-abcdef1234567890abcdef1234567890") {
		t.Error("expected sk-... to look like a secret")
	}
	if !LooksLikeSecret("ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcde") {
		t.Error("expected ghp_... to look like a secret")
	}
	if LooksLikeSecret("OpenAI") {
		t.Error("expected 'OpenAI' to NOT look like a secret")
	}
}

func TestRegistryUniqueness(t *testing.T) {
	r := NewRegistry()
	p := Provider{Slug: "custom", Name: "Custom", Auth: AuthSpec{Type: "none"}}
	if err := r.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(Provider{Slug: "custom", Name: "Duplicate", Auth: AuthSpec{Type: "none"}}); err == nil {
		t.Error("expected duplicate slug to error")
	}
}

func TestRegistryAdd_RejectsInvalid(t *testing.T) {
	r := NewRegistry()
	// Provider with auth but no env var should be rejected.
	err := r.Add(Provider{Slug: "bad", Name: "Bad", Auth: AuthSpec{Type: "bearer"}})
	if err == nil {
		t.Error("expected ValidateStrict to reject provider with auth but no env var")
	}
}

func TestRegistryUpdate(t *testing.T) {
	r := NewRegistry()
	_ = r.Add(Provider{Slug: "p1", Name: "P1", Auth: AuthSpec{Type: "none"}})
	if err := r.Update("p1", Provider{Slug: "p1", Name: "P1 Updated", Auth: AuthSpec{Type: "none"}}); err != nil {
		t.Errorf("Update: %v", err)
	}
	if r.Find("p1").Name != "P1 Updated" {
		t.Error("Update did not change name")
	}
	// Updating a missing provider should error.
	if err := r.Update("missing", Provider{Slug: "missing", Name: "M", Auth: AuthSpec{Type: "none"}}); err == nil {
		t.Error("expected error updating missing provider")
	}
}

func TestRegistryFindRemove(t *testing.T) {
	r := NewRegistry()
	for _, p := range DefaultProviders() {
		_ = r.Add(p)
	}
	if r.Find("openai") == nil {
		t.Error("expected to find seeded openai")
	}
	if r.Find("nonexistent") != nil {
		t.Error("expected nil for missing provider")
	}
	if err := r.Remove("openai"); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if r.Find("openai") != nil {
		t.Error("expected openai to be removed")
	}
	if err := r.Remove("ghost"); err == nil {
		t.Error("expected error removing missing provider")
	}
}

func TestDefaultProviderCountAndRequiredSlugs(t *testing.T) {
	providers := DefaultProviders()
	if len(providers) < 19 {
		t.Errorf("expected at least 19 providers, got %d", len(providers))
	}
	requiredSlugs := []string{
		"openai", "anthropic", "openrouter", "mistral", "gemini", "groq",
		"ollama", "lmstudio", "huggingface", "cerebras", "together",
		"fireworks", "deepseek", "moonshot", "qwen", "nvidia-nim",
		"modelscope", "vllm", "bedrock",
	}
	slugMap := map[string]bool{}
	for _, p := range providers {
		slugMap[p.Slug] = true
	}
	for _, slug := range requiredSlugs {
		if !slugMap[slug] {
			t.Errorf("missing required provider slug: %s", slug)
		}
	}
}

func TestDefaultProvidersPassValidateStrict(t *testing.T) {
	for _, p := range DefaultProviders() {
		if err := p.ValidateStrict(); err != nil {
			t.Errorf("provider %q failed strict validation: %v", p.Slug, err)
		}
	}
}

func TestNormalizeNewProviders(t *testing.T) {
	cases := []struct {
		slug         string
		wantCompat   CompatibilityMode
		wantProtocol Protocol
		wantAuthType string
		wantEnvVar   string
	}{
		{"zen", CompatOpenAI, ProtocolOpenAI, "bearer", "OPENCODE_ZEN_API_KEY"},
		{"opencode-go", CompatOpenAI, ProtocolOpenAI, "bearer", "OPENCODE_GO_API_KEY"},
		{"longcat", CompatOpenAI, ProtocolOpenAI, "bearer", "LONGCAT_API_KEY"},
		{"anyrouter", CompatAnthropic, ProtocolAnthropic, "bearer", "ANTHROPIC_AUTH_TOKEN"},
		{"azure-openai", CompatOpenAI, ProtocolOpenAI, "header", "AZURE_OPENAI_API_KEY"},
		{"alibaba-cloud", CompatOpenAI, ProtocolOpenAI, "bearer", "DASHSCOPE_API_KEY"},
		{"tencent", CompatOpenAI, ProtocolOpenAI, "bearer", "HUNYUAN_API_KEY"},
		{"commandcode", CompatOpenAI, ProtocolOpenAI, "bearer", "COMMANDCODE_API_KEY"},
		{"cline", CompatOpenAI, ProtocolOpenAI, "bearer", "CLINE_API_KEY"},
		{"cline-pass", CompatOpenAI, ProtocolOpenAI, "bearer", "CLINE_API_KEY"},
	}
	slugMap := map[string]Provider{}
	for _, p := range DefaultProviders() {
		slugMap[p.Slug] = p
	}
	for _, tc := range cases {
		p, ok := slugMap[tc.slug]
		if !ok {
			t.Errorf("provider %q not found in defaults", tc.slug)
			continue
		}
		p.Normalize()
		if p.Compatibility != tc.wantCompat {
			t.Errorf("%s: compatibility = %q, want %q", tc.slug, p.Compatibility, tc.wantCompat)
		}
		if p.Protocol != tc.wantProtocol {
			t.Errorf("%s: protocol = %q, want %q", tc.slug, p.Protocol, tc.wantProtocol)
		}
		if p.Auth.Type != tc.wantAuthType {
			t.Errorf("%s: auth type = %q, want %q", tc.slug, p.Auth.Type, tc.wantAuthType)
		}
		if p.CanonicalEnvVar() != tc.wantEnvVar {
			t.Errorf("%s: env var = %q, want %q", tc.slug, p.CanonicalEnvVar(), tc.wantEnvVar)
		}
	}
}

func TestCredentialCompatible_SharedServiceCredentials(t *testing.T) {
	cases := []struct {
		provider string
		key      string
		want     bool
	}{
		{"zen", "opencode-go", true},
		{"opencode-go", "zen", true},
		{"cline", "cline-pass", true},
		{"cline-pass", "cline", true},
		{"openai", "cline", false},
	}
	for _, tc := range cases {
		if got := CredentialCompatible(tc.provider, tc.key); got != tc.want {
			t.Errorf("CredentialCompatible(%q, %q) = %t, want %t", tc.provider, tc.key, got, tc.want)
		}
	}
}

func TestOpenCodeGoUsesItsDedicatedEndpoint(t *testing.T) {
	var goProvider *Provider
	for i := range DefaultProviders() {
		if DefaultProviders()[i].Slug == "opencode-go" {
			goProvider = &DefaultProviders()[i]
			break
		}
	}
	if goProvider == nil {
		t.Fatal("opencode-go provider missing")
	}
	if got, want := goProvider.CanonicalBaseURL(), "https://opencode.ai/zen/go/v1"; got != want {
		t.Fatalf("Go base URL = %q, want %q", got, want)
	}
	if got, want := goProvider.ModelRefreshURL(), "https://opencode.ai/zen/go/v1/models"; got != want {
		t.Fatalf("Go models URL = %q, want %q", got, want)
	}
}

func TestAzureOpenAIHeaderAuth(t *testing.T) {
	var p *Provider
	for i := range defaultProviders {
		if defaultProviders[i].Slug == "azure-openai" {
			p = &defaultProviders[i]
			break
		}
	}
	if p == nil {
		t.Fatal("azure-openai provider not found")
	}
	p.Normalize()
	if p.Auth.Type != "header" {
		t.Errorf("azure auth type = %q, want header", p.Auth.Type)
	}
	if p.Auth.HeaderName != "api-key" {
		t.Errorf("azure header name = %q, want api-key", p.Auth.HeaderName)
	}
	if p.AuthHeader != "api-key: ${KEY}" {
		t.Errorf("azure auth header = %q, want api-key: ${KEY}", p.AuthHeader)
	}
	// Azure must not use a bearer prefix.
	if p.Auth.Prefix != "" {
		t.Errorf("azure auth prefix = %q, want empty (uses api-key header, not bearer)", p.Auth.Prefix)
	}
}

func TestAnyRouterAnthropicCompat(t *testing.T) {
	var p *Provider
	for i := range defaultProviders {
		if defaultProviders[i].Slug == "anyrouter" {
			p = &defaultProviders[i]
			break
		}
	}
	if p == nil {
		t.Fatal("anyrouter provider not found")
	}
	p.Normalize()
	if p.Compatibility != CompatAnthropic {
		t.Errorf("anyrouter compatibility = %q, want anthropic", p.Compatibility)
	}
	if p.EnvVar != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("anyrouter env var = %q, want ANTHROPIC_AUTH_TOKEN", p.EnvVar)
	}
}

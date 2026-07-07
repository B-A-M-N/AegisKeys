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

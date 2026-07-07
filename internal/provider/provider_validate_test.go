package provider

import (
	"testing"

	"aegiskeys/internal/sensitive"
)

func TestValidateStrict_ValidProvider(t *testing.T) {
	p := Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY",
		BaseURL: "https://api.openai.com/v1", Compatibility: CompatOpenAI,
		Auth: AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
	}
	if err := p.ValidateStrict(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateStrict_InvalidSlug(t *testing.T) {
	p := Provider{Name: "Bad Slug!", Slug: "bad slug!", EnvVar: "KEY"}
	if err := p.ValidateStrict(); err == nil {
		t.Error("expected error for invalid slug")
	}
}

func TestValidateStrict_AuthWithoutEnvVar(t *testing.T) {
	p := Provider{Name: "Bad", Slug: "bad", Auth: AuthSpec{Type: "bearer"}}
	if err := p.ValidateStrict(); err == nil {
		t.Error("expected error for auth provider without env var")
	}
}

func TestValidateStrict_SecretInBaseURL(t *testing.T) {
	p := Provider{
		Name: "Bad", Slug: "bad", EnvVar: "KEY",
		BaseURL: "https://api.example.com/v1?key=sk-1234567890abcdef1234567890abcdef",
	}
	if err := p.ValidateStrict(); err == nil {
		t.Error("expected error for secret in base URL")
	}
}

func TestValidateStrict_SecretInExtraEnv(t *testing.T) {
	p := Provider{
		Name: "Bad", Slug: "bad", EnvVar: "KEY",
		ExtraEnv: map[string]string{"OPENAI_API_KEY": "sk-1234567890abcdef1234567890abcdef"},
	}
	if err := p.ValidateStrict(); err == nil {
		t.Error("expected error for secret in ExtraEnv")
	}
}

func TestValidateStrict_ShortValuesSafe(t *testing.T) {
	p := Provider{
		Name: "OK", Slug: "ok", EnvVar: "KEY",
		Auth:       AuthSpec{Type: "bearer", EnvVar: "KEY"},
		BaseURL:    "https://api.example.com/v1",
		AuthHeader: "Authorization: Bearer ${KEY}",
	}
	if err := p.ValidateStrict(); err != nil {
		t.Errorf("short URL/header should be safe: %v", err)
	}
}

func TestValidateStrict_AWSAuthType(t *testing.T) {
	p := Provider{
		Name: "AWS", Slug: "aws", EnvVar: "AWS_ACCESS_KEY_ID",
		BaseURL: "https://bedrock.us-east-1.amazonaws.com",
		Auth:    AuthSpec{Type: "aws", EnvVar: "AWS_ACCESS_KEY_ID"},
	}
	if err := p.ValidateStrict(); err != nil {
		t.Errorf("aws auth type should be accepted: %v", err)
	}
}

func TestValidSlug(t *testing.T) {
	cases := map[string]bool{
		"openai":      true,
		"open-router": true,
		"my_provider": true,
		"a":           true,
		"bad slug!":   false,
		"":            false,
		"UPPER":       false,
		"spaces here": false,
		"special!@#":  false,
	}
	for slug, want := range cases {
		got := validSlug(slug)
		if got != want {
			t.Errorf("validSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

func TestLooksLikeSecret_Delegates(t *testing.T) {
	cases := map[string]bool{
		"sk-1234567890abcdef1234567890abcdef":  true,
		"https://api.openai.com/v1":            false,
		"short":                                false,
		"Authorization: Bearer ${KEY}":         false,
		"ghp_1234567890abcdef1234567890abcdef": true,
	}
	for val, want := range cases {
		got := LooksLikeSecret(val)
		if got != want {
			t.Errorf("LooksLikeSecret(%q) = %v, want %v", val, got, want)
		}
	}
}

func TestSensitiveIsSecretName(t *testing.T) {
	cases := map[string]bool{
		"OPENAI_API_KEY":  true,
		"GITHUB_TOKEN":    true,
		"MY_SECRET":       true,
		"DB_PASSWORD":     true,
		"AUTH_TOKEN":      true,
		"MODEL":           false,
		"BASE_URL":        false,
		"OPENAI_BASE_URL": false,
		"TIMEOUT":         false,
		"OPENAI_MODEL":    false,
		"MODEL_ID":        false,
		"MY_CUSTOM_VAR":   false,
	}
	for name, want := range cases {
		got := sensitive.IsSecretName(name)
		if got != want {
			t.Errorf("IsSecretName(%q) = %v, want %v", name, got, want)
		}
	}
}

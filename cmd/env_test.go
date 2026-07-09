package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteEnvJSON_Masked(t *testing.T) {
	env := map[string]string{
		"OPENROUTER_API_KEY": "sk-orbitor-1234567890abcdef",
		"ANTHROPIC_BASE_URL": "https://openrouter.ai/api/v1",
	}

	var buf bytes.Buffer
	err := writeEnvJSON(&buf, env, "OPENROUTER_API_KEY", "OpenRouter", "test-prof", false)
	if err != nil {
		t.Fatalf("writeEnvJSON: %v", err)
	}

	var out struct {
		Profile  string            `json:"profile"`
		Provider string            `json:"provider"`
		Env      map[string]string `json:"env"`
		Full     bool              `json:"full"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}

	if out.Profile != "test-prof" {
		t.Errorf("profile = %q, want test-prof", out.Profile)
	}
	if out.Provider != "OpenRouter" {
		t.Errorf("provider = %q, want OpenRouter", out.Provider)
	}
	if out.Full {
		t.Error("full should be false for masked output")
	}
	// Secret should be masked.
	if out.Env["OPENROUTER_API_KEY"] == "sk-orbitor-1234567890abcdef" {
		t.Error("secret was not masked")
	}
	masked := out.Env["OPENROUTER_API_KEY"]
	// MaskSecret shows "...XXXX" (last 4 chars) or "<hidden>" for short secrets.
	if masked != "...cdef" && masked != "<hidden>" {
		t.Errorf("masked value should be ...cdef or <hidden>, got %q", masked)
	}
	// Non-secret should be plaintext.
	if out.Env["ANTHROPIC_BASE_URL"] != "https://openrouter.ai/api/v1" {
		t.Errorf("non-secret should be plaintext, got %q", out.Env["ANTHROPIC_BASE_URL"])
	}
}

func TestWriteEnvJSON_Full(t *testing.T) {
	env := map[string]string{
		"OPENROUTER_API_KEY": "sk-orbitor-1234567890abcdef",
	}

	var buf bytes.Buffer
	err := writeEnvJSON(&buf, env, "OPENROUTER_API_KEY", "OpenRouter", "test-prof", true)
	if err != nil {
		t.Fatalf("writeEnvJSON: %v", err)
	}

	var out struct {
		Env  map[string]string `json:"env"`
		Full bool              `json:"full"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Full {
		t.Error("full should be true")
	}
	if out.Env["OPENROUTER_API_KEY"] != "sk-orbitor-1234567890abcdef" {
		t.Errorf("full secret should be exposed, got %q", out.Env["OPENROUTER_API_KEY"])
	}
}

func TestWriteEnvJSON_SortedKeys(t *testing.T) {
	// Verify that JSON keys are sorted for deterministic output.
	env := map[string]string{
		"ZETA_VAR":  "3",
		"ALPHA_VAR": "1",
		"MU_VAR":    "2",
	}

	var buf bytes.Buffer
	err := writeEnvJSON(&buf, env, "", "Prov", "Prof", true)
	if err != nil {
		t.Fatalf("writeEnvJSON: %v", err)
	}

	// Raw bytes should have ALPHA before MU before ZETA.
	idx := buf.String()
	if !strings.Contains(idx, "ALPHA_VAR") || !strings.Contains(idx, "MU_VAR") || !strings.Contains(idx, "ZETA_VAR") {
		t.Errorf("unexpected output: %s", idx)
	}
	iAlpha := strings.Index(idx, "ALPHA_VAR")
	iMu := strings.Index(idx, "MU_VAR")
	iZeta := strings.Index(idx, "ZETA_VAR")
	if !(iAlpha < iMu && iMu < iZeta) {
		t.Errorf("keys not sorted: alpha=%d mu=%d zeta=%d", iAlpha, iMu, iZeta)
	}
}

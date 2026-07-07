package redact

import "testing"

func TestRedactor_RedactString(t *testing.T) {
	r := NewRedactor([]string{"sk-my-exact-secret-value-12345"})

	cases := []struct{ in, want string }{
		{"key is sk-my-exact-secret-value-12345 here", "key is <redacted> here"},
		{"Authorization: Bearer sk-abcdef1234567890abcdefgh", "Authorization: Bearer <redacted>"},
		{"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcd1234efgh", "<redacted>"},
		{"normal text without secrets", "normal text without secrets"},
		{"api_key=supersecretvalue1234567890", "<redacted>"},
		{"KEY=short", "KEY=short"}, // short value (5 chars) not redacted by pattern
	}
	for _, c := range cases {
		if got := r.RedactString(c.in); got != c.want {
			t.Errorf("RedactString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactor_ContainsSecret(t *testing.T) {
	r := NewRedactor([]string{"sk-exact-value-12345"})
	if !r.ContainsSecret("using sk-exact-value-12345 value") {
		t.Error("expected to detect exact value")
	}
	if !r.ContainsSecret("key sk-abcdefghijklmnopqrstuvwxyz123456") {
		t.Error("expected to detect pattern match")
	}
	if r.ContainsSecret("hello world") {
		t.Error("should not flag normal text")
	}
}

func TestRedactor_RedactMap(t *testing.T) {
	r := NewRedactor([]string{"sk-secret-12345"})
	m := map[string]string{
		"OPENAI_API_KEY": "sk-secret-12345",
		"URL":            "https://api.openai.com",
	}
	out := r.RedactMap(m)
	if out["OPENAI_API_KEY"] != "<redacted>" {
		t.Errorf("key not redacted: %q", out["OPENAI_API_KEY"])
	}
	if out["URL"] != "https://api.openai.com" {
		t.Errorf("url should be untouched: %q", out["URL"])
	}
}

func TestRedactor_EmptySecrets(t *testing.T) {
	// Pattern-only redaction still works with no exact values.
	r := NewRedactor(nil)
	s := r.RedactString("token=sk-abcdefghijklmnopqrstuvwxyz1234567890")
	if s == "token=sk-abcdefghijklmnopqrstuvwxyz1234567890" {
		t.Error("pattern redaction should still work with no exact values")
	}
}

func TestNewRedactorFromRecords(t *testing.T) {
	r := NewRedactorFromRecords([]MaskedSecret{
		{Secret: "sk-record-secret-12345"},
		{Secret: "ab"}, // too short, skipped
	})
	if !r.ContainsSecret("contains sk-record-secret-12345 value") {
		t.Error("should detect secret from records")
	}
}

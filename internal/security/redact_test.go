package security

import "testing"

func TestRedact(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Authorization: Bearer sk-abcdef1234567890abcdefgh", "Authorization: Bearer <redacted>"},
		{"OPENAI_API_KEY=sk-abcdef1234567890abcdefghij", "OPENAI_<redacted>"},
		{"api_key=supersecretvalue1234567890", "<redacted>"},
		{"token=abcdefghijklmnop", "<redacted>"},
		{"password=secretpassword123456789", "<redacted>"},
		{"normal text without secrets", "normal text without secrets"},
		{"KEY=short", "KEY=short"}, // short value (5 chars) not redacted
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactWithSecrets(t *testing.T) {
	secrets := []string{"sk-my-exact-secret-12345"}
	if got := RedactWithSecrets("key: sk-my-exact-secret-12345", secrets); got != "key: <redacted>" {
		t.Errorf("RedactWithSecrets = %q, want redaction", got)
	}
}

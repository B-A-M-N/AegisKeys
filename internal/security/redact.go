package security

import (
	"aegiskeys/internal/redact"
)

// Redact delegates to the central redactor. Kept for backward compatibility.
// Prefer building a redact.Redactor once and reusing it.
func Redact(input string) string {
	r := redact.NewRedactor(nil)
	return r.RedactString(input)
}

// RedactWithSecrets builds a Redactor seeded with exact vault values and
// applies pattern + exact-value redaction.
func RedactWithSecrets(input string, secrets []string) string {
	r := redact.NewRedactor(secrets)
	return r.RedactString(input)
}

// Redactor exposes the central redactor for repeated use.
func NewRedactor(secrets []string) *redact.Redactor {
	return redact.NewRedactor(secrets)
}

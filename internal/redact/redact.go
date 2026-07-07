// Package redact provides centralized secret detection and redaction.
// All output that might contain secrets (audit logs, env display, launch
// previews, doctor scans, error rendering, TUI detail modals) should flow
// through a Redactor so secrets never leak to the terminal or logs.
package redact

import (
	"regexp"
	"strings"
)

// Common API-key patterns. These are intentionally broad — false positives
// over-redact, which is safe; false negatives leak secrets.
var keyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),                                      // OpenAI / OpenRouter sk- keys
	regexp.MustCompile(`sk-or-v1-[a-f0-9]{48,}`),                                   // OpenRouter v1 keys
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{32,}`),                                     // GitHub personal tokens
	regexp.MustCompile(`gho_[a-zA-Z0-9]{32,}`),                                     // GitHub OAuth tokens
	regexp.MustCompile(`ghs_[a-zA-Z0-9]{32,}`),                                     // GitHub server-to-server
	regexp.MustCompile(`hf_[a-zA-Z0-9]{32,}`),                                      // HuggingFace tokens
	regexp.MustCompile(`xoxb-[a-zA-Z0-9-]{30,}`),                                   // Slack bot tokens
	regexp.MustCompile(`xoxp-[a-zA-Z0-9-]{30,}`),                                   // Slack user tokens
	regexp.MustCompile(`pat_[a-zA-Z0-9]{32,}`),                                     // Personal access tokens
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),     // JWT
	regexp.MustCompile(`(?:AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16,}`), // AWS keys
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_\-\.]{16,}`),                       // Bearer tokens
	regexp.MustCompile(`(?i)(api_key|token|secret|password)\s*[:=]\s*\S{8,}`),      // KEY=value (min 8 char value)
}

// highEntropyRe catches long base62-ish strings that look like secrets.
var highEntropyRe = regexp.MustCompile(`\b[a-zA-Z0-9_-]{40,}\b`)

// Redactor redacts both known patterns and exact vault values.
type Redactor struct {
	exactValues []string
	patterns    []*regexp.Regexp
}

// NewRedactor builds a Redactor from a list of raw secret values.
// Pass nil or empty to pattern-only redaction.
func NewRedactor(secrets []string) *Redactor {
	vals := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if len(s) > 4 {
			vals = append(vals, s)
		}
	}
	pats := make([]*regexp.Regexp, len(keyPatterns))
	copy(pats, keyPatterns)
	return &Redactor{exactValues: vals, patterns: pats}
}

// NewRedactorFromRecords builds a Redactor from secret records.
// This is a convenience wrapper to avoid importing the secret package
// at every call site — callers with []SecretRecord go through here.
func NewRedactorFromRecords(records []MaskedSecret) *Redactor {
	vals := make([]string, 0, len(records))
	for _, r := range records {
		if len(r.Secret) > 4 {
			vals = append(vals, r.Secret)
		}
	}
	return NewRedactor(vals)
}

// MaskedSecret is a minimal stand-in for secret.SecretRecord so the redact
// package does not need to import internal/secret (avoids a cycle).
type MaskedSecret struct {
	Secret string
}

// RedactString replaces all detected secrets with <redacted>.
// Exact values are replaced first (longest first to avoid partial overlap),
// then known patterns, then high-entropy strings.
func (r *Redactor) RedactString(s string) string {
	// 1. Exact-value matches — sort by length desc to avoid partial replaces.
	for _, v := range r.exactValues {
		s = strings.ReplaceAll(s, v, "<redacted>")
	}
	// 2. Known patterns.
	for _, re := range r.patterns {
		s = re.ReplaceAllString(s, "<redacted>")
	}
	// 3. High-entropy strings — only if not already inside a redaction.
	s = highEntropyRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) >= 40 {
			return "<redacted>"
		}
		return m
	})
	return s
}

// RedactMap redacts values in a KEY=VALUE map, keeping keys intact.
func (r *Redactor) RedactMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = r.RedactString(v)
	}
	return out
}

// ContainsSecret reports whether s contains anything that looks secret.
func (r *Redactor) ContainsSecret(s string) bool {
	for _, v := range r.exactValues {
		if strings.Contains(s, v) {
			return true
		}
	}
	for _, re := range r.patterns {
		if re.MatchString(s) {
			return true
		}
	}
	if highEntropyRe.MatchString(s) {
		return true
	}
	return false
}

// RedactExact redacts only exact-value matches (ignores pattern matching).
// Useful when you want to suppress known vault values without broad regex.
func (r *Redactor) RedactExact(s string) string {
	for _, v := range r.exactValues {
		s = strings.ReplaceAll(s, v, "<redacted>")
	}
	return s
}

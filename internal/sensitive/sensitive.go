// Package sensitive provides a single source of secret/policy heuristics
// for the whole codebase. Provider validation, adapter env validation, file
// writes, profile env overrides, and redaction all flow through here so the
// security boundary does not depend on several slightly different regexes.
//
// If you are adding a new secret-detection site, add it here rather than
// inventing another local helper.
package sensitive

import (
	"regexp"
	"strings"
)

// envNamePatterns matches environment variable names that conventionally
// carry secrets. High-signal only — must not reject legitimate names.
var envNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)_KEY$`),
	regexp.MustCompile(`(?i)_TOKEN$`),
	regexp.MustCompile(`(?i)_SECRET$`),
	regexp.MustCompile(`(?i)_PASSWORD$`),
	regexp.MustCompile(`(?i)_PASSWD$`),
	regexp.MustCompile(`(?i)^(PASSWORD|PASSWD)$`),
	regexp.MustCompile(`(?i)CREDENTIAL`),
	regexp.MustCompile(`(?i)AUTH_TOKEN`),
	regexp.MustCompile(`(?i)^BEARER$`),
	regexp.MustCompile(`(?i)PRIVATE_KEY`),
	regexp.MustCompile(`(?i)API_KEY$`),
	regexp.MustCompile(`(?i)^API_KEY$`),
}

// valuePatterns matches raw secret shapes that may appear in any free-text
// field (file content, env values, config files).
var valuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bsk-or-v1-[a-f0-9]{48,}`),
	regexp.MustCompile(`\bghp_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bhf_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}`),
	regexp.MustCompile(`\b[A-Za-z0-9_-]{40,}\b`),
}

// IsSecretName reports whether an env var name conventionally carries a
// secret. Used to reject profile env overrides that shadow credential vars.
func IsSecretName(name string) bool {
	for _, re := range envNamePatterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// IsSecretValue reports whether a free-text value looks like a credential.
// Used when scanning file content, env overrides, and metadata fields.
func IsSecretValue(value string) bool {
	if len(value) < 8 {
		return false
	}
	for _, re := range valuePatterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

// IsAllowedNonSecretEnv reports whether an env name is explicitly permitted
// to appear in a non-secret context (e.g. model id, base url hints).
// Extend this list rather than loosening IsSecretName.
func IsAllowedNonSecretEnv(name string) bool {
	upper := strings.ToUpper(name)
	allowed := []string{
		"MODEL", "BASE_URL", "API_PATH", "ENDPOINT", "URL",
		"PORT", "HOST", "TIMEOUT", "RETRIES", "PROXY",
		"LANG", "LOCALE", "REGION", "COMPAT",
	}
	for _, a := range allowed {
		if strings.Contains(upper, a) {
			return true
		}
	}
	return false
}

// RedactKnownSecrets returns s with every occurrence of any known secret
// replaced by "<secret>". Use this when emitting logs, previews, or config
// backups that may contain real credential values.
func RedactKnownSecrets(s string, known []string) string {
	for _, k := range known {
		if len(k) > 4 {
			s = strings.ReplaceAll(s, k, "<secret>")
		}
	}
	return s
}

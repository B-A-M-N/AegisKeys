package provider

import (
	"net/url"
	"regexp"

	"aegiskeys/internal/sensitive"
)

var envVarRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func ValidEnvVar(v string) bool {
	if v == "" {
		return true
	}
	return envVarRe.MatchString(v)
}

func ValidBaseURL(v string) bool {
	if v == "" {
		return true
	}
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// LooksLikeSecret reports whether a free-text value looks like a credential.
// Delegates to the centralized sensitive package so all callers share the
// same heuristic.
func LooksLikeSecret(v string) bool {
	return sensitive.IsSecretValue(v) || looksLikeLongBase62(v)
}

// looksLikeLongBase62 catches long alphanumeric-with-dashes strings that
// sensitive.IsSecretValue may miss (length-only heuristic).
func looksLikeLongBase62(v string) bool {
	return len(v) > 32 && base62Re.MatchString(v)
}

var base62Re = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

// IsSecretEnvName reports whether an env var name carries a credential.
// Delegates to the centralized sensitive package.
func IsSecretEnvName(name string) bool {
	return sensitive.IsSecretName(name)
}

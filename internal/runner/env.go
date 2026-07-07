package runner

import (
	"fmt"
	"strings"
)

func BuildEnvString(envVars map[string]string, redact bool) string {
	var pairs []string
	for k, v := range envVars {
		if redact {
			pairs = append(pairs, k+"=<redacted>")
		} else {
			pairs = append(pairs, k+"="+v)
		}
	}
	return strings.Join(pairs, " ")
}

func BuildShellExport(envVars map[string]string) string {
	var sb strings.Builder
	for k, v := range envVars {
		escaped := strings.ReplaceAll(v, "'", "'\\''")
		sb.WriteString(fmt.Sprintf("export %s='%s'\n", k, escaped))
	}
	return sb.String()
}

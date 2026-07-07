package adapter

import (
	"os"
	"path/filepath"
	"strings"
)

// knownSecretKeys are env var names that, if found in a .env file, indicate
// a potential shadowing risk for AegisKeys-injected secrets.
var knownSecretKeys = []string{
	"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY",
	"GEMINI_API_KEY", "GOOGLE_API_KEY", "MISTRAL_API_KEY",
	"COHERE_API_KEY", "TOGETHER_API_KEY", "FIREWORKS_API_KEY",
	"DEEPSEEK_API_KEY", "MOONSHOT_API_KEY", "GROQ_API_KEY",
	"NVIDIA_API_KEY", "QWEN_API_KEY",
}

// CheckDotEnvShadowing scans common .env locations for known API keys that
// could shadow AegisKeys-injected secrets. Returns hazards for each risk found.
func CheckDotEnvShadowing(workdir string) []Hazard {
	hazards := []Hazard{}
	locations := dotEnvLocations(workdir)
	for _, path := range locations {
		if keys := scanDotEnvForSecrets(path); len(keys) > 0 {
			hazards = append(hazards, Hazard{
				Severity: "high",
				Title:    "Project .env contains API keys that may shadow AegisKeys",
				Detail:   path + " contains: " + strings.Join(keys, ", "),
				Fix:      "Launch with --env-file /dev/null or remove the keys from .env",
			})
		}
	}
	return hazards
}

// dotEnvLocations returns candidate .env file paths to check.
func dotEnvLocations(workdir string) []string {
	locations := []string{}
	if workdir != "" {
		locations = append(locations, filepath.Join(workdir, ".env"))
	}
	if home := os.Getenv("HOME"); home != "" {
		locations = append(locations, filepath.Join(home, ".env"))
	}
	if cwd, err := os.Getwd(); err == nil {
		locations = append(locations, filepath.Join(cwd, ".env"))
	}
	return locations
}

// scanDotEnvForSecrets reads a .env file and returns any known secret keys found.
func scanDotEnvForSecrets(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	found := []string{}
	for _, key := range knownSecretKeys {
		if strings.Contains(content, key+"=") {
			found = append(found, key)
		}
	}
	return found
}

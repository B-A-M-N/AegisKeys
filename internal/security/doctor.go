package security

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// SecurityPolicy configures the minimum acceptable security posture.
// Doctor uses it to report deviations.
type SecurityPolicy struct {
	ConfigDirMode    os.FileMode // default 0700
	VaultMode        os.FileMode // default 0600
	AuditMode        os.FileMode // default 0600
	RequireKDFParams bool        // require stored KDF params
	MinMemoryKiB     uint32      // minimum Argon2 memory
	MinTime          uint32      // minimum Argon2 time
}

// DefaultSecurityPolicy returns the standard AegisKeys security policy.
func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		ConfigDirMode:    0700,
		VaultMode:        0600,
		AuditMode:        0600,
		RequireKDFParams: true,
		MinMemoryKiB:     64 * 1024,
		MinTime:          3,
	}
}

type Severity int

const (
	SeverityOK Severity = iota
	SeverityWarn
	SeverityFail
)

type CheckResult struct {
	Severity Severity
	Message  string
	Fix      string
}

func RunDoctor(configDir string) []CheckResult {
	policy := DefaultSecurityPolicy()
	checks := []struct {
		label string
		fn    func() CheckResult
	}{
		{"Config exists", func() CheckResult { return checkConfigDir(configDir) }},
		{"Config permissions", func() CheckResult { return checkConfigPermissions(configDir, policy) }},
		{"Providers loaded", func() CheckResult { return checkProviders(configDir) }},
		{"Secrets encrypted", func() CheckResult { return checkVault(configDir) }},
		{"Vault permissions", func() CheckResult { return checkVaultPermissions(configDir, policy) }},
		{"Vault crypto metadata", func() CheckResult { return checkVaultEnvelope(configDir, policy) }},
		{".gitignore protective", func() CheckResult { return checkGitignore(configDir) }},
		{"No plaintext secrets", func() CheckResult { return checkPlaintext(configDir) }},
		{"Current tree plaintext secrets", func() CheckResult { return checkCurrentTreePlaintext(configDir) }},
		{"Audit log present", func() CheckResult { return checkAudit(configDir, policy) }},
		{"Profiles reference valid keys", func() CheckResult { return checkProfiles(configDir) }},
		{"Providers validate strictly", func() CheckResult { return checkProvidersStrict(configDir) }},
		{"Adapter contracts complete", func() CheckResult { return checkContracts() }},
		{"Adapter proof status", func() CheckResult { return checkAdapterProofStatus() }},
		{"Temp env files not stale", func() CheckResult { return checkTempEnvfiles(configDir) }},
	}
	var results []CheckResult
	for _, c := range checks {
		results = append(results, c.fn())
	}
	return results
}

func checkConfigDir(dir string) CheckResult {
	if _, err := os.Stat(dir); err == nil {
		return CheckResult{SeverityOK, "Config directory exists", ""}
	}
	return CheckResult{SeverityFail, "Config directory missing: " + dir, "Run `aegiskeys init`"}
}

func checkConfigPermissions(dir string, policy SecurityPolicy) CheckResult {
	info, err := os.Stat(dir)
	if err != nil {
		return CheckResult{SeverityFail, "Cannot stat config dir", ""}
	}
	m := info.Mode().Perm()
	if m == policy.ConfigDirMode {
		return CheckResult{SeverityOK, fmt.Sprintf("Config dir permissions %04o (safe)", m), ""}
	}
	return CheckResult{SeverityWarn, fmt.Sprintf("Config dir permissions %04o (expected %04o)", m, policy.ConfigDirMode), "chmod 0700 " + dir}
}

func checkProviders(dir string) CheckResult {
	providers := filepath.Join(dir, "providers.json")
	data, err := os.ReadFile(providers)
	if err != nil {
		return CheckResult{SeverityWarn, "Provider registry missing", "Run `aegiskeys init`"}
	}
	var reg struct {
		Providers []struct {
			Slug string `json:"slug"`
		} `json:"providers"`
	}
	_ = json.Unmarshal(data, &reg)
	if len(reg.Providers) >= 19 {
		return CheckResult{SeverityOK, fmt.Sprintf("%d providers loaded", len(reg.Providers)), ""}
	}
	return CheckResult{SeverityWarn, fmt.Sprintf("Only %d providers loaded (expected 19+)", len(reg.Providers)), "Run `aegiskeys init`"}
}

func checkVault(dir string) CheckResult {
	vault := filepath.Join(dir, "vault.enc")
	if _, err := os.Stat(vault); err == nil {
		return CheckResult{SeverityOK, "Vault file exists", ""}
	}
	return CheckResult{SeverityWarn, "No vault found (no keys stored yet)", "aegiskeys key add"}
}

func checkGitignore(dir string) CheckResult {
	// Only run gitignore check if the config dir is inside a git repo.
	// Default config is ~/.config/aegiskeys, which is NOT in a repo.
	repo := findGitRoot(dir)
	if repo == "" {
		return CheckResult{SeverityOK, "Config dir not in a git repo, no .gitignore check needed", ""}
	}
	gitignore := filepath.Join(repo, ".gitignore")
	if _, err := os.Stat(gitignore); err != nil {
		return CheckResult{SeverityWarn, ".gitignore not found in repo root", "Add aegiskeys.local.*, vault.enc, *.env to .gitignore"}
	}
	data, _ := os.ReadFile(gitignore)
	needs := []string{"vault.enc", "*.env", ".aegiskeys/", "*.secret"}
	for _, n := range needs {
		if !strings.Contains(string(data), n) {
			return CheckResult{SeverityWarn, ".gitignore missing protection: " + n, "Add '" + n + "' to .gitignore"}
		}
	}
	return CheckResult{SeverityOK, ".gitignore protects vault and secrets", ""}
}

// findGitRoot walks up from dir to find the nearest .git directory.
// Returns the repo root path or "" if no git repo found.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root
		}
		dir = parent
	}
}

var secretHeuristic = regexp.MustCompile(`[a-zA-Z0-9]{20,}`)

// checkVaultPermissions verifies the vault file is 0600.
func checkVaultPermissions(dir string, policy SecurityPolicy) CheckResult {
	vault := filepath.Join(dir, "vault.enc")
	info, err := os.Stat(vault)
	if err != nil {
		return CheckResult{SeverityOK, "No vault file", ""}
	}
	if info.Mode().Perm() == policy.VaultMode {
		return CheckResult{SeverityOK, "Vault file permissions are 0600", ""}
	}
	return CheckResult{SeverityWarn, fmt.Sprintf("Vault file permissions %04o (expected %04o)", info.Mode().Perm(), policy.VaultMode), "chmod 0600 " + vault}
}

// checkVaultEnvelope verifies the vault file is a valid encrypted envelope
// with correct KDF (argon2id), valid nonce/salt lengths, and current KDF params.
func checkVaultEnvelope(dir string, policy SecurityPolicy) CheckResult {
	vault := filepath.Join(dir, "vault.enc")
	data, err := os.ReadFile(vault)
	if err != nil {
		return CheckResult{SeverityOK, "No vault file", ""}
	}
	var env secret.VaultEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return CheckResult{SeverityFail, "Vault file is not a valid encrypted envelope", "Re-initialize with `aegiskeys init`"}
	}
	if env.KDF != "argon2id" {
		return CheckResult{SeverityFail, fmt.Sprintf("Vault uses KDF %q (required: argon2id)", env.KDF), "Re-initialize with `aegiskeys init`"}
	}
	// Nonce must decode to exactly 12 bytes (GCM standard).
	if nb, err := base64.StdEncoding.DecodeString(env.Nonce); err != nil || len(nb) != 12 {
		return CheckResult{SeverityFail, "Vault nonce is invalid (expected 12 bytes base64)", "Re-initialize with `aegiskeys init`"}
	}
	// Salt must decode to a reasonable length (>= 16 bytes).
	if sb, err := base64.StdEncoding.DecodeString(env.Salt); err != nil || len(sb) < 16 {
		return CheckResult{SeverityFail, "Vault salt is invalid (expected >= 16 bytes base64)", "Re-initialize with `aegiskeys init`"}
	}
	if policy.RequireKDFParams {
		if env.KDFParams.Time == 0 {
			return CheckResult{SeverityWarn, "Vault lacks KDF params (sealed with old version) — rekey recommended", "Run `aegiskeys vault rekey` or use the TUI doctor rekey action"}
		}
		if env.KDFParams.Time < policy.MinTime || env.KDFParams.MemoryKiB < policy.MinMemoryKiB {
			return CheckResult{SeverityWarn, fmt.Sprintf("Vault KDF costs below policy (time=%d, mem=%d)", env.KDFParams.Time, env.KDFParams.MemoryKiB), "Run `aegiskeys vault rekey` or use the TUI doctor rekey action"}
		}
	}
	return CheckResult{SeverityOK, "Vault envelope valid (argon2id, current KDF params)", ""}
}

// checkTempEnvfiles warns about env files in tmp/ older than 24h. It walks
// the temp tree recursively so nested env files (e.g. tmp/envfiles/*.env) are
// detected, not just the top-level entries.
func checkTempEnvfiles(dir string) CheckResult {
	tmpDir := filepath.Join(dir, "tmp")
	old := []string{}
	cutoff := time.Now().Add(-24 * time.Hour)
	err := filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr // stop on access error
		}
		if d.IsDir() {
			return nil
		}
		// Only flag env-style files.
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".env" && ext != ".envfile" {
			return nil
		}
		info, _ := d.Info()
		if info != nil && info.ModTime().Before(cutoff) {
			// Report a path relative to the config dir for readability.
			if rel, rerr := filepath.Rel(dir, path); rerr == nil {
				old = append(old, rel)
			} else {
				old = append(old, path)
			}
		}
		return nil
	})
	if err != nil {
		return CheckResult{SeverityOK, "No temp directory", ""}
	}
	if len(old) > 0 {
		return CheckResult{SeverityWarn, fmt.Sprintf("Stale temp env file(s): %s", strings.Join(old, ", ")), "Run `aegiskeys shred-envfile <path>`"}
	}
	return CheckResult{SeverityOK, "No stale temp env files", ""}
}

// checkProfiles verifies that every profile references a provider that exists
// in the registry. Key existence cannot be verified without unlocking the
// vault, so this guard catches the more common misconfiguration: a profile
// bound to a provider slug that is not registered.
func checkProfiles(dir string) CheckResult {
	var store struct {
		Profiles []struct {
			Name         string `json:"name"`
			ProviderSlug string `json:"provider_slug"`
		} `json:"profiles"`
	}
	if err := readJSON(filepath.Join(dir, "profiles.json"), &store); err != nil {
		return CheckResult{SeverityOK, "No profiles file", ""}
	}
	var reg struct {
		Providers []struct {
			Slug string `json:"slug"`
		} `json:"providers"`
	}
	_ = readJSON(filepath.Join(dir, "providers.json"), &reg)
	slugs := map[string]bool{}
	for _, p := range reg.Providers {
		slugs[p.Slug] = true
	}
	for _, p := range store.Profiles {
		if !slugs[p.ProviderSlug] {
			return CheckResult{SeverityWarn, fmt.Sprintf("profile %s references missing provider %s", p.Name, p.ProviderSlug), "Run `aegiskeys profile list`"}
		}
	}
	return CheckResult{SeverityOK, fmt.Sprintf("All %d profile(s) reference known providers", len(store.Profiles)), ""}
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// checkKDFFreshness returns true if the vault envelope uses KDF params
// below the current policy. Used by RunDoctorUnlocked to warn the user.
func checkKDFFreshness(configDir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(configDir, "vault.enc"))
	if err != nil {
		return false, err
	}
	var env secret.VaultEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return false, err
	}
	return secret.NeedsRekey(&env), nil
}

const maxSecretScanBytes = 1 << 20 // 1 MiB

// secretContentPatterns matches high-signal credential patterns in file content.
var secretContentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)\s*[:=]\s*["']?[A-Za-z0-9._\-]{20,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9._\-]{20,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`hf_[A-Za-z0-9_]{20,}`),
}

// fileContainsSecrets reads a small text file and returns true if it looks
// like it contains high-entropy credential material.
func fileContainsSecrets(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxSecretScanBytes {
		return false
	}
	s := string(data)
	for _, re := range secretContentPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func checkPlaintext(dir string) CheckResult {
	return checkPlaintextRoot(dir, "config dir")
}

func checkCurrentTreePlaintext(configDir string) CheckResult {
	wd, err := os.Getwd()
	if err != nil {
		return CheckResult{SeverityOK, "Cannot determine current working tree", ""}
	}
	if samePath(wd, configDir) {
		return CheckResult{SeverityOK, "Current tree is config dir; already scanned", ""}
	}
	return checkPlaintextRoot(wd, "current tree")
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && filepath.Clean(aa) == filepath.Clean(bb)
}

func checkPlaintextRoot(dir, label string) CheckResult {
	found := []string{}
	walkRoot := filepath.Clean(dir)
	_ = filepath.Walk(walkRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Base(path) == "vault.enc" {
			return nil
		}
		if isBinaryOrCache(path) {
			return nil
		}
		// Documentation, Go test files, and testdata/ fixtures intentionally
		// contain secret-shaped strings (source snippets, adversarial fixtures
		// with fake tokens), so they are not plaintext-secret signals. Skipping
		// them avoids cry-wolf on the project's own tree while still scanning
		// real source, config, and data files.
		if strings.EqualFold(filepath.Ext(path), ".md") ||
			strings.HasSuffix(info.Name(), "_test.go") ||
			isUnderSegment(path, "testdata") {
			return nil
		}
		rel, _ := filepath.Rel(walkRoot, path)
		// Check for suspicious .env-like filenames.
		if strings.Contains(info.Name(), ".env") && !strings.HasSuffix(info.Name(), ".example") {
			found = append(found, rel)
			return nil
		}
		// Scan file content for credential patterns (skip large/non-text files).
		if fileContainsSecrets(path) {
			found = append(found, rel+" (content) ")
		}
		return nil
	})
	if len(found) > 0 {
		return CheckResult{SeverityWarn, fmt.Sprintf("Possible plaintext secret files in %s: %s", label, strings.Join(found, ", ")), "Remove or encrypt these files"}
	}
	return CheckResult{SeverityOK, "No obvious plaintext secret files in " + label, ""}
}

// isUnderSegment reports whether path contains a directory component named seg.
func isUnderSegment(path, seg string) bool {
	return slices.Contains(strings.Split(path, string(os.PathSeparator)), seg)
}

// isBinaryOrCache returns true for files that should not be scanned for secrets.
func isBinaryOrCache(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".exe", ".dll", ".so", ".dylib", ".bin", ".png", ".jpg", ".gif", ".ico", ".zip", ".gz", ".tar":
		return true
	}
	// Skip common cache dirs.
	return strings.Contains(path, "/node_modules/") || strings.Contains(path, "/.cache/") || strings.Contains(path, "/target/")
}

func checkAudit(dir string, policy SecurityPolicy) CheckResult {
	log := filepath.Join(dir, "audit.log")
	if _, err := os.Stat(log); err == nil {
		info, _ := os.Stat(log)
		m := info.Mode().Perm()
		if m == policy.AuditMode {
			return CheckResult{SeverityOK, "Audit log exists with 0600 permissions", ""}
		}
		return CheckResult{SeverityWarn, fmt.Sprintf("Audit log permissions %04o (expected 0600)", m), "chmod 0600 " + log}
	}
	return CheckResult{SeverityWarn, "Audit log not found", "Enables audit logging in settings"}
}

// checkProvidersStrict verifies every provider in the registry passes
// ValidateStrict — catches secrets accidentally pasted into metadata.
func checkProvidersStrict(dir string) CheckResult {
	reg, err := provider.LoadRegistry(filepath.Join(dir, "providers.json"))
	if err != nil {
		return CheckResult{SeverityOK, "No providers file", ""}
	}
	for i := range reg.Providers {
		p := &reg.Providers[i]
		p.Normalize()
		if vErr := p.ValidateStrict(); vErr != nil {
			return CheckResult{
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("provider %q failed strict validation: %v", p.Slug, vErr),
				Fix:      "Edit provider metadata; secrets belong in the vault, not providers.json",
			}
		}
	}
	return CheckResult{SeverityOK, fmt.Sprintf("All %d provider(s) pass strict validation", len(reg.Providers)), ""}
}

// checkContracts verifies every registered adapter has a complete contract.
func checkContracts() CheckResult {
	reg := adapter.NewRegistry()
	errs := reg.ValidateAllContracts()
	if len(errs) > 0 {
		return CheckResult{SeverityFail, fmt.Sprintf("%d adapter(s) have incomplete contracts", len(errs)), "Update the adapter's Contract() method"}
	}
	return CheckResult{SeverityOK, fmt.Sprintf("All %d adapter contracts complete", len(reg.AllIDs())), ""}
}

func checkAdapterProofStatus() CheckResult {
	reg := adapter.NewRegistry()
	counts := map[adapter.SupportConfidence]int{}
	var falseVerified []string
	var missingProofs []string
	var missingGoldens []string
	proofDir := findAdapterProofDir()
	goldenDir := findAdapterGoldenDir()

	for _, a := range reg.All() {
		c := a.Contract()
		conf := adapter.DemotedConfidence(c)
		counts[conf]++
		if c.SupportConfidence == adapter.ConfidenceVerified && !c.Verification.Verified() {
			falseVerified = append(falseVerified, c.ID)
		}
		if (conf == adapter.ConfidenceManualProof || conf == adapter.ConfidenceVerified) && proofDir != "" {
			pattern := filepath.Join(proofDir, c.ID+".*.proof.json")
			files, err := filepath.Glob(pattern)
			if err != nil || len(files) == 0 {
				missingProofs = append(missingProofs, c.ID)
			}
		}
		if conf == adapter.ConfidenceVerified && goldenDir != "" {
			pattern := filepath.Join(goldenDir, c.ID+".*.golden.json")
			files, err := filepath.Glob(pattern)
			if err != nil || len(files) == 0 {
				missingGoldens = append(missingGoldens, c.ID)
			}
		}
	}

	if len(falseVerified) > 0 {
		sort.Strings(falseVerified)
		return CheckResult{
			Severity: SeverityFail,
			Message:  fmt.Sprintf("%d falsely verified adapter(s): %s", len(falseVerified), strings.Join(falseVerified, ", ")),
			Fix:      "Set confidence below verified or complete all verification gates",
		}
	}
	if len(missingProofs) > 0 {
		sort.Strings(missingProofs)
		return CheckResult{
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("adapter(s) missing proof files: %s", strings.Join(missingProofs, ", ")),
			Fix:      "Add testdata/adapter_proofs/<adapter>.<provider>.proof.json",
		}
	}
	if len(missingGoldens) > 0 {
		sort.Strings(missingGoldens)
		return CheckResult{
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("verified adapter(s) missing render golden files: %s", strings.Join(missingGoldens, ", ")),
			Fix:      "Add testdata/adapter_golden/<adapter>.<provider>.golden.json",
		}
	}

	msg := fmt.Sprintf(
		"Adapters: %d manual-proof, %d verified, %d experimental, %d guided, %d blocked",
		counts[adapter.ConfidenceManualProof],
		counts[adapter.ConfidenceVerified],
		counts[adapter.ConfidenceExperimental],
		counts[adapter.ConfidenceGuided],
		counts[adapter.ConfidenceBlocked],
	)
	if proofDir == "" {
		msg += " (proof files not available from current working tree)"
	}
	return CheckResult{SeverityOK, msg, ""}
}

func findAdapterProofDir() string {
	return findRepoTestdataDir("adapter_proofs")
}

func findAdapterGoldenDir() string {
	return findRepoTestdataDir("adapter_golden")
}

func findRepoTestdataDir(name string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root := findGitRoot(cwd)
	if root == "" {
		return ""
	}
	dir := filepath.Join(root, "testdata", name)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// RunDoctorUnlocked runs the full locked doctor suite plus vault-aware checks
// requiring decrypted secrets. Call only after the vault is unlocked. Merges
// the results of RunDoctor with the unlocked-only checks so callers get a
// single consistent result slice.
func RunDoctorUnlocked(configDir string, vault *secret.Vault, store *profile.Store) []CheckResult {
	results := RunDoctor(configDir)

	// Vault KDF freshness check (read envelope from disk).
	needsRekey, err := checkKDFFreshness(configDir)
	if err == nil && needsRekey {
		results = append(results, CheckResult{
			Severity: SeverityWarn,
			Message:  "Vault KDF costs are outdated — rekey recommended",
			Fix:      "Run `aegiskeys vault rekey`",
		})
	}

	// Profile → key resolution: ensure every profile's key id exists.
	for _, p := range store.Profiles {
		if p.KeyID == "" {
			results = append(results, CheckResult{
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("profile %q has no key assigned", p.Name),
				Fix:      "Assign a key via `aegiskeys profile create` or the TUI wizard",
			})
			continue
		}
		rec := vault.Get(p.KeyID)
		if rec == nil {
			results = append(results, CheckResult{
				Severity: SeverityFail,
				Message:  fmt.Sprintf("profile %q references missing key %s", p.Name, p.KeyID),
				Fix:      "Key was deleted; reassign or recreate the profile",
			})
			continue
		}
		if rec.ProviderSlug != "" && !provider.CredentialCompatible(p.ProviderSlug, rec.ProviderSlug) {
			results = append(results, CheckResult{
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("profile %q: key provider %q does not match profile provider %q", p.Name, rec.ProviderSlug, p.ProviderSlug),
				Fix:      "Reassign a key for the correct provider",
			})
		}
	}

	return results
}

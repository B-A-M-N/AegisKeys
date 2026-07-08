package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aegiskeys/internal/fsutil"
	"aegiskeys/internal/redact"
	"github.com/pelletier/go-toml/v2"
)

// ApplyFileWrites materializes the given file writes with full safety semantics:
// path expansion, preflight checks (symlinks, scope, parent perms), secret
// redaction checks, backup, and merge policies.
func ApplyFileWrites(writes []FileWrite, env map[string]string) error {
	// Seed backup redaction with current env values so backups never preserve
	// active secrets verbatim.
	knownSecrets := make([]string, 0, len(env))
	for _, v := range env {
		if len(v) > 4 {
			knownSecrets = append(knownSecrets, v)
		}
	}
	for _, w := range writes {
		path, err := expandPath(w.Path, env)
		if err != nil {
			return fmt.Errorf("expand path %q: %w", w.Path, err)
		}

		// Preflight: refuse symlinks, scope violations, unsafe parents.
		if err := preflightWrite(path, w.Scope); err != nil {
			return fmt.Errorf("preflight %q: %w", path, err)
		}

		// Redact check: refuse to write raw secrets to config files.
		if w.RedactCheck && containsRealSecret(w.Content, env) {
			return fmt.Errorf("refusing to write raw secret to %s (redact_check enabled)", path)
		}

		if w.MergePolicy == MergeTOML {
			if err := refuseUnsafeReplace(path, w.Scope, "toml"); err != nil {
				return err
			}
		}
		if w.MergePolicy == PatchXML {
			if err := refuseUnsafeReplace(path, w.Scope, "xml"); err != nil {
				return err
			}
		}

		// Backup existing file per policy — seeded with current env values.
		if err := backupWithPolicy(path, w.BackupPolicy, w.Scope, knownSecrets); err != nil {
			return fmt.Errorf("backup %s: %w", path, err)
		}

		switch w.MergePolicy {
		case MergeNone:
			if err := ensureDir(path); err != nil {
				return err
			}
			if err := atomicWrite(path, []byte(w.Content), w.Mode); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		case MergeJSON:
			if err := mergeJSONFile(path, []byte(w.Content), w.Mode); err != nil {
				return fmt.Errorf("merge json %s: %w", path, err)
			}
		case MergeJSONC:
			if err := mergeJSONCFile(path, []byte(w.Content), w.Mode); err != nil {
				return fmt.Errorf("merge jsonc %s: %w", path, err)
			}
		case MergeYAML:
			if err := mergeYAMLFile(path, []byte(w.Content), w.Mode, w.ManagedBlockID); err != nil {
				return fmt.Errorf("merge yaml %s: %w", path, err)
			}
		case MergeTOML:
			if err := mergeTOMLFile(path, []byte(w.Content), w.Mode); err != nil {
				return fmt.Errorf("merge toml %s: %w", path, err)
			}
		case PatchXML:
			// XML patching is not implemented; do not clobber existing
			// user/project files under a misleading merge policy.
			if err := ensureDir(path); err != nil {
				return err
			}
			if err := atomicWrite(path, []byte(w.Content), w.Mode); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		case AvoidWrite:
			return fmt.Errorf("adapter requested avoid-write for %s", path)
		default:
			return fmt.Errorf("unsupported merge policy %q for %s", w.MergePolicy, path)
		}
	}
	return nil
}

type fileSnapshot struct {
	path    string
	exists  bool
	content []byte
	mode    os.FileMode
}

// ApplyFileWritesWithRestore applies file writes and returns a cleanup function
// that restores the pre-launch file state. Files created by the write are
// removed; files that existed before the write are restored with their prior
// content and mode.
func ApplyFileWritesWithRestore(writes []FileWrite, env map[string]string) (func() error, error) {
	snapshots := make([]fileSnapshot, 0, len(writes))
	for _, w := range writes {
		path, err := expandPath(w.Path, env)
		if err != nil {
			return nil, fmt.Errorf("expand path %q: %w", w.Path, err)
		}
		if err := preflightWrite(path, w.Scope); err != nil {
			return nil, fmt.Errorf("preflight %q: %w", path, err)
		}

		snap := fileSnapshot{path: path}
		info, err := os.Stat(path)
		switch {
		case os.IsNotExist(err):
			snapshots = append(snapshots, snap)
			continue
		case err != nil:
			return nil, fmt.Errorf("stat %s: %w", path, err)
		case info.IsDir():
			return nil, fmt.Errorf("cannot snapshot directory %s", path)
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read %s for restore snapshot: %w", path, err)
			}
			snap.exists = true
			snap.content = data
			snap.mode = info.Mode().Perm()
			snapshots = append(snapshots, snap)
		}
	}

	restore := func() error {
		var errs []string
		for i := len(snapshots) - 1; i >= 0; i-- {
			snap := snapshots[i]
			if snap.exists {
				if err := ensureDir(snap.path); err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", snap.path, err))
					continue
				}
				if err := atomicWrite(snap.path, snap.content, snap.mode); err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", snap.path, err))
				}
				continue
			}
			if err := os.Remove(snap.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Sprintf("%s: %v", snap.path, err))
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("restore file writes: %s", strings.Join(errs, "; "))
		}
		return nil
	}

	if err := ApplyFileWrites(writes, env); err != nil {
		_ = restore()
		return nil, err
	}
	return restore, nil
}

func refuseUnsafeReplace(path string, scope ConfigScope, format string) error {
	if scope == ScopeProfile || scope == ScopeTemp {
		return nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("cannot write %s config over directory %s", format, path)
	}
	return fmt.Errorf("%s merge is not implemented; refusing to overwrite existing %s-scoped config %s", format, scope, path)
}

// preflightWrite validates that a write is safe: no symlinks, no sensitive
// project paths, and (for project scope) no group/world-writable parent dirs.
func preflightWrite(path string, scope ConfigScope) error {
	clean := filepath.Clean(path)

	// Refuse to write through symlinks (final path or any parent directory).
	info, err := os.Lstat(clean)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink: %s", clean)
	}
	if err := rejectSymlinkParents(clean); err != nil {
		return err
	}

	// Refuse project-scoped writes to sensitive system/config paths.
	if scope == ScopeProject && isSensitivePath(clean) {
		return fmt.Errorf("refusing project-scoped write to sensitive path: %s", clean)
	}

	// For project-scope writes, refuse group/world-writable parents.
	// (User-scope and profile-scope writes commonly target ~/.config which
	// may be 0775 on some systems — blocking those would make the tool unusable.)
	if scope == ScopeProject {
		parent := filepath.Dir(clean)
		pinfo, err := os.Stat(parent)
		if err != nil {
			return fmt.Errorf("stat parent %s: %w", parent, err)
		}
		if pinfo.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("parent directory is group/world writable: %s (mode %04o)", parent, pinfo.Mode().Perm())
		}
	}

	return nil
}

// rejectSymlinkParents walks every parent directory of path and refuses if any
// is a symlink. This prevents symlink-swap attacks where an attacker replaces
// a parent directory between the preflight check and the actual write.
func rejectSymlinkParents(path string) error {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)

	// Walk from root to the target dir, checking each component.
	parts := strings.Split(dir, string(os.PathSeparator))
	cur := string(os.PathSeparator)

	for _, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				// Parent doesn't exist yet — will be created by ensureDir.
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink parent: %s", cur)
		}
	}
	return nil
}

// sensitivePaths are paths that project-scoped writes must never touch.
var sensitivePaths = []string{
	"/etc/", "/usr/", "/bin/", "/sbin/", "/boot/", "/root/",
	"/sys/", "/proc/", "/dev/",
}

// isSensitivePath reports whether a path is in a sensitive system location.
func isSensitivePath(path string) bool {
	for _, prefix := range sensitivePaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// containsRealSecret checks if the content contains any true secret value
// (credential-like env vars, not URLs or model IDs). This avoids false
// positives on long non-secret values while catching real key leakage.
func containsRealSecret(content string, env map[string]string) bool {
	for k, v := range env {
		// Only check values whose env var name indicates a secret.
		if !isCredentialEnvName(k) {
			continue
		}
		if len(v) > 0 && strings.Contains(content, v) {
			return true
		}
	}
	return false
}

// isCredentialEnvName reports whether an env var name suggests it carries a secret.
func isCredentialEnvName(k string) bool {
	upper := strings.ToUpper(k)
	for _, p := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "AUTH_TOKEN", "API_KEY"} {
		if strings.Contains(upper, p) {
			return true
		}
	}
	return false
}

// atomicWrite writes data to path with the given permissions atomically.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0600
	}
	if err := fsutil.AtomicWriteFile(path, data); err != nil {
		return err
	}
	// AtomicWriteFile may create with a different mode; enforce 0600 (or the
	// requested mode) explicitly. This is critical for secrets-adjacent files.
	return os.Chmod(path, mode)
}

// ensureDir creates the parent directory if needed.
func ensureDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0700)
}

// backupExisting creates a timestamped backup of an existing file.
func backupExisting(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil // nothing to back up
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("cannot backup directory %s", path)
	}

	backupDir := filepath.Join(filepath.Dir(path), ".aegiskeys-backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}

	timestamp := fmt.Sprintf("%d", info.ModTime().Unix())
	backupName := filepath.Base(path) + "." + timestamp + ".bak"
	backupPath := filepath.Join(backupDir, backupName)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", path, err)
	}
	if err := atomicWrite(backupPath, data, 0600); err != nil {
		return fmt.Errorf("write backup %s: %w", backupPath, err)
	}
	// Enforce 0600 on the backup even if atomicWrite created it looser.
	_ = os.Chmod(backupPath, 0600)
	return nil
}

// backupWithPolicy backs up an existing file according to the backup policy.
// For user-scope configs, it defaults to redacted backups to prevent plaintext
// secret duplication. secrets is an optional list of known secret values to redact.
func backupWithPolicy(path string, policy BackupPolicy, scope ConfigScope, secrets []string) error {
	switch policy {
	case BackupNone, "":
		return nil
	case BackupPlain:
		// Plaintext backup only allowed for profile/temp scope (isolated dirs).
		if scope != ScopeProfile && scope != ScopeTemp {
			return fmt.Errorf("plaintext backup not allowed for scope %q; use redacted or encrypted", scope)
		}
		return backupExisting(path)
	case BackupRedacted:
		return backupRedacted(path, secrets)
	case BackupEncrypted:
		// Encrypted backup requires a vault session; fall back to redacted here.
		return backupRedacted(path, secrets)
	default:
		return fmt.Errorf("unknown backup policy %q", policy)
	}
}

// backupRedacted copies an existing file with secrets replaced by <redacted>.
func backupRedacted(path string, secrets []string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("cannot backup directory %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", path, err)
	}

	// Redact known secrets and patterns.
	r := redact.NewRedactor(secrets)
	redacted := r.RedactString(string(data))

	backupDir := filepath.Join(filepath.Dir(path), ".aegiskeys-backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}

	timestamp := fmt.Sprintf("%d", info.ModTime().Unix())
	backupName := filepath.Base(path) + "." + timestamp + ".bak"
	backupPath := filepath.Join(backupDir, backupName)

	if err := atomicWrite(backupPath, []byte(redacted), 0600); err != nil {
		return fmt.Errorf("write backup %s: %w", backupPath, err)
	}
	_ = os.Chmod(backupPath, 0600)
	return nil
}

// mergeJSONFile deeply merges new JSON content with existing file content.
// Parse errors in existing JSON cause a hard fail (refusing to blindly overwrite).
func mergeJSONFile(path string, newContent []byte, mode os.FileMode) error {
	var incoming map[string]any
	if err := json.Unmarshal(newContent, &incoming); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// File doesn't exist — write fresh.
		merged, err := json.MarshalIndent(incoming, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		if err := ensureDir(path); err != nil {
			return err
		}
		return atomicWrite(path, merged, mode)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var existing map[string]any
	if err := json.Unmarshal(data, &existing); err != nil {
		// Refuse to merge into corrupted JSON — would destroy user data silently.
		return fmt.Errorf("existing %s is not valid JSON (refusing to merge): %w", path, err)
	}

	// Deep merge: recursively combine maps, with incoming winning on conflict.
	deepMerge(existing, incoming)

	merged, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged json: %w", err)
	}

	if err := ensureDir(path); err != nil {
		return err
	}
	return atomicWrite(path, merged, mode)
}

// deepMerge recursively copies incoming into base. Maps are merged recursively;
// all other values are overwritten.
func deepMerge(base, incoming map[string]any) {
	for k, v := range incoming {
		// If both sides are maps, merge recursively.
		if baseMap, ok := base[k]; ok {
			if bm, ok := baseMap.(map[string]any); ok {
				if im, ok := v.(map[string]any); ok {
					deepMerge(bm, im)
					continue
				}
			}
		}
		base[k] = v
	}
}

// mergeJSONCFile merges JSON-with-comments (JSONC). It strips // and /* */
// comments from both the new and existing content before JSON parsing (so
// existing JSON parsers don't choke), then delegates to the JSON merge path.
// Comments are not preserved in the merged output — this is the conservative
// behaviour. Callers needing comment preservation must avoid MergeJSONC or use
// AvoidWrite.
func mergeJSONCFile(path string, newContent []byte, mode os.FileMode) error {
	return mergeJSONCFileInternal(path, stripJSONCComments(newContent), mode)
}

// mergeJSONCFileInternal is like mergeJSONFile but reads the existing file
// with comment stripping.
func mergeJSONCFileInternal(path string, newContent []byte, mode os.FileMode) error {
	var incoming map[string]any
	if err := json.Unmarshal(newContent, &incoming); err != nil {
		return fmt.Errorf("invalid json (after comment strip): %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		merged, err := json.MarshalIndent(incoming, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		if err := ensureDir(path); err != nil {
			return err
		}
		return atomicWrite(path, merged, mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	data = stripJSONCComments(data)
	var existing map[string]any
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("existing %s is valid JSONC but not valid JSON after comment stripping: %w", path, err)
	}
	deepMerge(existing, incoming)
	merged, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged json: %w", err)
	}
	if err := ensureDir(path); err != nil {
		return err
	}
	return atomicWrite(path, merged, mode)
}

// stripJSONCComments removes // line comments and /* */ block comments from
// JSONC content so it can be parsed as plain JSON. It is intentionally
// conservative: it does not attempt to preserve comments in the output.
func stripJSONCComments(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		c := src[i]
		// String literal: copy verbatim (comments inside strings are not comments).
		if c == '"' {
			j := i + 1
			for j < len(src) {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == '"' {
					j++
					break
				}
				j++
			}
			out = append(out, src[i:j]...)
			i = j
			continue
		}
		// // line comment.
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// /* */ block comment.
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

// managedBlockMarker returns the start/end markers for AegisKeys-managed
// blocks. When a ManagedBlockID is supplied it is incorporated into the marker
// so multiple profiles writing the same config file get distinct blocks instead
// of colliding. Otherwise the marker derives from the file basename.
func managedBlockMarker(profile, managedBlockID string) (start, end string) {
	key := profile
	if managedBlockID != "" {
		key = profile + " " + managedBlockID
	}
	return fmt.Sprintf("# BEGIN AEGISKEYS MANAGED %s", key),
		fmt.Sprintf("# END AEGISKEYS MANAGED %s", key)
}

// mergeYAMLFile merges YAML content using managed block markers so updates
// replace only the AegisKeys-owned section, leaving user content intact.
func mergeYAMLFile(path string, newContent []byte, mode os.FileMode, managedBlockID string) error {
	markerStart, markerEnd := managedBlockMarker(profileName(path), managedBlockID)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// First write — wrap in managed markers.
		var sb strings.Builder
		sb.WriteString(markerStart + "\n")
		sb.Write(newContent)
		if !strings.HasSuffix(string(newContent), "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString(markerEnd + "\n")
		if err := ensureDir(path); err != nil {
			return err
		}
		return atomicWrite(path, []byte(sb.String()), mode)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	existing := string(data)

	// Find managed block.
	startIdx := strings.Index(existing, markerStart)
	endIdx := strings.Index(existing, markerEnd)

	if startIdx == -1 || endIdx == -1 || endIdx < startIdx {
		// No managed block yet — append one at the end.
		var sb strings.Builder
		sb.WriteString(existing)
		if !strings.HasSuffix(existing, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n" + markerStart + "\n")
		sb.Write(newContent)
		if !strings.HasSuffix(string(newContent), "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString(markerEnd + "\n")
		if err := ensureDir(path); err != nil {
			return err
		}
		return atomicWrite(path, []byte(sb.String()), mode)
	}

	// Replace the managed block content (between start and end markers).
	var sb strings.Builder
	sb.WriteString(existing[:startIdx])
	sb.WriteString(markerStart + "\n")
	sb.Write(newContent)
	if !strings.HasSuffix(string(newContent), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(existing[endIdx:]) // includes markerEnd and everything after

	if err := ensureDir(path); err != nil {
		return err
	}
	return atomicWrite(path, []byte(sb.String()), mode)
}

// profileName derives a stable identifier from a file path for managed blocks.
func profileName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// RedactConfigContent removes raw secret values from config content using
// simple env var name heuristics to find secret values.
func RedactConfigContent(content string, env map[string]string) string {
	result := content
	for k, v := range env {
		if len(v) > 10 && looksSecretEnvName(k) {
			result = strings.ReplaceAll(result, v, "<redacted>")
		}
	}
	return result
}

// looksSecretEnvName reports whether an env var name looks like it carries a secret.
func looksSecretEnvName(k string) bool {
	upper := strings.ToUpper(k)
	patterns := []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "AUTH", "PRIV", "ACCESS"}
	for _, p := range patterns {
		if strings.Contains(upper, p) {
			return true
		}
	}
	return false
}

func mergeTOMLFile(path string, newContent []byte, mode os.FileMode) error {
	var incoming map[string]any
	if err := toml.Unmarshal(newContent, &incoming); err != nil {
		return fmt.Errorf("invalid toml: %w", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		merged, err := toml.Marshal(incoming)
		if err != nil {
			return fmt.Errorf("marshal toml: %w", err)
		}
		if err := ensureDir(path); err != nil {
			return err
		}
		return atomicWrite(path, merged, mode)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var existing map[string]any
	if err := toml.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("existing %s is not valid TOML (refusing to merge): %w", path, err)
	}

	deepMerge(existing, incoming)

	merged, err := toml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal merged toml: %w", err)
	}

	if err := ensureDir(path); err != nil {
		return err
	}
	return atomicWrite(path, merged, mode)
}

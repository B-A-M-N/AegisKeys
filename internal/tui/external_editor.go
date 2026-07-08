package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"aegiskeys/internal/config"
)

// openInEditor opens the given text in the user's $EDITOR (falling back to
// micro, nano, vim). The text is written to a secure temp file under the
// config temp dir (0700), edited, read back, and the temp file is removed.
func openInEditor(body string) (string, error) {
	dir := config.TmpPath("")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "scratch-*.md")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = findFallbackEditor()
	}
	if editor == "" {
		return "", fmt.Errorf("no editor found (set $EDITOR)")
	}

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// findFallbackEditor locates a known editor binary on PATH.
func findFallbackEditor() string {
	for _, name := range []string{"micro", "nano", "vim", "vi"} {
		if path, err := exec.LookPath(name); err == nil {
			return filepath.Base(path)
		}
	}
	return ""
}

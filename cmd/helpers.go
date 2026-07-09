package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/huh/v2"
	"golang.org/x/term"

	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

func loadAppConfig() config.Config {
	cfg, err := config.LoadConfig(config.ConfigPath(resolvedConfigDir()))
	if err != nil {
		return config.DefaultConfig()
	}
	return cfg
}

func effectiveProfileName(flagValue string, args ...string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	// Positional fallback: first arg is the profile name.
	for _, a := range args {
		if a != "" {
			return a, nil
		}
	}
	cfg := loadAppConfig()
	if cfg.DefaultProfile != "" {
		return cfg.DefaultProfile, nil
	}
	return "", fmt.Errorf("--profile is required (or set settings.default_profile)")
}

// readPassword prompts on stderr and reads a password from stdin without
// echo. If stdin is not a terminal, it falls back to reading a line (used
// by tests and piped input).
func readPassword(prompt string) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		var sec string
		err := huh.NewInput().
			Title(strings.TrimSpace(strings.TrimSuffix(prompt, ": "))).
			EchoMode(huh.EchoModePassword).
			Value(&sec).
			Run()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return sec, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	// Non-interactive fallback.
	line, err := readLine(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readLine reads a single line from r.
func readLine(r io.Reader) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		if _, err := r.Read(buf); err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		if buf[0] == '\n' {
			break
		}
		sb.WriteByte(buf[0])
	}
	return sb.String(), nil
}

// loadVault prompts for the master password and loads the encrypted vault.
// It returns the unlocked Vault. The returned Vault contains raw secrets;
// callers are responsible for masking before display.
func loadVault() (*secret.Vault, error) {
	pw, err := promptPassword()
	if err != nil {
		return nil, err
	}
	return openVault(pw)
}

// promptPassword reads the master password from the user (no echo).
func promptPassword() (string, error) {
	return readPassword("Master password: ")
}

// openVault loads and decrypts the vault using the given password.
func openVault(password string) (*secret.Vault, error) {
	if err := requireInitialized(); err != nil {
		return nil, err
	}
	if password == "" {
		return nil, fmt.Errorf("master password is required")
	}
	vaultPath := config.VaultPath(resolvedConfigDir())
	if !secret.VaultExists(vaultPath) {
		return nil, fmt.Errorf("no vault found at %s\nRun `aegiskeys init` first", vaultPath)
	}
	return secret.LoadVault(vaultPath, password)
}

// saveVault encrypts and persists the vault with the given password.
func saveVault(password string, v *secret.Vault) error {
	return secret.SaveVault(config.VaultPath(resolvedConfigDir()), password, v)
}

// confirm prompts the user to type an exact token (case-insensitive) before
// proceeding with a risky operation. Returns true if the user confirmed.
func confirm(prompt, token string) bool {
	fmt.Println(prompt)
	fmt.Printf("Type %s to continue: ", token)
	var resp string
	fmt.Scanln(&resp)
	return strings.EqualFold(strings.TrimSpace(resp), token)
}

// confirmPrompt displays a prompt then asks user to type the confirm token.
// Returns (true, nil) if the user typed the token correctly.
func confirmPrompt(prompt, token string) (bool, error) {
	fmt.Print(prompt)
	var resp string
	_, err := fmt.Scanln(&resp)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(resp), token), nil
}

// timeNow wraps time.Now for testability.
func timeNow() time.Time {
	return time.Now()
}

// copyToClipboard copies text to the system clipboard if a tool is available.
// Returns an error if no clipboard tool is found.
func copyToClipboard(text string) error {
	// Try common clipboard tools.
	tools := [][]string{
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
		{"wl-copy"},
	}
	for _, cmd := range tools {
		if _, err := exec.LookPath(cmd[0]); err == nil {
			return execCommand(cmd[0], cmd[1:], text)
		}
	}
	return fmt.Errorf("no clipboard tool found (install xclip, xsel, pbcopy, or wl-copy)")
}

// execCommand runs a command with text piped to stdin.
func execCommand(name string, args []string, stdinText string) error {
	c := exec.Command(name, args...)
	c.Stdin = strings.NewReader(stdinText)
	return c.Run()
}

func clearClipboard() error {
	return copyToClipboard("")
}

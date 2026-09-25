package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aegiskeys/internal/fsutil"
)

type Config struct {
	Version                     int       `json:"version"`
	Initialized                 bool      `json:"initialized"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at,omitzero"`
	AutoLock                    int       `json:"auto_lock_minutes"` // minutes before auto-lock; 0 = disabled
	Theme                       string    `json:"theme"`
	DefaultProfile              string    `json:"default_profile,omitempty"`
	ClipboardTTLSeconds         int       `json:"clipboard_ttl_seconds"`
	AdapterVerifyTimeoutSeconds int       `json:"adapter_verify_timeout_seconds"`
	UnsafeAllowRealHomeVerify   bool      `json:"unsafe_allow_real_home_verify"`
	EnableAnimations            bool      `json:"enable_animations"`
	EnableRiskyExport           bool      `json:"enable_risky_export"`
	RotationReminderDays        int       `json:"rotation_reminder_days"` // days before flagging a key for rotation; 0 = disabled
	RuntimePolicy               string    `json:"runtime_policy"`         // strict (default), standard, permissive
	KeyringEnabled              bool      `json:"keyring_enabled,omitempty"`
	BrokerAutoLockMinutes       int       `json:"broker_auto_lock_minutes"` // absolute broker session lifetime; 0 = never
	// EnableExternalScratchpadEditor defaults to false. External editors may
	// create backup/swap files containing plaintext scratchpad bodies, so the
	// operator must explicitly opt in after reviewing that editor behavior.
	EnableExternalScratchpadEditor bool `json:"enable_external_scratchpad_editor,omitempty"`

	// InheritEnv lists parent environment variable names that are passed
	// through to launched profile apps on top of the strict allowlist. This
	// is the sanctioned escape hatch for session/clipboard variables (e.g.
	// TMUX, DISPLAY, WAYLAND_DISPLAY, XDG_SESSION_TYPE) that the sanitizing
	// allowlist otherwise drops and that a launched app may need. Empty by
	// default, which preserves the strict (minimal-env) launch behavior.
	InheritEnv []string `json:"inherit_env,omitempty"`
}

// Runtime policy levels govern dangerous runtime operations (e.g. writing
// full secrets to a plaintext env file). "strict" forbids them; the other
// levels permit them subject to per-key access policy and confirmation.
const (
	RuntimePolicyStrict     = "strict"
	RuntimePolicyStandard   = "standard"
	RuntimePolicyPermissive = "permissive"
)

// AllowsRiskyExport reports whether the configured runtime policy permits
// risky operations such as materializing secrets to a plaintext env file.
func (c Config) AllowsRiskyExport() bool {
	return c.RuntimePolicy != RuntimePolicyStrict
}

func DefaultConfig() Config {
	return Config{
		Version:                     3,
		Initialized:                 false,
		AutoLock:                    15,
		Theme:                       "vault",
		ClipboardTTLSeconds:         45,
		AdapterVerifyTimeoutSeconds: 20,
		UnsafeAllowRealHomeVerify:   false,
		EnableAnimations:            true,
		EnableRiskyExport:           false,
		RotationReminderDays:        90,
		RuntimePolicy:               RuntimePolicyStrict,
	}
}

func (c Config) WithDefaults() Config {
	d := DefaultConfig()
	originalVersion := c.Version
	if c.Version < d.Version {
		c.Version = d.Version
	}
	if c.Theme == "" || c.Theme == "dark" {
		c.Theme = d.Theme
	}
	if c.AutoLock < 0 {
		c.AutoLock = d.AutoLock
	}
	if originalVersion < 3 && c.BrokerAutoLockMinutes < 0 {
		c.BrokerAutoLockMinutes = d.BrokerAutoLockMinutes
	}
	if originalVersion < 2 && c.ClipboardTTLSeconds == 0 {
		c.ClipboardTTLSeconds = d.ClipboardTTLSeconds
	}
	if originalVersion < 2 && c.AdapterVerifyTimeoutSeconds == 0 {
		c.AdapterVerifyTimeoutSeconds = d.AdapterVerifyTimeoutSeconds
	}
	if c.AdapterVerifyTimeoutSeconds < 1 {
		c.AdapterVerifyTimeoutSeconds = 1
	}
	if c.RuntimePolicy == "" {
		c.RuntimePolicy = RuntimePolicyStrict
	}
	switch c.RuntimePolicy {
	case RuntimePolicyStrict, RuntimePolicyStandard, RuntimePolicyPermissive:
	default:
		c.RuntimePolicy = RuntimePolicyStrict
	}
	if c.RotationReminderDays < 0 {
		c.RotationReminderDays = 0
	}
	return c
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return DefaultConfig(), err
	}
	return c.WithDefaults(), nil
}

func MutateConfigFile(path string, mutate func(*Config) error) error {
	if mutate == nil {
		return fmt.Errorf("nil config mutation")
	}
	if err := fsutil.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	lock, err := fsutil.OpenLockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := fsutil.LockFile(lock); err != nil {
		return err
	}
	defer fsutil.UnlockFile(lock)
	cfg, err := LoadConfig(path)
	if os.IsNotExist(err) {
		cfg = DefaultConfig()
	} else if err != nil {
		return err
	}
	if err := mutate(&cfg); err != nil {
		return err
	}
	return SaveConfig(path, cfg)
}

func SaveConfig(path string, c Config) error {
	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	c = c.WithDefaults()
	data, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(path, data)
}

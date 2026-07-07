package config

import (
	"encoding/json"
	"os"
	"time"

	"aegiskeys/internal/fsutil"
)

type Config struct {
	Version                     int       `json:"version"`
	Initialized                 bool      `json:"initialized"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at,omitempty"`
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
		Version:                     2,
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

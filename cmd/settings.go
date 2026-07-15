package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"aegiskeys/internal/config"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Show and edit AegisKeys preferences",
}

var settingsShowJSON bool

var settingsShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current preferences",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadAppConfig()
		if settingsShowJSON {
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		printSetting("theme", cfg.Theme)
		printSetting("auto_lock_minutes", strconv.Itoa(cfg.AutoLock))
		printSetting("default_profile", emptyDash(cfg.DefaultProfile))
		printSetting("clipboard_ttl_seconds", strconv.Itoa(cfg.ClipboardTTLSeconds))
		printSetting("adapter_verify_timeout_seconds", strconv.Itoa(cfg.AdapterVerifyTimeoutSeconds))
		printSetting("unsafe_allow_real_home_verify", strconv.FormatBool(cfg.UnsafeAllowRealHomeVerify))
		printSetting("rotation_reminder_days", strconv.Itoa(cfg.RotationReminderDays))
		printSetting("runtime_policy", cfg.RuntimePolicy)
		printSetting("inherit_env", strings.Join(cfg.InheritEnv, ","))
		return nil
	},
}

var settingsSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set one preference",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadAppConfig()
		key, value := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		if err := applySetting(&cfg, key, value); err != nil {
			return err
		}
		if err := config.EnsureDir(resolvedConfigDir()); err != nil {
			return err
		}
		if err := config.SaveConfig(config.ConfigPath(resolvedConfigDir()), cfg); err != nil {
			return err
		}
		fmt.Printf("Set %s=%s\n", key, value)
		return nil
	},
}

var settingsResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset preferences to defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.DefaultConfig()
		cfg.Initialized = loadAppConfig().Initialized
		if err := config.EnsureDir(resolvedConfigDir()); err != nil {
			return err
		}
		if err := config.SaveConfig(config.ConfigPath(resolvedConfigDir()), cfg); err != nil {
			return err
		}
		fmt.Println("Settings reset to defaults.")
		return nil
	},
}

func applySetting(cfg *config.Config, key, value string) error {
	switch key {
	case "theme":
		cfg.Theme = value
	case "auto_lock_minutes":
		n, err := parseNonNegativeInt(key, value)
		if err != nil {
			return err
		}
		cfg.AutoLock = n
	case "default_profile":
		cfg.DefaultProfile = value
	case "clipboard_ttl_seconds":
		n, err := parseNonNegativeInt(key, value)
		if err != nil {
			return err
		}
		cfg.ClipboardTTLSeconds = n
	case "adapter_verify_timeout_seconds":
		n, err := parseNonNegativeInt(key, value)
		if err != nil {
			return err
		}
		if n < 1 || n > 300 {
			return fmt.Errorf("%s must be between 1 and 300", key)
		}
		cfg.AdapterVerifyTimeoutSeconds = n
	case "unsafe_allow_real_home_verify":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		cfg.UnsafeAllowRealHomeVerify = b
	case "rotation_reminder_days":
		n, err := parseNonNegativeInt(key, value)
		if err != nil {
			return err
		}
		cfg.RotationReminderDays = n
	case "runtime_policy":
		switch value {
		case config.RuntimePolicyStrict, config.RuntimePolicyStandard, config.RuntimePolicyPermissive:
			cfg.RuntimePolicy = value
		default:
			return fmt.Errorf("%s must be one of: %s, %s, %s",
				key, config.RuntimePolicyStrict, config.RuntimePolicyStandard, config.RuntimePolicyPermissive)
		}
	case "inherit_env":
		// Comma-separated list of parent env var names to pass through to
		// launched apps. An empty value clears the list.
		if value == "" {
			cfg.InheritEnv = nil
			break
		}
		parts := strings.Split(value, ",")
		clean := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			clean = append(clean, p)
		}
		cfg.InheritEnv = clean
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	return nil
}

func parseNonNegativeInt(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return n, nil
}

func printSetting(key, value string) {
	fmt.Printf("%-36s %s\n", key, value)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	settingsShowCmd.Flags().BoolVar(&settingsShowJSON, "json", false, "print settings as JSON")
	settingsCmd.AddCommand(settingsShowCmd, settingsSetCmd, settingsResetCmd)
	rootCmd.AddCommand(settingsCmd)
}

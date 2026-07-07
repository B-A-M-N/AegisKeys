package tui

import (
	"aegiskeys/internal/config"
	"path/filepath"
	"testing"
)

func TestAdjustSetting(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfgPath := filepath.Join(dir, "config.json")
	config.EnsureDir(dir)
	config.SaveConfig(cfgPath, cfg)

	m := &model{
		configDir: dir,
		cfg:       cfg,
	}
	m.selected[screenSettings] = 3 // Clipboard TTL
	m.adjustSetting(1)

	cfg2, _ := config.LoadConfig(cfgPath)
	if cfg2.ClipboardTTLSeconds == cfg.ClipboardTTLSeconds {
		t.Fatalf("did not change! %v", cfg2.ClipboardTTLSeconds)
	}
}

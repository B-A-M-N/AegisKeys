package cmd

import (
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/config"
)

func TestAdapterVerifyDefaultDoesNotRequireInstalledCLI(t *testing.T) {
	prev := adapterVerifyInstalled
	adapterVerifyInstalled = false
	t.Cleanup(func() { adapterVerifyInstalled = prev })

	result := verifyOneAdapter("opencode", adapter.NewRegistry(), config.DefaultConfig())
	if result.status == "FAIL" {
		t.Fatalf("default adapter verification should not require installed opencode CLI: %s", result.detail)
	}
}

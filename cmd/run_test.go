package cmd

import (
	"testing"

	"aegiskeys/internal/adapter"
)

func TestRunCommandOverrideReplacesResolvedTarget(t *testing.T) {
	strategy := &adapter.LaunchStrategy{Plan: adapter.LaunchPlan{
		Command: "free-code",
		Args:    []string{"--configured"},
	}}
	args := []string{"bun", "run", "scripts/cache-probe.ts"}
	extraArgs := applyRunCommandOverride(strategy, args)

	if strategy.Plan.Command != "bun" {
		t.Fatalf("command = %q, want bun", strategy.Plan.Command)
	}
	if len(strategy.Plan.Args) != 0 {
		t.Fatalf("adapter args leaked into override: %v", strategy.Plan.Args)
	}
	if len(extraArgs) != 2 || extraArgs[0] != "run" || extraArgs[1] != "scripts/cache-probe.ts" {
		t.Fatalf("extra args = %v", extraArgs)
	}
}

func TestRunCommandIsNotShadowedByLaunchAlias(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run command: %v", err)
	}
	if command != runCmd {
		t.Fatalf("run resolved to %q, want run command", command.Name())
	}
}

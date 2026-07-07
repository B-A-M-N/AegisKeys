package adapter

import (
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

// TestAdaptersDoNotDoubleAppendProfileArgs verifies that no adapter appends
// p.Args directly. Profile args must only be appended by the resolver.
func TestAdaptersDoNotDoubleAppendProfileArgs(t *testing.T) {
	registry := NewRegistry()
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY",
		BaseURL: "https://api.openai.com/v1", Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	p := profile.Profile{
		Name: "test", ProviderSlug: "openai", KeyID: "key_1",
		Target: profile.TargetConfig{App: "hermes"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
		Args:   []string{"--flag1", "--flag2"},
	}

	strategy, err := ResolveLaunchStrategy(p, prov, key, registry)
	if err != nil {
		t.Fatal(err)
	}
	// Strategy args should include adapter args (chat) + profile args.

	count := 0
	for _, arg := range strategy.Plan.Args {
		if arg == "--flag1" || arg == "--flag2" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected profile args appended once, got %d occurrences in %v", count, strategy.Plan.Args)
	}
}

package profile

import (
	"testing"
)

func TestStoreAddUnique(t *testing.T) {
	s := NewStore()
	if err := s.Add(Profile{Name: "or-main", ProviderSlug: "openrouter", KeyID: "key_1"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(Profile{Name: "or-main", ProviderSlug: "openai", KeyID: "key_2"}); err == nil {
		t.Error("expected duplicate name to error")
	}
	if err := s.Add(Profile{Name: "", ProviderSlug: "x", KeyID: "y"}); err == nil {
		t.Error("expected empty name to error")
	}
}

func TestStoreFind(t *testing.T) {
	s := NewStore()
	s.Add(Profile{Name: "or-main", ProviderSlug: "openrouter", KeyID: "key_1", Aliases: []string{"router"}})
	if p := s.Find("or-main"); p == nil || p.KeyID != "key_1" {
		t.Error("expected to find by name")
	}
	if p := s.Find("router"); p == nil || p.Name != "or-main" {
		t.Error("expected to find by alias")
	}
	if s.Find("missing") != nil {
		t.Error("expected nil for missing profile")
	}
}

func TestStoreRemove(t *testing.T) {
	s := NewStore()
	s.Add(Profile{Name: "a", ProviderSlug: "x", KeyID: "k"})
	s.Add(Profile{Name: "b", ProviderSlug: "x", KeyID: "k"})
	if err := s.Remove("a"); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if len(s.Profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(s.Profiles))
	}
	if err := s.Remove("a"); err == nil {
		t.Error("expected error removing missing profile")
	}
}

func TestAliasCollision(t *testing.T) {
	s := NewStore()
	s.Add(Profile{Name: "or-main", ProviderSlug: "openrouter", KeyID: "k1", Aliases: []string{"router"}})
	// Adding a second profile whose alias collides with the first's alias.
	err := s.Add(Profile{Name: "other", ProviderSlug: "openai", KeyID: "k2", Aliases: []string{"router"}})
	if err == nil {
		t.Error("expected alias collision to error")
	}
	// A profile whose alias collides with an existing *name* should also fail.
	err = s.Add(Profile{Name: "another", ProviderSlug: "openai", KeyID: "k3", Aliases: []string{"or-main"}})
	if err == nil {
		t.Error("expected alias/name collision to error")
	}
}

func TestActiveModelSlots_HermesExpanded(t *testing.T) {
	slots := ActiveModelSlots("hermes")
	expected := []string{"main", "compression", "vision", "web_extract"}
	if len(slots) != len(expected) {
		t.Fatalf("hermes slots: got %v, want %v", slots, expected)
	}
	for i, s := range slots {
		if s != expected[i] {
			t.Errorf("hermes slot %d: got %s, want %s", i, s, expected[i])
		}
	}
}

func TestActiveModelSlots_ZedExpanded(t *testing.T) {
	slots := ActiveModelSlots("zed")
	expected := []string{"main", "inline_assistant", "subagent", "commit_message", "thread_summary", "alternatives"}
	if len(slots) != len(expected) {
		t.Fatalf("zed slots: got %v, want %v", slots, expected)
	}
}

func TestActiveModelSlots_AiderUnchanged(t *testing.T) {
	slots := ActiveModelSlots("aider")
	expected := []string{"main", "weak", "editor"}
	if len(slots) != len(expected) {
		t.Fatalf("aider slots: got %v, want %v", slots, expected)
	}
}

func TestActiveModelSlots_DefaultFallsBackToMain(t *testing.T) {
	slots := ActiveModelSlots("unknown-app")
	if len(slots) != 1 || slots[0] != "main" {
		t.Errorf("default slots: got %v, want [main]", slots)
	}
}

func TestModelSlots_HermesAuxiliaryRoundTrips(t *testing.T) {
	slots := ModelSlots{
		Main:        &ModelRef{ID: "claude-opus-4-5"},
		Compression: &ModelRef{ID: "gemini-3-flash-preview"},
		Vision:      &ModelRef{ID: "gpt-4o"},
		WebExtract:  &ModelRef{ID: "gemini-3-flash-preview"},
	}
	_ = slots.Main
	if slots.Compression == nil || slots.Compression.ID != "gemini-3-flash-preview" {
		t.Error("compression slot not preserved")
	}
	if slots.Vision == nil || slots.Vision.ID != "gpt-4o" {
		t.Error("vision slot not preserved")
	}
	if slots.WebExtract == nil || slots.WebExtract.ID != "gemini-3-flash-preview" {
		t.Error("web_extract slot not preserved")
	}
}

func TestModelSlots_ZedFeatureSlots(t *testing.T) {
	slots := ModelSlots{
		Main:            &ModelRef{ID: "claude-sonnet-4-5"},
		InlineAssistant: &ModelRef{ID: "gpt-4o-mini"},
		CommitMessage:   &ModelRef{ID: "gpt-4o-mini"},
		ThreadSummary:   &ModelRef{ID: "gpt-4o-mini"},
		Subagent:        &ModelRef{ID: "claude-haiku-4"},
		Alternatives:    []ModelRef{{ID: "gemini-3-flash"}},
	}
	_ = slots.Main
	_ = slots.CommitMessage
	_ = slots.ThreadSummary
	_ = slots.Subagent
	if slots.InlineAssistant == nil || slots.InlineAssistant.ID != "gpt-4o-mini" {
		t.Error("inline_assistant slot not preserved")
	}
	if len(slots.Alternatives) != 1 || slots.Alternatives[0].ID != "gemini-3-flash" {
		t.Error("alternatives slot not preserved")
	}
}

func TestModelSlots_CustomSlotMap(t *testing.T) {
	slots := ModelSlots{
		Main: &ModelRef{ID: "gpt-4o"},
		Custom: map[string]ModelRef{
			"my_slot": {ID: "custom-model"},
		},
	}
	_ = slots.Main
	if len(slots.Custom) != 1 || slots.Custom["my_slot"].ID != "custom-model" {
		t.Error("custom slot not preserved")
	}
}

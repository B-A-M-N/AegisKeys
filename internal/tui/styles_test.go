package tui

import (
	"reflect"
	"testing"
)

func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) == 0 {
		t.Fatal("ThemeNames returned no themes")
	}
	if names[0] != "vault" {
		t.Errorf("default theme should be first, got %q", names[0])
	}
	// Must contain the documented set.
	want := []string{"vault", "light", "matrix", "ice", "ember", "mono"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("ThemeNames = %v, want %v", names, want)
	}
}

func TestNewStylesAllThemes(t *testing.T) {
	for _, name := range ThemeNames() {
		s := NewStyles(name)
		if s == nil {
			t.Fatalf("NewStyles(%q) returned nil", name)
		}
		if s.ThemeName != name {
			t.Errorf("NewStyles(%q).ThemeName = %q", name, s.ThemeName)
		}
		// Styles must be usable: rendering must not panic and must echo text.
		if got := s.Panel.Render("x"); got == "" {
			t.Errorf("theme %q Panel.Render produced empty output", name)
		}
	}
}

func TestNormalizeTheme(t *testing.T) {
	cases := map[string]string{
		"":       "vault",
		"dark":   "vault",
		"light":  "light",
		"matrix": "matrix",
		"ICE":    "vault", // case-sensitive; unknown => default
		"bogus":  "vault",
	}
	for in, want := range cases {
		if got := normalizeTheme(in); got != want {
			t.Errorf("normalizeTheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCycleTheme(t *testing.T) {
	names := ThemeNames()
	// Cycling the last theme wraps to the first.
	last := names[len(names)-1]
	if got := cycleTheme(last); got != names[0] {
		t.Errorf("cycleTheme(%q) = %q, want wrap to %q", last, got, names[0])
	}
	// Cycling an unknown theme lands on the first.
	if got := cycleTheme("nope"); got != names[0] {
		t.Errorf("cycleTheme(unknown) = %q, want %q", got, names[0])
	}
}

// noopStyle is the zero value of lipgloss.Style, used above to detect an
// uninitialized style field.
type noopStyle = struct{}

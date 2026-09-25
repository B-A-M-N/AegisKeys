package tui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

func composedModel(t *testing.T, w, h int) *model {
	t.Helper()
	m := newTestModel(t)
	m.unlocked = true
	m.cfg.EnableAnimations = true
	m.vaultSession = &vaultSession{}
	m.unlockSessionContext()
	m.width, m.height = w, h
	m.matrix.Resize(w, h)
	m.matrix.Frame = 100
	return m
}
func TestComposedFramesAllPrimaryScreensAndSizes(t *testing.T) {
	for _, dim := range [][2]int{{80, 24}, {120, 40}, {180, 60}} {
		for s := screenDashboard; s < screenSentinel; s++ {
			m := composedModel(t, dim[0], dim[1])
			m.active = s
			m.focus = focusContent
			out := stripANSIForTest(m.View().Content)
			if out == "" || strings.Contains(out, "Terminal too small") {
				t.Fatalf("screen %d size %v missing frame", s, dim)
			}
			m.modal = modalDetail
			modal := stripANSIForTest(m.View().Content)
			if modal == "" {
				t.Fatalf("screen %d size %v missing modal frame", s, dim)
			}
		}
	}
}
func TestComposedFrameSurvivesResizeAndFocusLoss(t *testing.T) {
	m := composedModel(t, 80, 24)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.FocusMsg{})
	m.Update(tea.BlurMsg{})
	m.Update(tea.FocusMsg{})
	if m.width != 120 || m.height != 40 || m.View().Content == "" {
		t.Fatal("resize/focus lifecycle broke composed frame")
	}
}
func TestExplicitAppPinsLogoAndEmptyContextResumesRotation(t *testing.T) {
	m := composedModel(t, 120, 40)
	m.active = screenProfiles
	m.selected[screenProfiles] = 0
	m.View()
	if !m.matrix.LogoPinned() {
		t.Fatal("explicit profile app did not pin logo")
	}
	m.active = screenDoctor
	m.View()
	if m.matrix.LogoPinned() {
		t.Fatal("automatic screen did not resume logo rotation")
	}
}
func BenchmarkComposedFrame120x40(b *testing.B) {
	m := &model{styles: NewStyles("vault"), providers: provider.NewRegistry(), profiles: profile.NewStore(), width: 120, height: 40, matrix: NewMatrix(120, 40), adapterRegistry: adapter.NewRegistry(), unlocked: true, vaultSession: &vaultSession{}}
	m.cfg.EnableAnimations = true
	m.unlockSessionContext()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.matrix.Frame++
		_ = m.View().Content
	}
}

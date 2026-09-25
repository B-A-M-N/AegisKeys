package tui

import (
	"aegiskeys/internal/adapter"
	"aegiskeys/internal/appcatalog"
	"aegiskeys/internal/logo"
	"testing"
)

func TestEveryCatalogAppHasAdapterAndVisualFallback(t *testing.T) {
	reg := adapter.NewRegistry()
	for _, id := range appcatalog.All() {
		if _, ok := reg.Get(id); !ok {
			t.Fatalf("catalog app %q missing adapter", id)
		}
		if !logo.DefaultAssetAvailable(id) {
			t.Fatalf("catalog app %q missing logo fallback", id)
		}
	}
}

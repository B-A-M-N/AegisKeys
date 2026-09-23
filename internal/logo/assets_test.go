package logo

import (
	"os"
	"testing"
)

func TestDefaultAssetsAreEmbeddedAndRegistered(t *testing.T) {
	if err := ValidateDefaultAssets(); err != nil {
		t.Fatal(err)
	}
	for id := range DefaultAssets {
		if !DefaultAssetAvailable(id) {
			t.Fatalf("registered asset unavailable: %s", id)
		}
		if _, ok := LoadDefaultMask(id); !ok {
			t.Fatalf("mask decode failed: %s", id)
		}
	}
}

func TestEmbeddedAssetsWorkOutsideRepository(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if _, ok := LoadDefaultMask("claude"); !ok {
		t.Fatal("embedded logo unavailable outside repository")
	}
}

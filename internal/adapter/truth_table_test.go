package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type adapterProof struct {
	Adapter       string `json:"adapter"`
	VerifiedGates struct {
		RenderGolden bool `json:"render_golden"`
		NoSecretLeak bool `json:"no_secret_leak"`
		ConfigMerge  bool `json:"config_merge"`
		LaunchSmoke  bool `json:"launch_smoke"`
	} `json:"verified_gates"`
}

func TestAdapterTruthTableVerifiedAdapters(t *testing.T) {
	want := map[string]SupportConfidence{
		"generic": ConfidenceVerified,
		"crush":   ConfidenceVerified,
		"aider":   ConfidenceVerified,
		"qwen":    ConfidenceVerified,
		"claude":  ConfidenceVerified,
		"goose":   ConfidenceVerified,
	}

	reg := NewRegistry()
	for id, confidence := range want {
		a, ok := reg.Get(id)
		if !ok {
			t.Fatalf("missing adapter %q", id)
		}
		c := a.Contract()
		if c.SupportConfidence != confidence {
			t.Fatalf("adapter %q confidence = %q, want %q", id, c.SupportConfidence, confidence)
		}
		if !c.Verification.Verified() {
			t.Fatalf("adapter %q should have all verification gates true", id)
		}
		if _, err := readAdapterProof(id); err != nil {
			t.Fatalf("verified adapter %q has no readable proof: %v", id, err)
		}
	}
}

func TestProofFilesMatchVerifiedGates(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "adapter_proofs", "*.proof.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected manual proof files")
	}
	for _, file := range files {
		var p adapterProof
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		if p.Adapter == "" {
			t.Fatalf("%s: missing adapter", file)
		}
		if !p.VerifiedGates.RenderGolden ||
			!p.VerifiedGates.NoSecretLeak ||
			!p.VerifiedGates.ConfigMerge ||
			!p.VerifiedGates.LaunchSmoke {
			t.Fatalf("%s: verified proof must record all automated gates", file)
		}
	}
}

func readAdapterProof(id string) (adapterProof, error) {
	var p adapterProof
	pattern := filepath.Join("..", "..", "testdata", "adapter_proofs", id+".*.proof.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return p, err
	}
	if len(files) == 0 {
		return p, os.ErrNotExist
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(data, &p)
}

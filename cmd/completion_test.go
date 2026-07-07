package cmd

import "testing"

func TestCompletionCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"completion", "bash"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil || cmd.Name() != "completion" {
		t.Fatalf("completion command not registered: %#v", cmd)
	}
}

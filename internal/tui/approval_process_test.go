package tui

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegiskeys/internal/broker"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

func TestApprovalCrashRecoveryProcess(t *testing.T) {
	points := []string{"after-stage", "after-policy", "after-activate"}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			vaultPath := config.VaultPath(dir)
			if err := secret.InitVault(vaultPath, "pw"); err != nil {
				t.Fatal(err)
			}
			_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
			if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
				return v.Add(secret.SecretRecord{ID: "k", Label: "Key", Secret: "s", Policy: secret.SecretPolicy{Version: 1}})
			}); err != nil {
				t.Fatal(err)
			}
			meta := broker.NewFile()
			meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "b", Name: "app/key", SecretID: "k"})
			if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
				t.Fatal(err)
			}
			exe := filepath.Join(dir, "app")
			if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0700); err != nil {
				t.Fatal(err)
			}
			runApprovalCrashChild(t, dir, hex.EncodeToString(key[:]), exe, point)
			state := runApprovalRecoveryChild(t, dir, hex.EncodeToString(key[:]))
			v, _ := secret.LoadVaultByKey(vaultPath, key)
			got, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
			if state != "ok" || len(got.PendingApprovals) != 0 {
				t.Fatalf("recovery state=%s meta=%+v", state, got)
			}
			switch point {
			case "after-stage", "after-policy":
				if len(got.Grants) != 0 || v.Get("k").Policy.AllowBrokerResolve {
					t.Fatalf("%s did not roll back: grants=%+v policy=%+v", point, got.Grants, v.Get("k").Policy)
				}
			case "after-activate":
				if len(got.Grants) != 1 || !got.Grants[0].Enabled || !v.Get("k").Policy.AllowBrokerResolve {
					t.Fatalf("activation not finalized: grants=%+v policy=%+v", got.Grants, v.Get("k").Policy)
				}
			}
		})
	}
}

func approvalChild(t *testing.T) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=TestApprovalCrashChildProcess")
	cmd.Env = append(os.Environ(), "AEGISKEYS_APPROVAL_CRASH_CHILD=1")
	return cmd
}

func runApprovalCrashChild(t *testing.T, dir, keyHex, exe, point string) {
	cmd := approvalChild(t)
	cmd.Env = append(cmd.Env, "AEGISKEYS_APPROVAL_DIR="+dir, "AEGISKEYS_APPROVAL_KEY="+keyHex, "AEGISKEYS_APPROVAL_EXE="+exe, "AEGISKEYS_APPROVAL_POINT="+point)
	if err := cmd.Run(); err == nil {
		t.Fatal("crash child exited successfully")
	}
}

func runApprovalRecoveryChild(t *testing.T, dir, keyHex string) string {
	cmd := exec.Command(os.Args[0], "-test.run=TestApprovalRecoveryChildProcess")
	cmd.Env = append(os.Environ(), "AEGISKEYS_APPROVAL_RECOVERY_CHILD=1", "AEGISKEYS_APPROVAL_DIR="+dir, "AEGISKEYS_APPROVAL_KEY="+keyHex)
	out, err := cmd.CombinedOutput()
	idx := strings.Index(string(out), "ok")
	if err != nil || idx < 0 {
		t.Fatalf("recovery child: %v: %s", err, out)
	}
	return "ok"
}

func TestApprovalCrashChildProcess(t *testing.T) {
	if os.Getenv("AEGISKEYS_APPROVAL_CRASH_CHILD") == "" {
		t.Skip("helper")
	}
	point := os.Getenv("AEGISKEYS_APPROVAL_POINT")
	approvalCrashPoint = func(name string) {
		if name == point {
			os.Exit(23)
		}
	}
	dir := os.Getenv("AEGISKEYS_APPROVAL_DIR")
	raw, _ := hex.DecodeString(os.Getenv("AEGISKEYS_APPROVAL_KEY"))
	var key [32]byte
	copy(key[:], raw)
	prepared, ok := prepareAccessApprovalCmd(dir, "b", key, os.Getenv("AEGISKEYS_APPROVAL_EXE"), "resolve", time.Now().Add(time.Hour).Format(time.RFC3339))().(accessApprovalPreparedMsg)
	if !ok {
		t.Fatal("prepare failed")
	}
	if point == "after-stage" {
		os.Exit(23)
	}
	commitStagedAccessCmd(dir, prepared.pending)()
	os.Exit(0)
}

func TestApprovalRecoveryChildProcess(t *testing.T) {
	if os.Getenv("AEGISKEYS_APPROVAL_RECOVERY_CHILD") == "" {
		t.Skip("helper")
	}
	dir := os.Getenv("AEGISKEYS_APPROVAL_DIR")
	raw, _ := hex.DecodeString(os.Getenv("AEGISKEYS_APPROVAL_KEY"))
	var key [32]byte
	copy(key[:], raw)
	msg := recoverPendingApprovalsCmd(dir, key)().(accessMutationDoneMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	fmt.Print("ok")
}

package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aegiskeys/internal/config"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
)

func TestExternalEditorUsesConfiguredPrivateRuntimeAndArguments(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.RuntimePath(dir), 0700); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(dir, "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\n[ \"$1\" = --wait ] && shift\nprintf 'changed' > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor+" --wait")
	msg := prepareExternalEditor(dir, "secret body", 1, "note")().(externalEditorPreparedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if !strings.HasPrefix(msg.path, config.RuntimePath(dir)+string(os.PathSeparator)) {
		t.Fatalf("unsafe editor path %s", msg.path)
	}
	if len(msg.command) != 3 || msg.command[1] != "--wait" || msg.command[2] != msg.path {
		t.Fatalf("bad editor argv %#v", msg.command)
	}
	if err := os.WriteFile(msg.path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupExternalEditor(filepath.Dir(msg.path)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(msg.path); !os.IsNotExist(err) {
		t.Fatalf("plaintext editor file remains: %v", err)
	}
}

func TestExternalEditorCleansInterruptedPrivateDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.RuntimePath(dir), 0700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(config.RuntimePath(dir), "scratch-editor-stale")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "scratchpad.md"), []byte("plaintext"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupAbandonedEditorDirs(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale plaintext survived: %v", err)
	}
}

func TestExternalEditorRejectsPermissiveRuntimeDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	runtime := config.RuntimePath(dir)
	if err := os.MkdirAll(runtime, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runtime, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "/bin/true")
	if msg := prepareExternalEditor(dir, "secret", 1, "note")().(externalEditorPreparedMsg); msg.err == nil {
		t.Fatal("permissive runtime directory was accepted")
	}
}

func TestExternalEditorFailedPreparationLeavesNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "/definitely/missing/editor")
	msg := prepareExternalEditor(dir, "top-secret", 1, "note")().(externalEditorPreparedMsg)
	if msg.err == nil {
		t.Fatal("missing editor accepted")
	}
	entries, _ := os.ReadDir(config.RuntimePath(dir))
	if len(entries) != 0 {
		t.Fatalf("failed preparation left files: %v", entries)
	}
}

func TestExternalEditorFailureCallbackRemovesResidualDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(dir, "failing-editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	prepared := prepareExternalEditor(dir, "plaintext", 1, "note")().(externalEditorPreparedMsg)
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	msg := finishExternalEditor(prepared, 1, "note", exec.Command(prepared.command[0], prepared.command[1:]...).Run())
	if msg.err == nil {
		t.Fatal("failing editor returned success")
	}
	if _, err := os.Stat(filepath.Dir(prepared.path)); !os.IsNotExist(err) {
		t.Fatalf("failed editor left residual directory: %v", err)
	}
}

func TestStalePreparedEditorIsDeletedWithoutLaunch(t *testing.T) {
	m := newTestModel(t)
	m.scratchBodyInput = textarea.New()
	m.scratchBodyInput.SetWidth(40)
	m.scratchBodyInput.SetHeight(8)
	m.scratchTitleInput = textinput.New()
	m.cfg.EnableExternalScratchpadEditor = true
	m.unlocked = true
	m.vaultSession = &vaultSession{}
	m.unlockSessionContext()
	oldGen := m.sessionGen
	dir := t.TempDir()
	if err := config.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "/bin/true")
	m.scratchEditingID = "note"
	cmd := prepareExternalEditor(dir, "old-session-plaintext", oldGen, "note")
	prepared := cmd().(externalEditorPreparedMsg)
	if prepared.err != nil {
		t.Fatal(prepared.err)
	}
	if _, err := os.Stat(prepared.path); err != nil {
		t.Fatal(err)
	}
	m.lockVault()
	m.unlocked = true
	m.vaultSession = &vaultSession{}
	m.unlockSessionContext()
	m.scratchEditingID = "new-note"
	_, next := m.Update(prepared)
	if next != nil {
		t.Fatal("stale editor preparation launched a command")
	}
	if _, err := os.Stat(prepared.path); !os.IsNotExist(err) {
		t.Fatalf("stale plaintext survived: %v", err)
	}
}

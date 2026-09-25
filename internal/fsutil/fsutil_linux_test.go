//go:build linux

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFileRejectsSymlinkParent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(filepath.Join(link, "data"), []byte("x")); err == nil {
		t.Fatal("write through symlink parent accepted")
	}
}

func TestAtomicWriteFileReplacesTargetSymlinkWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFileMode(target, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("target=%q err=%v", got, err)
	}
	outsideGot, err := os.ReadFile(outside)
	if err != nil || string(outsideGot) != "outside" {
		t.Fatalf("outside=%q err=%v", outsideGot, err)
	}
}

func TestEnsureDirRejectsSymlinkComponent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(filepath.Join(link, "nested")); err == nil {
		t.Fatal("EnsureDir followed symlink component")
	}
}

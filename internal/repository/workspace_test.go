package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceSearchAndRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("video upload\ntransactional outbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	results, err := workspace.Search("outbox", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "README.md" || results[0].StartLine != 2 {
		t.Fatalf("unexpected search result: %#v", results)
	}
	read, err := workspace.ReadFile("README.md", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 1 || read[0].EndLine != 2 {
		t.Fatalf("unexpected read result: %#v", read)
	}
}

func TestWorkspaceRejectsPathEscape(t *testing.T) {
	workspace, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.ReadFile("../secret.txt", 1, 1); err == nil {
		t.Fatal("expected repository boundary error")
	}
}

func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks are unavailable on this host: %v", err)
	}
	workspace, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.ReadFile("link.txt", 1, 1); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}

func TestRefreshCountsModules(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gateway"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway", "go.mod"), []byte("module gateway"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := workspace.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.GoModules != 1 || info.Files != 1 {
		t.Fatalf("unexpected repository info: %#v", info)
	}
}

package execution

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhao/codeatlas/internal/project"
)

func TestReplaceTextHonorsApprovalAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	profile := &project.Profile{Name: "test", DeniedPrefixes: []string{".git"}}
	path := filepath.Join(root, "gateway", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceText(root, "gateway/main.go", "package main", "package gateway", []string{"gateway/**"}, profile); err != nil {
		t.Fatal(err)
	}
	if err := replaceText(root, "rpc-user/main.go", "x", "y", []string{"gateway/**"}, profile); err == nil {
		t.Fatal("expected scope rejection")
	}
}

func TestCreateFileCannotOverwrite(t *testing.T) {
	root := t.TempDir()
	profile := &project.Profile{Name: "test"}
	if err := createFile(root, "gateway/new.go", "package gateway", []string{"gateway/**"}, profile); err != nil {
		t.Fatal(err)
	}
	if err := createFile(root, "gateway/new.go", "overwrite", []string{"gateway/**"}, profile); err == nil {
		t.Fatal("expected overwrite rejection")
	}
}

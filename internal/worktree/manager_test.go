package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareDiffAndDiscard(t *testing.T) {
	source := t.TempDir()
	runGit(t, source, "init")
	runGit(t, source, "config", "user.email", "test@example.com")
	runGit(t, source, "config", "user.name", "CodeAtlas Test")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "initial")
	revision := strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD"))
	manager, err := NewManager(source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := manager.Prepare(context.Background(), "exec-test", revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Path, "README.md"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff, files, err := manager.Diff(context.Background(), workspace, revision)
	if err != nil || len(files) != 1 || files[0] != "README.md" || !strings.Contains(diff, "+after") {
		t.Fatalf("unexpected diff: %v %#v %s", err, files, diff)
	}
	if err := manager.Discard(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

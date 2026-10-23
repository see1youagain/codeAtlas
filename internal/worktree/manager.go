package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type Workspace struct {
	Path   string
	Branch string
	Mirror string
}

type Manager struct {
	sourceRoot  string
	runtimeRoot string
	mu          sync.Mutex
}

func NewManager(sourceRoot, runtimeRoot string) (*Manager, error) {
	source, err := filepath.Abs(sourceRoot)
	if err != nil {
		return nil, err
	}
	runtime, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(runtime, "mirrors"), 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(runtime, "worktrees"), 0o700); err != nil {
		return nil, err
	}
	return &Manager{sourceRoot: source, runtimeRoot: runtime}, nil
}

func (m *Manager) Prepare(ctx context.Context, executionID, revision string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mirror := filepath.Join(m.runtimeRoot, "mirrors", m.sourceKey()+".git")
	if _, err := os.Stat(mirror); os.IsNotExist(err) {
		if output, err := command(ctx, "", "git", "clone", "--mirror", "--no-hardlinks", m.sourceRoot, mirror); err != nil {
			return nil, fmt.Errorf("create repository mirror: %w: %s", err, output)
		}
	} else {
		if output, err := command(ctx, "", "git", "--git-dir", mirror, "fetch", "--prune", "origin"); err != nil {
			return nil, fmt.Errorf("refresh repository mirror: %w: %s", err, output)
		}
	}
	branch := "codeatlas/" + sanitize(executionID)
	path := filepath.Join(m.runtimeRoot, "worktrees", sanitize(executionID))
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("worktree already exists: %s", path)
	}
	output, err := command(ctx, "", "git", "--git-dir", mirror, "worktree", "add", "-b", branch, path, revision)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w: %s", err, output)
	}
	return &Workspace{Path: path, Branch: branch, Mirror: mirror}, nil
}

func (m *Manager) Discard(ctx context.Context, workspace *Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if workspace == nil || !m.withinRuntime(workspace.Path) || !m.withinRuntime(workspace.Mirror) {
		return fmt.Errorf("refuse to discard workspace outside runtime root")
	}
	output, err := command(ctx, "", "git", "--git-dir", workspace.Mirror, "worktree", "remove", "--force", workspace.Path)
	if err != nil {
		return fmt.Errorf("remove worktree: %w: %s", err, output)
	}
	_, _ = command(ctx, "", "git", "--git-dir", workspace.Mirror, "branch", "-D", workspace.Branch)
	return nil
}

func (m *Manager) Diff(ctx context.Context, workspace *Workspace, revision string) (string, []string, error) {
	// Only the isolated index changes. Intent-to-add exposes new files to diff
	// without creating a commit or staging their content as an approved change.
	untracked, err := commandStdout(ctx, workspace.Path, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", nil, fmt.Errorf("list new files: %w", err)
	}
	for _, path := range strings.Split(untracked, "\x00") {
		if path == "" {
			continue
		}
		if _, err := commandStdout(ctx, workspace.Path, "git", "add", "--intent-to-add", "--", path); err != nil {
			return "", nil, fmt.Errorf("include new file in diff: %w", err)
		}
	}
	diff, err := commandStdout(ctx, workspace.Path, "git", "diff", "--no-ext-diff", "--unified=3", revision, "--")
	if err != nil {
		return "", nil, fmt.Errorf("read diff: %w", err)
	}
	nameOutput, err := commandStdout(ctx, workspace.Path, "git", "diff", "--name-only", "-z", revision, "--")
	if err != nil {
		return "", nil, fmt.Errorf("read changed files: %w", err)
	}
	files := make([]string, 0)
	for _, line := range strings.Split(nameOutput, "\x00") {
		if line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	if len(diff) > 1<<20 {
		diff = diff[:1<<20] + "\n... diff truncated ..."
	}
	return diff, files, nil
}

func (m *Manager) sourceKey() string {
	sum := sha256.Sum256([]byte(strings.ToLower(m.sourceRoot)))
	return hex.EncodeToString(sum[:8])
}

func (m *Manager) withinRuntime(path string) bool {
	rel, err := filepath.Rel(m.runtimeRoot, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var unsafeBranchChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitize(value string) string { return unsafeBranchChars.ReplaceAllString(value, "-") }

func command(ctx context.Context, directory, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if directory != "" {
		cmd.Dir = directory
	}
	output, err := cmd.CombinedOutput()
	if len(output) > 1<<20 {
		output = output[:1<<20]
	}
	return string(output), err
}

func commandStdout(ctx context.Context, directory, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if directory != "" {
		cmd.Dir = directory
	}
	output, err := cmd.Output()
	if len(output) > 1<<20 {
		output = output[:1<<20]
	}
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return string(output), fmt.Errorf("%w: %s", err, string(exitErr.Stderr))
	}
	return string(output), err
}

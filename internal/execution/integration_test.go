package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/store"
	"github.com/lizhao/codeatlas/internal/worktree"
)

type fakeDeveloper struct {
	responses []string
	index     int
}

func (f *fakeDeveloper) Complete(context.Context, string, string) (string, error) {
	response := f.responses[f.index]
	f.index++
	return response, nil
}

func TestExecutorRunsBaselineEditsTestsAndReviews(t *testing.T) {
	source := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/calc\n\ngo 1.22\n")
	write("calc.go", "package calc\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	write("calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad sum\") } }\n")
	runGit(t, source, "init")
	runGit(t, source, "config", "user.email", "test@example.com")
	runGit(t, source, "config", "user.name", "CodeAtlas Test")
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-m", "initial")
	revision := strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD"))

	profile, err := project.Detect(source)
	if err != nil {
		t.Fatal(err)
	}
	persistence, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer persistence.Close()
	manager, err := worktree.NewManager(source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeDeveloper{responses: []string{
		`{"thought":"make the approved refactor","action":{"name":"replace_text","input":{"path":"calc.go","old":"return a + b","new":"sum := a + b\n\treturn sum"}}}`,
		`{"thought":"verify independently","action":{"name":"run_tests","input":{}}}`,
		`{"thought":"tests pass","final":{"summary":"refactored Add without changing behavior"}}`,
	}}
	executor := New(client, profile, manager, persistence, 5)
	now := time.Now().UTC()
	plan := &model.ChangePlan{ID: "plan-e2e", Requirement: "refactor Add", Summary: "refactor", BaseRevision: revision, Status: model.PlanApproved, CreatedAt: now, UpdatedAt: now}
	approval := &model.Approval{ID: "approval-e2e", PlanID: plan.ID, AllowedPaths: []string{"calc.go"}, TestSuiteIDs: []string{"go-test-root"}, CreatedAt: now}
	if err := persistence.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), plan, approval)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != "calc.go" || !strings.Contains(result.Diff, "+\tsum := a + b") {
		t.Fatalf("unexpected execution: %#v\n%s", result, result.Diff)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("QA gate did not pass: %#v", result.Tests)
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

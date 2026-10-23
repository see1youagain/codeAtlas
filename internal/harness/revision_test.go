package harness

import (
	"context"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestApprovalRejectsLiveRevisionChangeWithoutMetadataRefresh(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("init")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "first")
	w, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := New(nil, nil, nil, w, p, s, true)
	now := time.Now().UTC()
	plan := &model.ChangePlan{ID: "revision-plan", Status: model.PlanPending, Digest: "digest", BaseRevision: w.Info().Revision, CandidatePaths: []string{"main.go"}, TestSuiteIDs: []string{"go-test-root"}, CreatedAt: now, UpdatedAt: now}
	if err := s.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "second")
	if w.Info().Revision != plan.BaseRevision {
		t.Fatal("fixture unexpectedly refreshed metadata")
	}
	if _, err := h.Approve(context.Background(), plan.ID, model.ApprovalRequest{PlanDigest: plan.Digest, BaseRevision: plan.BaseRevision, AllowedPaths: []string{"main.go"}, TestSuiteIDs: []string{"go-test-root"}}); err == nil {
		t.Fatal("stale revision approved")
	}
}

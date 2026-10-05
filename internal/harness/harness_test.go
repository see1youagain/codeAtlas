package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/store"
)

func TestApprovalIsBoundToDigestRevisionAndNarrowScope(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gateway"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway", "go.mod"), []byte("module gateway\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, _ := repository.New(root)
	profile, _ := project.Detect(root)
	persistence, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer persistence.Close()
	h := New(nil, nil, nil, workspace, profile, persistence, true)
	now := time.Now().UTC()
	plan := &model.ChangePlan{ID: "plan-1", BaseRevision: workspace.Info().Revision, Digest: "digest-1", CandidatePaths: []string{"gateway/**"}, TestSuiteIDs: []string{"go-test-gateway"}, Status: model.PlanPending, CreatedAt: now, UpdatedAt: now}
	if err := persistence.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	response, err := h.Approve(context.Background(), plan.ID, model.ApprovalRequest{PlanDigest: plan.Digest, BaseRevision: plan.BaseRevision, AllowedPaths: []string{"gateway/main.go"}, TestSuiteIDs: []string{"go-test-gateway"}})
	if err != nil || response.Token == "" {
		t.Fatalf("approval failed: %#v %v", response, err)
	}
}

func TestApprovalRejectsExpandedScope(t *testing.T) {
	plan := &model.ChangePlan{CandidatePaths: []string{"gateway/**"}, TestSuiteIDs: []string{"go-test-gateway"}}
	profile := &project.Profile{TestSuites: []model.TestSuite{{ID: "go-test-gateway"}}}
	err := validateApprovalScope(plan, &model.ApprovalRequest{AllowedPaths: []string{"rpc-user/**"}, TestSuiteIDs: []string{"go-test-gateway"}}, profile)
	if err == nil {
		t.Fatal("expected expanded scope to be rejected")
	}
}

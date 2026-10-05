package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
)

func TestPlanApprovalAndExecutionRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	plan := &model.ChangePlan{ID: "plan-1", Status: model.PlanPending, Summary: "test", CreatedAt: now, UpdatedAt: now}
	if err := s.SavePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetPlan(ctx, plan.ID)
	if err != nil || loaded.Summary != plan.Summary {
		t.Fatalf("unexpected plan: %#v, %v", loaded, err)
	}
	approval := &model.Approval{ID: "approval-1", PlanID: plan.ID, TokenHash: "hash", CreatedAt: now}
	if err := s.SaveApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	if loadedApproval, err := s.GetApprovalByPlan(ctx, plan.ID); err != nil || loadedApproval.TokenHash != "hash" {
		t.Fatalf("unexpected approval: %#v, %v", loadedApproval, err)
	}
	execution := &model.Execution{ID: "exec-1", PlanID: plan.ID, Status: "completed", StartedAt: now}
	if err := s.SaveExecution(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if loadedExecution, err := s.GetExecution(ctx, execution.ID); err != nil || loadedExecution.Status != "completed" {
		t.Fatalf("unexpected execution: %#v, %v", loadedExecution, err)
	}
}

func TestRecoverInterruptedDoesNotReplayWrites(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	plan := &model.ChangePlan{ID: "plan-running", Status: model.PlanRunning, CreatedAt: now, UpdatedAt: now}
	if err := s.SavePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	execution := &model.Execution{ID: "exec-running", PlanID: plan.ID, Status: "editing", StartedAt: now}
	if err := s.SaveExecution(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	loadedPlan, _ := s.GetPlan(ctx, plan.ID)
	loadedExecution, _ := s.GetExecution(ctx, execution.ID)
	if loadedPlan.Status != model.PlanFailed || loadedExecution.Status != "failed" || loadedExecution.Error == "" {
		t.Fatalf("unexpected recovery: %#v %#v", loadedPlan, loadedExecution)
	}
}

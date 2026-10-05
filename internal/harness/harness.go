package harness

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lizhao/codeatlas/internal/agent"
	"github.com/lizhao/codeatlas/internal/execution"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/security"
	"github.com/lizhao/codeatlas/internal/store"
)

type Role struct {
	Name           string   `json:"name"`
	Responsibility string   `json:"responsibility"`
	Capabilities   []string `json:"capabilities"`
}

type Harness struct {
	researcher *agent.ReAct
	planner    *agent.Planner
	executor   *execution.Executor
	workspace  *repository.Workspace
	profile    *project.Profile
	store      *store.Store
	timeout    time.Duration
	write      bool
	mu         sync.Mutex
}

func New(researcher *agent.ReAct, planner *agent.Planner, executor *execution.Executor, workspace *repository.Workspace, profile *project.Profile, persistence *store.Store, writeEnabled bool) *Harness {
	return &Harness{researcher: researcher, planner: planner, executor: executor, workspace: workspace, profile: profile, store: persistence, timeout: 75 * time.Second, write: writeEnabled}
}

func (h *Harness) Roles() []Role {
	developerCapabilities := []string{"disabled until ENABLE_WRITE and approval"}
	if h.write {
		developerCapabilities = []string{"isolated worktree", "approved path edits", "approved test suites"}
	}
	return []Role{
		{Name: "researcher", Responsibility: "ReAct repository investigation", Capabilities: []string{"repo_summary", "list_files", "search_code", "read_file"}},
		{Name: "architect", Responsibility: "grounded change planning", Capabilities: []string{"research report", "structured plan"}},
		{Name: "developer", Responsibility: "execute only an approved plan", Capabilities: developerCapabilities},
		{Name: "qa", Responsibility: "run profile-owned baseline and post-change suites", Capabilities: []string{"pre-registered commands only"}},
		{Name: "reviewer", Responsibility: "deterministic citation, scope, test and diff gates", Capabilities: []string{"read-only verification"}},
	}
}

func (h *Harness) Profile() *project.Profile { return h.profile }

func (h *Harness) Ask(ctx context.Context, runID, question string) (model.AskResponse, error) {
	if err := validateInput(question); err != nil {
		return model.AskResponse{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	response, err := h.researcher.Run(runCtx, runID, question)
	if err == nil {
		h.persistTrace(ctx, runID, response.Trace)
	}
	return response, err
}

func (h *Harness) Plan(ctx context.Context, runID, requirement string) (model.PlanResponse, error) {
	if err := validateInput(requirement); err != nil {
		return model.PlanResponse{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*h.timeout)
	defer cancel()
	investigationQuestion := "请只读调查下面修改需求的当前实现、可能影响文件和调用链，为架构师提供证据，不要修改代码：\n" + requirement
	investigation, err := h.researcher.Run(runCtx, runID+"-research", investigationQuestion)
	if err != nil {
		return model.PlanResponse{}, fmt.Errorf("research phase failed: %w", err)
	}
	response, err := h.planner.Create(runCtx, runID, requirement, investigation.Answer, investigation.Citations)
	if err != nil {
		return model.PlanResponse{}, err
	}
	response.Trace = append(investigation.Trace, response.Trace...)
	if err := h.store.SavePlan(ctx, response.Record); err != nil {
		return model.PlanResponse{}, err
	}
	h.persistTrace(ctx, runID, response.Trace)
	return response, nil
}

func (h *Harness) Plans(ctx context.Context) ([]model.ChangePlan, error) {
	return h.store.ListPlans(ctx)
}

func (h *Harness) PlanByID(ctx context.Context, id string) (*model.ChangePlan, error) {
	return h.store.GetPlan(ctx, id)
}

func (h *Harness) Approve(ctx context.Context, planID string, request model.ApprovalRequest) (*model.ApprovalResponse, error) {
	if !h.write {
		return nil, errors.New("write workflow is disabled")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	plan, err := h.store.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != model.PlanPending {
		return nil, fmt.Errorf("plan status %s cannot be approved", plan.Status)
	}
	if request.PlanDigest != plan.Digest || request.BaseRevision != plan.BaseRevision {
		return nil, errors.New("approval does not match the plan digest and base revision")
	}
	if h.workspace.Info().Revision != plan.BaseRevision {
		return nil, errors.New("repository revision changed; replan is required")
	}
	if err := validateApprovalScope(plan, &request, h.profile); err != nil {
		return nil, err
	}
	token := security.Token()
	approval := &model.Approval{
		ID: security.ID("approval"), PlanID: plan.ID, PlanDigest: plan.Digest, BaseRevision: plan.BaseRevision,
		AllowedPaths: request.AllowedPaths, TestSuiteIDs: request.TestSuiteIDs,
		TokenHash: security.Hash(token), CreatedAt: time.Now().UTC(),
	}
	if err := h.store.SaveApproval(ctx, approval); err != nil {
		return nil, err
	}
	plan.Status = model.PlanApproved
	plan.UpdatedAt = time.Now().UTC()
	if err := h.store.SavePlan(ctx, plan); err != nil {
		return nil, err
	}
	return &model.ApprovalResponse{Approval: *approval, Token: token}, nil
}

func (h *Harness) Execute(ctx context.Context, planID, token string) (*model.Execution, error) {
	if !h.write {
		return nil, errors.New("write workflow is disabled")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	plan, err := h.store.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != model.PlanApproved {
		return nil, fmt.Errorf("plan status %s cannot execute", plan.Status)
	}
	approval, err := h.store.GetApprovalByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	providedHash := security.Hash(token)
	if subtle.ConstantTimeCompare([]byte(providedHash), []byte(approval.TokenHash)) != 1 {
		return nil, errors.New("invalid approval token")
	}
	if approval.PlanDigest != plan.Digest || approval.BaseRevision != plan.BaseRevision || h.workspace.Info().Revision != plan.BaseRevision {
		return nil, errors.New("approval is stale; replan is required")
	}
	plan.Status = model.PlanRunning
	plan.UpdatedAt = time.Now().UTC()
	if err := h.store.SavePlan(ctx, plan); err != nil {
		return nil, err
	}
	executionResult, executeErr := h.executor.Execute(ctx, plan, approval)
	plan.UpdatedAt = time.Now().UTC()
	if executeErr != nil {
		plan.Status = model.PlanFailed
	} else {
		plan.Status = model.PlanCompleted
	}
	_ = h.store.SavePlan(ctx, plan)
	return executionResult, executeErr
}

func (h *Harness) Execution(ctx context.Context, id string) (*model.Execution, error) {
	return h.store.GetExecution(ctx, id)
}

func (h *Harness) Discard(ctx context.Context, planID, executionID string) error {
	if !h.write {
		return errors.New("write workflow is disabled")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	plan, err := h.store.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	execRecord, err := h.store.GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if execRecord.PlanID != plan.ID {
		return errors.New("execution does not belong to plan")
	}
	if err := h.executor.Discard(ctx, execRecord); err != nil {
		return err
	}
	plan.Status = model.PlanDiscarded
	plan.UpdatedAt = time.Now().UTC()
	return h.store.SavePlan(ctx, plan)
}

func (h *Harness) Events(ctx context.Context, runID string) ([]model.TraceEvent, error) {
	return h.store.Events(ctx, runID)
}

func (h *Harness) persistTrace(ctx context.Context, runID string, events []model.TraceEvent) {
	for _, event := range events {
		_ = h.store.AppendEvent(ctx, runID, event)
	}
}

func validateInput(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("input is required")
	}
	if len([]rune(value)) > 4000 {
		return errors.New("input exceeds 4000 characters")
	}
	return nil
}

func validateApprovalScope(plan *model.ChangePlan, request *model.ApprovalRequest, profile *project.Profile) error {
	if len(request.AllowedPaths) == 0 || len(request.TestSuiteIDs) == 0 {
		return errors.New("approval requires allowed paths and independent test suites")
	}
	for _, allowed := range request.AllowedPaths {
		if !profile.PathAllowed(allowed) || !containedByAny(allowed, plan.CandidatePaths) {
			return fmt.Errorf("approved path %s is outside the proposed plan", allowed)
		}
	}
	proposedSuites := make(map[string]bool)
	for _, suiteID := range plan.TestSuiteIDs {
		proposedSuites[suiteID] = true
	}
	for _, suiteID := range request.TestSuiteIDs {
		if !proposedSuites[suiteID] {
			return fmt.Errorf("test suite %s was not proposed by the plan", suiteID)
		}
		if _, ok := profile.Suite(suiteID); !ok {
			return fmt.Errorf("unknown test suite %s", suiteID)
		}
	}
	return nil
}

func containedByAny(value string, candidates []string) bool {
	value = filepath.ToSlash(filepath.Clean(value))
	for _, candidate := range candidates {
		candidate = filepath.ToSlash(filepath.Clean(candidate))
		if candidate == value {
			return true
		}
		if strings.HasSuffix(candidate, "/**") {
			prefix := strings.TrimSuffix(candidate, "/**")
			if value == prefix || strings.HasPrefix(value, prefix+"/") {
				return true
			}
		}
	}
	return false
}

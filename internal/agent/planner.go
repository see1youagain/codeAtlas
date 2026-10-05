package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lizhao/codeatlas/internal/llm"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/security"
)

const plannerSystemPrompt = `You are the Architect role in CodeAtlas. Produce a change plan, not code.
Return exactly one JSON object without Markdown fences using this schema:
{"summary":"...","non_goals":["..."],"facts_to_verify":["..."],"candidate_paths":["gateway/**"],"steps":["..."],"test_suite_ids":["go-test-gateway"],"risks":["..."],"rollback":"..."}
Use only repository facts supplied by the Researcher. Candidate paths must be relative repository paths. Select tests only from Available test suites. Do not claim the change has been made.`

type planDraft struct {
	Summary        string   `json:"summary"`
	NonGoals       []string `json:"non_goals"`
	FactsToVerify  []string `json:"facts_to_verify"`
	CandidatePaths []string `json:"candidate_paths"`
	Steps          []string `json:"steps"`
	TestSuiteIDs   []string `json:"test_suite_ids"`
	Risks          []string `json:"risks"`
	Rollback       string   `json:"rollback"`
}

type Planner struct {
	client    llm.Client
	workspace *repository.Workspace
	profile   *project.Profile
}

func NewPlanner(client llm.Client, workspace *repository.Workspace, profile *project.Profile) *Planner {
	return &Planner{client: client, workspace: workspace, profile: profile}
}

func (p *Planner) Create(ctx context.Context, runID, requirement, investigation string, evidence []model.Citation) (model.PlanResponse, error) {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return model.PlanResponse{}, errors.New("requirement is required")
	}
	info := p.workspace.Info()
	suites, _ := json.Marshal(p.profile.TestSuites)
	evidenceJSON, _ := json.Marshal(evidence)
	prompt := fmt.Sprintf("Requirement:\n%s\n\nRepository:\nname=%s\nrevision=%s\nservices=%s\n\nResearcher evidence report:\n%s\n\nEvidence citations:\n%s\n\nAvailable test suites:\n%s",
		requirement, info.Name, info.Revision, strings.Join(info.Services, ", "), investigation, string(evidenceJSON), string(suites))
	started := time.Now()
	raw, err := p.client.Complete(ctx, plannerSystemPrompt, prompt)
	trace := []model.TraceEvent{{At: time.Now().UTC(), Role: "architect", Type: "model", Name: "create_plan", Success: err == nil, DurationMS: time.Since(started).Milliseconds()}}
	if err != nil {
		return model.PlanResponse{}, err
	}
	var draft planDraft
	if err := json.Unmarshal([]byte(stripFence(raw)), &draft); err != nil {
		return model.PlanResponse{}, fmt.Errorf("architect returned invalid plan JSON: %w", err)
	}
	if err := p.validateDraft(&draft); err != nil {
		return model.PlanResponse{}, err
	}
	now := time.Now().UTC()
	plan := &model.ChangePlan{
		ID: security.ID("plan"), RunID: runID, Requirement: requirement, BaseRevision: info.Revision,
		Summary: draft.Summary, NonGoals: draft.NonGoals, FactsToVerify: draft.FactsToVerify, Evidence: evidence,
		CandidatePaths: draft.CandidatePaths, Steps: draft.Steps, TestSuiteIDs: draft.TestSuiteIDs,
		Risks: draft.Risks, Rollback: draft.Rollback, Status: model.PlanPending, CreatedAt: now, UpdatedAt: now,
	}
	digest, err := security.Digest(struct {
		Requirement    string
		BaseRevision   string
		Summary        string
		NonGoals       []string
		FactsToVerify  []string
		Evidence       []model.Citation
		CandidatePaths []string
		Steps          []string
		TestSuiteIDs   []string
		Risks          []string
		Rollback       string
	}{plan.Requirement, plan.BaseRevision, plan.Summary, plan.NonGoals, plan.FactsToVerify, plan.Evidence, plan.CandidatePaths, plan.Steps, plan.TestSuiteIDs, plan.Risks, plan.Rollback})
	if err != nil {
		return model.PlanResponse{}, err
	}
	plan.Digest = digest
	trace = append(trace, model.TraceEvent{At: time.Now().UTC(), Role: "qa", Type: "policy", Name: "approval_gate", Success: true, Message: "write tools remain hidden until digest- and revision-bound approval"})
	return model.PlanResponse{RunID: runID, Revision: info.Revision, Plan: renderPlan(plan), Record: plan, Trace: trace}, nil
}

func (p *Planner) validateDraft(draft *planDraft) error {
	if strings.TrimSpace(draft.Summary) == "" || len(draft.Steps) == 0 || len(draft.CandidatePaths) == 0 || len(draft.TestSuiteIDs) == 0 {
		return errors.New("plan gate requires summary, steps, candidate paths, and independent test suites")
	}
	for _, path := range draft.CandidatePaths {
		if !p.profile.PathAllowed(path) {
			return fmt.Errorf("plan gate rejected unsafe candidate path %s", path)
		}
	}
	for _, suiteID := range draft.TestSuiteIDs {
		if _, ok := p.profile.Suite(suiteID); !ok {
			return fmt.Errorf("plan gate rejected unknown test suite %s", suiteID)
		}
	}
	return nil
}

func renderPlan(plan *model.ChangePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 目标\n\n%s\n\n## 非目标\n", plan.Summary)
	for _, value := range plan.NonGoals {
		fmt.Fprintf(&b, "- %s\n", value)
	}
	b.WriteString("\n## 待确认事实\n")
	for _, value := range plan.FactsToVerify {
		fmt.Fprintf(&b, "- %s\n", value)
	}
	b.WriteString("\n## 批准候选范围\n")
	for _, value := range plan.CandidatePaths {
		fmt.Fprintf(&b, "- `%s`\n", value)
	}
	b.WriteString("\n## 实施步骤\n")
	for index, value := range plan.Steps {
		fmt.Fprintf(&b, "%d. %s\n", index+1, value)
	}
	b.WriteString("\n## 独立测试套件\n")
	for _, value := range plan.TestSuiteIDs {
		fmt.Fprintf(&b, "- `%s`\n", value)
	}
	b.WriteString("\n## 风险\n")
	for _, value := range plan.Risks {
		fmt.Fprintf(&b, "- %s\n", value)
	}
	fmt.Fprintf(&b, "\n## 回滚\n\n%s\n\n> Base Revision: `%s`; Plan Digest: `%s`; 未审批前不开放写工具。", plan.Rollback, plan.BaseRevision, plan.Digest)
	return b.String()
}

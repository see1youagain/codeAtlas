package model

import "time"

type Citation struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Snippet   string `json:"snippet"`
}

type TraceEvent struct {
	At         time.Time      `json:"at"`
	Role       string         `json:"role"`
	Type       string         `json:"type"`
	Name       string         `json:"name,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Success    bool           `json:"success"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Message    string         `json:"message,omitempty"`
}

type AskRequest struct {
	Question  string `json:"question"`
	SessionID string `json:"session_id,omitempty"`
	UseMemory *bool  `json:"use_memory,omitempty"`
}

type AskResponse struct {
	SessionID string       `json:"session_id,omitempty"`
	RunID     string       `json:"run_id"`
	Revision  string       `json:"revision"`
	Answer    string       `json:"answer"`
	Citations []Citation   `json:"citations"`
	Trace     []TraceEvent `json:"trace"`
}

type PlanRequest struct {
	Requirement string `json:"requirement"`
	SessionID   string `json:"session_id,omitempty"`
	UseMemory   *bool  `json:"use_memory,omitempty"`
}

type PlanResponse struct {
	SessionID string       `json:"session_id,omitempty"`
	RunID     string       `json:"run_id"`
	Revision  string       `json:"revision"`
	Plan      string       `json:"plan"`
	Record    *ChangePlan  `json:"record,omitempty"`
	Trace     []TraceEvent `json:"trace"`
}

type ChangePlan struct {
	ID             string     `json:"id"`
	RunID          string     `json:"run_id"`
	Requirement    string     `json:"requirement"`
	BaseRevision   string     `json:"base_revision"`
	Summary        string     `json:"summary"`
	NonGoals       []string   `json:"non_goals"`
	FactsToVerify  []string   `json:"facts_to_verify"`
	Evidence       []Citation `json:"evidence"`
	CandidatePaths []string   `json:"candidate_paths"`
	Steps          []string   `json:"steps"`
	TestSuiteIDs   []string   `json:"test_suite_ids"`
	Risks          []string   `json:"risks"`
	Rollback       string     `json:"rollback"`
	Status         PlanStatus `json:"status"`
	Digest         string     `json:"digest"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PlanStatus string

const (
	PlanPending   PlanStatus = "pending_approval"
	PlanApproved  PlanStatus = "approved"
	PlanRunning   PlanStatus = "running"
	PlanCompleted PlanStatus = "completed"
	PlanFailed    PlanStatus = "failed"
	PlanDiscarded PlanStatus = "discarded"
)

type ApprovalRequest struct {
	PlanDigest   string   `json:"plan_digest"`
	BaseRevision string   `json:"base_revision"`
	AllowedPaths []string `json:"allowed_paths"`
	TestSuiteIDs []string `json:"test_suite_ids"`
}

type Approval struct {
	ID            string     `json:"id"`
	PlanID        string     `json:"plan_id"`
	PlanDigest    string     `json:"plan_digest"`
	BaseRevision  string     `json:"base_revision"`
	AllowedPaths  []string   `json:"allowed_paths"`
	TestSuiteIDs  []string   `json:"test_suite_ids"`
	TokenHash     string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	InvalidatedAt *time.Time `json:"invalidated_at,omitempty"`
}

type ApprovalResponse struct {
	Approval Approval `json:"approval"`
	Token    string   `json:"approval_token"`
}

type ExecuteRequest struct {
	ApprovalToken string `json:"approval_token"`
}

type TestResult struct {
	SuiteID    string `json:"suite_id"`
	Command    string `json:"command"`
	Passed     bool   `json:"passed"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output"`
}

type Execution struct {
	ID           string       `json:"id"`
	PlanID       string       `json:"plan_id"`
	Status       string       `json:"status"`
	WorktreePath string       `json:"worktree_path,omitempty"`
	Branch       string       `json:"branch,omitempty"`
	ChangedFiles []string     `json:"changed_files"`
	Diff         string       `json:"diff,omitempty"`
	Tests        []TestResult `json:"tests"`
	Review       string       `json:"review,omitempty"`
	Error        string       `json:"error,omitempty"`
	StartedAt    time.Time    `json:"started_at"`
	FinishedAt   *time.Time   `json:"finished_at,omitempty"`
}

type KnowledgeReport struct {
	ID              string    `json:"id"`
	FromRevision    string    `json:"from_revision"`
	ToRevision      string    `json:"to_revision"`
	ChangedFiles    []string  `json:"changed_files"`
	ImpactedModules []string  `json:"impacted_modules"`
	SuggestedDocs   []string  `json:"suggested_docs"`
	Summary         string    `json:"summary"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

type TestSuite struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Directory   string   `json:"directory"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	TimeoutSec  int      `json:"timeout_sec"`
}

type RepositoryInfo struct {
	Name       string   `json:"name"`
	Root       string   `json:"root"`
	Revision   string   `json:"revision"`
	Files      int      `json:"files"`
	GoModules  int      `json:"go_modules"`
	Services   []string `json:"services"`
	LastScanAt string   `json:"last_scan_at"`
}

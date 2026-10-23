package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lizhao/codeatlas/internal/llm"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/security"
	"github.com/lizhao/codeatlas/internal/store"
	"github.com/lizhao/codeatlas/internal/worktree"
)

const developerSystemPrompt = `You are the Developer role in CodeAtlas. Work only inside the approved scope.
Return exactly one JSON object without Markdown fences.
Available actions:
{"thought":"...","action":{"name":"search_code","input":{"query":"...","path":""}}}
{"thought":"...","action":{"name":"read_file","input":{"path":"...","start_line":1,"end_line":160}}}
{"thought":"...","action":{"name":"replace_text","input":{"path":"...","old":"exact old text","new":"replacement"}}}
{"thought":"...","action":{"name":"create_file","input":{"path":"...","content":"..."}}}
{"thought":"...","action":{"name":"run_tests","input":{}}}
{"thought":"...","final":{"summary":"what changed and why"}}
Never modify a path outside Allowed paths. Do not weaken or delete tests merely to make them pass. Inspect before editing.`

type Executor struct {
	client        llm.Client
	profile       *project.Profile
	worktrees     *worktree.Manager
	store         *store.Store
	maxIterations int
}

type editDecision struct {
	Thought string      `json:"thought"`
	Action  *editAction `json:"action,omitempty"`
	Final   *struct {
		Summary string `json:"summary"`
	} `json:"final,omitempty"`
}

type editAction struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func New(client llm.Client, profile *project.Profile, worktrees *worktree.Manager, persistence *store.Store, maxIterations int) *Executor {
	return &Executor{client: client, profile: profile, worktrees: worktrees, store: persistence, maxIterations: maxIterations}
}

func (e *Executor) Execute(ctx context.Context, plan *model.ChangePlan, approval *model.Approval) (*model.Execution, error) {
	execution := &model.Execution{ID: security.ID("exec"), PlanID: plan.ID, Status: "preparing", ChangedFiles: []string{}, Tests: []model.TestResult{}, StartedAt: time.Now().UTC()}
	if err := e.store.SaveExecution(ctx, execution); err != nil {
		return nil, err
	}
	workspace, err := e.worktrees.Prepare(ctx, execution.ID, plan.BaseRevision)
	if err != nil {
		return e.fail(ctx, execution, err)
	}
	execution.WorktreePath = workspace.Path
	execution.Branch = workspace.Branch
	execution.Status = "baseline_testing"
	_ = e.store.SaveExecution(ctx, execution)

	baseline := e.runSuites(ctx, workspace.Path, approval.TestSuiteIDs)
	execution.Tests = baseline
	if !allPassed(baseline) {
		return e.fail(ctx, execution, errors.New("independent baseline tests failed; refusing to attribute existing failures to the agent"))
	}

	repo, err := repository.New(workspace.Path)
	if err != nil {
		return e.fail(ctx, execution, err)
	}
	execution.Status = "editing"
	_ = e.store.SaveExecution(ctx, execution)
	scratchpad := e.initialPrompt(plan, approval)
	var developerSummary string
	for iteration := 1; iteration <= e.maxIterations; iteration++ {
		raw, callErr := e.client.Complete(ctx, developerSystemPrompt, scratchpad)
		if callErr != nil {
			return e.fail(ctx, execution, callErr)
		}
		var decision editDecision
		if err := json.Unmarshal([]byte(stripFence(raw)), &decision); err != nil {
			return e.fail(ctx, execution, fmt.Errorf("developer returned invalid JSON: %w", err))
		}
		if decision.Final != nil {
			developerSummary = strings.TrimSpace(decision.Final.Summary)
			break
		}
		if decision.Action == nil {
			return e.fail(ctx, execution, errors.New("developer returned neither action nor final"))
		}
		observation := e.executeAction(ctx, repo, workspace.Path, approval, decision.Action)
		scratchpad += fmt.Sprintf("\n\nStep %d thought: %s\nAction: %s\nObservation:\n%s", iteration, decision.Thought, decision.Action.Name, observation)
		if len(scratchpad) > 80000 {
			scratchpad = scratchpad[len(scratchpad)-80000:]
		}
		if iteration == e.maxIterations {
			return e.fail(ctx, execution, fmt.Errorf("developer exceeded %d iterations", e.maxIterations))
		}
	}

	diff, changedFiles, err := e.worktrees.Diff(ctx, workspace, plan.BaseRevision)
	if err != nil {
		return e.fail(ctx, execution, err)
	}
	if len(changedFiles) == 0 {
		return e.fail(ctx, execution, errors.New("developer finished without a code change"))
	}
	for _, path := range changedFiles {
		if !pathApproved(path, approval.AllowedPaths) || !e.profile.PathAllowed(path) {
			return e.fail(ctx, execution, fmt.Errorf("scope gate rejected changed path %s", path))
		}
	}

	execution.Status = "qa_testing"
	_ = e.store.SaveExecution(ctx, execution)
	postTests := e.runSuites(ctx, workspace.Path, approval.TestSuiteIDs)
	execution.Tests = postTests
	if !allPassed(postTests) {
		execution.Diff = diff
		execution.ChangedFiles = changedFiles
		return e.fail(ctx, execution, errors.New("QA test gate failed"))
	}
	execution.Diff = diff
	execution.ChangedFiles = changedFiles
	execution.Review = fmt.Sprintf("Deterministic review passed: %d changed file(s) are inside the approved scope; all %d independent suite(s) passed. Developer summary: %s", len(changedFiles), len(postTests), developerSummary)
	execution.Status = "completed"
	now := time.Now().UTC()
	execution.FinishedAt = &now
	if err := e.store.SaveExecution(ctx, execution); err != nil {
		return nil, err
	}
	return execution, nil
}

func (e *Executor) Discard(ctx context.Context, execution *model.Execution) error {
	if execution.WorktreePath == "" || execution.Branch == "" {
		return errors.New("execution has no worktree")
	}
	workspace := &worktree.Workspace{Path: execution.WorktreePath, Branch: execution.Branch, Mirror: filepath.Join(filepath.Dir(filepath.Dir(execution.WorktreePath)), "mirrors", "")}
	// Recover the actual mirror from git's common directory instead of trusting client input.
	cmd := exec.CommandContext(ctx, "git", "-C", execution.WorktreePath, "rev-parse", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("resolve worktree metadata: %w", err)
	}
	common := strings.TrimSpace(string(output))
	if !filepath.IsAbs(common) {
		common = filepath.Join(execution.WorktreePath, common)
	}
	workspace.Mirror = filepath.Clean(common)
	return e.worktrees.Discard(ctx, workspace)
}

func (e *Executor) executeAction(ctx context.Context, repo *repository.Workspace, root string, approval *model.Approval, action *editAction) string {
	switch action.Name {
	case "search_code":
		results, err := repo.Search(stringInput(action.Input, "query"), stringInput(action.Input, "path"), 30)
		if err != nil {
			return "ERROR: " + err.Error()
		}
		payload, _ := json.Marshal(results)
		return string(payload)
	case "read_file":
		results, err := repo.ReadFile(stringInput(action.Input, "path"), intInput(action.Input, "start_line", 1), intInput(action.Input, "end_line", 160))
		if err != nil {
			return "ERROR: " + err.Error()
		}
		payload, _ := json.Marshal(results)
		return string(payload)
	case "replace_text":
		err := replaceText(root, stringInput(action.Input, "path"), stringInput(action.Input, "old"), stringInput(action.Input, "new"), approval.AllowedPaths, e.profile)
		if err != nil {
			return "ERROR: " + err.Error()
		}
		return "OK: exact text replaced"
	case "create_file":
		err := createFile(root, stringInput(action.Input, "path"), stringInput(action.Input, "content"), approval.AllowedPaths, e.profile)
		if err != nil {
			return "ERROR: " + err.Error()
		}
		// An explicitly approved created file must be reviewable even if ignored
		// by the target project's .gitignore. This touches only the worktree index.
		cmd := exec.CommandContext(ctx, "git", "-C", root, "add", "--intent-to-add", "--force", "--", stringInput(action.Input, "path"))
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Sprintf("ERROR: include created file in diff: %v: %s", err, output)
		}
		return "OK: file created"
	case "run_tests":
		results := e.runSuites(ctx, root, approval.TestSuiteIDs)
		payload, _ := json.Marshal(results)
		return string(payload)
	default:
		return "ERROR: tool is not visible to Developer"
	}
}

func (e *Executor) runSuites(ctx context.Context, root string, suiteIDs []string) []model.TestResult {
	results := make([]model.TestResult, 0, len(suiteIDs))
	for _, suiteID := range suiteIDs {
		suite, ok := e.profile.Suite(suiteID)
		if !ok {
			results = append(results, model.TestResult{SuiteID: suiteID, Passed: false, ExitCode: -1, Output: "unknown test suite"})
			continue
		}
		timeout := time.Duration(suite.TimeoutSec) * time.Second
		if timeout <= 0 || timeout > 10*time.Minute {
			timeout = 3 * time.Minute
		}
		testCtx, cancel := context.WithTimeout(ctx, timeout)
		started := time.Now()
		cmd := exec.CommandContext(testCtx, suite.Command, suite.Args...)
		cmd.Dir = filepath.Join(root, filepath.FromSlash(suite.Directory))
		output, err := cmd.CombinedOutput()
		cancel()
		if len(output) > 256<<10 {
			output = append(output[:256<<10], []byte("\n... output truncated ...")...)
		}
		exitCode := 0
		if err != nil {
			exitCode = -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		results = append(results, model.TestResult{SuiteID: suite.ID, Command: suite.Command + " " + strings.Join(suite.Args, " "), Passed: err == nil, ExitCode: exitCode, DurationMS: time.Since(started).Milliseconds(), Output: string(output)})
	}
	return results
}

func (e *Executor) initialPrompt(plan *model.ChangePlan, approval *model.Approval) string {
	payload, _ := json.MarshalIndent(map[string]any{
		"requirement": plan.Requirement, "summary": plan.Summary, "steps": plan.Steps,
		"allowed_paths": approval.AllowedPaths, "test_suite_ids": approval.TestSuiteIDs,
		"base_revision": plan.BaseRevision,
	}, "", "  ")
	return "Approved change plan:\n" + string(payload)
}

func (e *Executor) fail(ctx context.Context, execution *model.Execution, err error) (*model.Execution, error) {
	execution.Status = "failed"
	execution.Error = err.Error()
	now := time.Now().UTC()
	execution.FinishedAt = &now
	_ = e.store.SaveExecution(ctx, execution)
	return execution, err
}

func replaceText(root, relative, oldText, newText string, allowed []string, profile *project.Profile) error {
	if oldText == "" {
		return errors.New("old text is required")
	}
	path, err := editablePath(root, relative, allowed, profile)
	if err != nil {
		return err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(payload) > 1<<20 {
		return errors.New("file exceeds 1 MiB edit limit")
	}
	if strings.Count(string(payload), oldText) != 1 {
		return errors.New("old text must occur exactly once")
	}
	updated := strings.Replace(string(payload), oldText, newText, 1)
	return os.WriteFile(path, []byte(updated), 0o600)
}

func createFile(root, relative, content string, allowed []string, profile *project.Profile) error {
	path, err := editablePath(root, relative, allowed, profile)
	if err != nil {
		return err
	}
	if len(content) > 1<<20 {
		return errors.New("content exceeds 1 MiB edit limit")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("create_file cannot overwrite an existing file")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func editablePath(root, relative string, allowed []string, profile *project.Profile) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(relative))
	if !profile.PathAllowed(clean) || !pathApproved(clean, allowed) {
		return "", errors.New("path is outside approved scope")
	}
	rootAbs, _ := filepath.Abs(root)
	candidate := filepath.Join(rootAbs, filepath.FromSlash(clean))
	rel, err := filepath.Rel(rootAbs, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes worktree")
	}
	for current := candidate; current != rootAbs; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symbolic links are not editable")
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
	return candidate, nil
}

func pathApproved(path string, allowed []string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, pattern := range allowed {
		pattern = filepath.ToSlash(filepath.Clean(pattern))
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return true
			}
			continue
		}
		if matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path)); matched || path == pattern {
			return true
		}
	}
	return false
}

func allPassed(results []model.TestResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if !result.Passed {
			return false
		}
	}
	return true
}

func stringInput(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func intInput(input map[string]any, key string, fallback int) int {
	if value, ok := input[key].(float64); ok {
		return int(value)
	}
	return fallback
}

func stripFence(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

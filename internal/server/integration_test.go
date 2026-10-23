package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizhao/codeatlas/internal/agent"
	"github.com/lizhao/codeatlas/internal/execution"
	"github.com/lizhao/codeatlas/internal/harness"
	"github.com/lizhao/codeatlas/internal/knowledge"
	"github.com/lizhao/codeatlas/internal/mcpserver"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/store"
	"github.com/lizhao/codeatlas/internal/tool"
	"github.com/lizhao/codeatlas/internal/worktree"
)

// Real repository, SQLite, HTTP, tools, worktree and Go tests; only LLM is scripted.
type workflowClient struct {
	prompts []string
	edits   int
	fail    bool
}

func (c *workflowClient) Complete(_ context.Context, system, prompt string) (string, error) {
	c.prompts = append(c.prompts, prompt)
	if c.fail {
		return `{}`, nil
	}
	if strings.Contains(system, "Architect role") {
		return `{"summary":"增加 Double 函数","candidate_paths":["new.go"],"steps":["增加函数"],"test_suite_ids":["go-test-root"],"risks":["确认行为"],"rollback":"丢弃工作区"}`, nil
	}
	if strings.Contains(system, "Developer role") {
		c.edits++
		if c.edits == 1 {
			return `{"action":{"name":"create_file","input":{"path":"new.go","content":"package calc\nfunc Double(n int) int { return n * 2 }\n"}}}`, nil
		}
		return `{"final":{"summary":"增加 Double 函数"}}`, nil
	}
	if strings.Contains(prompt, "\nAction: search_code\nObservation:") {
		return `{"final":{"answer":"上传通过 Kafka 触发转码 [C1]"}}`, nil
	}
	return `{"action":{"name":"search_code","input":{"query":"Kafka","path":"README.md"}}}`, nil
}

func TestHTTPMemoryPlanAndExecutionWorkflow(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{"README.md": "Kafka upload triggers transcode\n", "go.mod": "module example.com/calc\n\ngo 1.22\n", "calc.go": "package calc\nfunc Add(a,b int) int {return a+b}\n", "calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T){if Add(2,3)!=5{t.Fatal(\"bad sum\")}}\n", ".gitignore": "new.go\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, output)
		}
	}
	git("init")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture")
	w, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := project.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	wtrees, err := worktree.NewManager(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &workflowClient{}
	h := harness.New(agent.NewReAct(c, tool.NewDispatcher(w), w, 5), agent.NewPlanner(c, w, p), execution.New(c, p, wtrees, s, 5), w, p, s, true)
	api := New(w, h, knowledge.New(w, s), mcpserver.New(w, h, "test-admin-token", true), "test-admin-token", true, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	request := func(method, path string, payload any, token, runID string, status int, out any) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("X-Request-ID", runID)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		api.ServeHTTP(response, r)
		if response.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		if out != nil {
			if err := json.Unmarshal(response.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	var first, follow model.AskResponse
	request("POST", "/api/v1/ask", model.AskRequest{Question: "Kafka 上传转码"}, "", "ask-1", 200, &first)
	if first.SessionID == "" {
		t.Fatal("session id missing")
	}
	request("POST", "/api/v1/ask", model.AskRequest{Question: "它怎样继续？", SessionID: first.SessionID}, "", "ask-2", 200, &follow)
	if !strings.Contains(c.prompts[2], "Untrusted prior conversation") || len(follow.Citations) != 1 {
		t.Fatal("follow-up context not integrated with fresh evidence")
	}
	var records []model.ResearchMemory
	request("GET", "/api/v1/memories", nil, "", "list-unauthorized", 401, nil)
	request("GET", "/api/v1/memories", nil, "test-admin-token", "list", 200, &records)
	count := len(records)
	disabled := false
	start := len(c.prompts)
	request("POST", "/api/v1/ask", model.AskRequest{Question: "Kafka 上传转码", SessionID: first.SessionID, UseMemory: &disabled}, "", "disabled", 200, nil)
	if strings.Contains(c.prompts[start], "historical_summary") || strings.Contains(c.prompts[start], "Untrusted prior conversation") {
		t.Fatal("disabled memory used")
	}
	request("GET", "/api/v1/memories", nil, "test-admin-token", "list-after", 200, &records)
	if len(records) != count {
		t.Fatal("disabled memory was recorded")
	}
	request("POST", "/api/v1/ask", map[string]any{"question": "hello", "session_id": "../bad"}, "", "invalid", 422, nil)

	var planned model.PlanResponse
	request("POST", "/api/v1/plan", model.PlanRequest{Requirement: "Kafka 上传模块增加函数", SessionID: first.SessionID}, "", "plan", 201, &planned)
	if planned.SessionID != first.SessionID || planned.Record == nil {
		t.Fatal("plan session missing")
	}
	if !strings.Contains(planned.Trace[0].Message, "2 prior turns") {
		t.Fatal("disabled request altered session history")
	}
	var approved model.ApprovalResponse
	request("POST", "/api/v1/plans/"+planned.Record.ID+"/approve", model.ApprovalRequest{PlanDigest: planned.Record.Digest, BaseRevision: planned.Record.BaseRevision, AllowedPaths: []string{"new.go"}, TestSuiteIDs: []string{"go-test-root"}}, "test-admin-token", "approve", 201, &approved)
	var executed model.Execution
	request("POST", "/api/v1/plans/"+planned.Record.ID+"/execute", model.ExecuteRequest{ApprovalToken: approved.Token}, "test-admin-token", "execute", 200, &executed)
	if len(executed.ChangedFiles) != 1 || executed.ChangedFiles[0] != "new.go" || !strings.Contains(executed.Diff, "+func Double") {
		t.Fatalf("created ignored file omitted: %#v", executed)
	}
	if _, err := os.Stat(filepath.Join(root, "new.go")); !os.IsNotExist(err) {
		t.Fatal("source repository modified")
	}
	request("DELETE", "/api/v1/plans/"+planned.Record.ID+"/executions/"+executed.ID, nil, "test-admin-token", "discard", 204, nil)
	request("DELETE", "/api/v1/memories/"+records[0].ID, nil, "test-admin-token", "delete", 204, nil)
	request("DELETE", "/api/v1/memories/"+records[0].ID, nil, "test-admin-token", "delete-again", 404, nil)
	request("DELETE", "/api/v1/sessions/"+first.SessionID, nil, "test-admin-token", "forget-session", 204, nil)
	start = len(c.prompts)
	request("POST", "/api/v1/ask", model.AskRequest{Question: "继续查询", SessionID: first.SessionID}, "", "after-forget", 200, nil)
	if strings.Contains(c.prompts[start], "Untrusted prior conversation") {
		t.Fatal("deleted conversation still injected")
	}
	c.fail = true
	request("POST", "/api/v1/ask", model.AskRequest{Question: "故障注入"}, "", "failed-run", 422, nil)
	var failedEvents []model.TraceEvent
	request("GET", "/api/v1/runs/failed-run/events", nil, "", "failed-events", 200, &failedEvents)
	if len(failedEvents) == 0 || failedEvents[len(failedEvents)-1].Name != "run_failed" {
		t.Fatal("failed run trace missing")
	}
	var events []model.TraceEvent
	request("GET", "/api/v1/runs/ask-2/events", nil, "", "events", 200, &events)
	if len(events) == 0 {
		t.Fatal("trace not persisted")
	}
}

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizhao/codeatlas/internal/memory"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/store"
	"github.com/lizhao/codeatlas/internal/tool"
)

type inspectingClient struct {
	sequenceClient
	prompts []string
}

func (c *inspectingClient) Complete(ctx context.Context, system, prompt string) (string, error) {
	c.prompts = append(c.prompts, prompt)
	return c.sequenceClient.Complete(ctx, system, prompt)
}

func TestRecalledMemoryStillRequiresFreshToolEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("Kafka upload triggers transcode\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"add", "README.md"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, output)
		}
	}
	w, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := memory.New(s, w)
	responses := []string{`{"action":{"name":"search_code","input":{"query":"Kafka"}}}`, `{"final":{"answer":"Kafka 上传触发转码 [C1]"}}`}
	first := NewReAct(&sequenceClient{responses: responses}, tool.NewDispatcher(w), w, 4)
	first.SetMemory(m)
	if _, err := first.Run(context.Background(), "first", "Kafka 上传转码"); err != nil {
		t.Fatal(err)
	}
	client := &inspectingClient{sequenceClient: sequenceClient{responses: responses}}
	second := NewReAct(client, tool.NewDispatcher(w), w, 4)
	second.SetMemory(m)
	response, err := second.Run(context.Background(), "second", "Kafka 上传转码流程")
	if err != nil || len(response.Citations) != 1 || !strings.Contains(client.prompts[0], "historical_summary") {
		t.Fatalf("recall integration: %v", err)
	}
	guess := NewReAct(&sequenceClient{responses: []string{`{"final":{"answer":"历史答案 [C1]"}}`}}, tool.NewDispatcher(w), w, 2)
	guess.SetMemory(m)
	if _, err := guess.Run(context.Background(), "guess", "Kafka 上传转码"); err == nil {
		t.Fatal("memory alone bypassed citation gate")
	}
}

func TestCitationRejectsMixedUnknownIDsAndInvalidLines(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("Kafka\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := w.ReadFile("README.md", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAnswer("[C1] [C99]", evidence, w); err == nil {
		t.Fatal("mixed unknown reference accepted")
	}
	evidence[0].EndLine = 999
	if err := verifyAnswer("[C1]", evidence, w); err == nil {
		t.Fatal("out-of-range lines accepted")
	}
}

func TestBoundedContextPreservesTaskAndRecentCompleteSteps(t *testing.T) {
	if got := boundedContext("task:", []string{"old observation", "new"}, 8); got != "task:new" {
		t.Fatalf("got %q", got)
	}
}

func TestCitationRejectsEvidenceChangedAfterRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("Kafka\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := w.ReadFile("README.md", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("HTTP\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAnswer("[C1]", evidence, w); err == nil {
		t.Fatal("changed evidence accepted")
	}
}

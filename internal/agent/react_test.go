package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/tool"
)

type sequenceClient struct {
	responses []string
	index     int
}

func (c *sequenceClient) Complete(context.Context, string, string) (string, error) {
	response := c.responses[c.index]
	c.index++
	return response, nil
}

func TestReActReturnsGroundedAnswer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("Gateway forwards authenticated requests.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	client := &sequenceClient{responses: []string{
		`{"thought":"find evidence","action":{"name":"search_code","input":{"query":"authenticated"}}}`,
		`{"thought":"enough evidence","final":{"answer":"网关负责转发已鉴权请求。[C1]"}}`,
	}}
	runner := NewReAct(client, tool.NewDispatcher(workspace), workspace, 4)
	response, err := runner.Run(context.Background(), "run-test", "网关做什么？")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Citations) != 1 || response.Citations[0].Path != "README.md" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestReActRejectsUngroundedFinal(t *testing.T) {
	workspace, err := repository.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &sequenceClient{responses: []string{`{"thought":"guess","final":{"answer":"没有证据的结论"}}`}}
	runner := NewReAct(client, tool.NewDispatcher(workspace), workspace, 2)
	if _, err := runner.Run(context.Background(), "run-test", "问题"); err == nil {
		t.Fatal("expected citation gate failure")
	}
}

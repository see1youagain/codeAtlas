package mcpserver

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReadMCPListsAndCallsRepositoryTools(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("video transcode pipeline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	handlers := New(workspace, nil, "", false)
	server := httptest.NewServer(handlers.Read)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 4 {
		t.Fatalf("unexpected tools: %#v %v", tools, err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_code", Arguments: map[string]any{"query": "transcode"}})
	if err != nil || result.IsError {
		t.Fatalf("tool call failed: %#v %v", result, err)
	}
}

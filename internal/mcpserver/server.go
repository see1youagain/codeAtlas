package mcpserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/lizhao/codeatlas/internal/harness"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Handlers struct {
	Read  http.Handler
	Admin http.Handler
}

type listInput struct {
	Path  string `json:"path,omitempty" jsonschema:"repository-relative directory"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of files, up to 500"`
}

type listOutput struct {
	Files []string `json:"files"`
}

type searchInput struct {
	Query string `json:"query" jsonschema:"literal case-insensitive text to find"`
	Path  string `json:"path,omitempty" jsonschema:"optional repository-relative directory"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum matches, up to 100"`
}

type searchOutput struct {
	Citations []model.Citation `json:"citations"`
}

type readInput struct {
	Path      string `json:"path" jsonschema:"repository-relative file path"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"first one-based line"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"last one-based line, maximum 200 lines per call"`
}

type readOutput struct {
	Citations []model.Citation `json:"citations"`
}

type executeInput struct {
	PlanID        string `json:"plan_id" jsonschema:"approved CodeAtlas plan id"`
	ApprovalToken string `json:"approval_token" jsonschema:"one-time approval capability returned by the REST approval endpoint"`
}

type discardInput struct {
	PlanID      string `json:"plan_id"`
	ExecutionID string `json:"execution_id"`
}

func New(workspace *repository.Workspace, runtime *harness.Harness, adminToken string, writeEnabled bool) Handlers {
	readServer := baseServer(workspace)
	readHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return readServer }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})

	adminServer := baseServer(workspace)
	if writeEnabled {
		mcp.AddTool(adminServer, &mcp.Tool{Name: "execute_plan", Description: "Execute an already approved CodeAtlas plan in an isolated worktree. Requires the plan-bound approval capability."},
			func(ctx context.Context, _ *mcp.CallToolRequest, input *executeInput) (*mcp.CallToolResult, *model.Execution, error) {
				result, err := runtime.Execute(ctx, input.PlanID, input.ApprovalToken)
				return nil, result, err
			})
		mcp.AddTool(adminServer, &mcp.Tool{Name: "discard_execution", Description: "Remove an execution worktree after review; it does not modify the source repository."},
			func(ctx context.Context, _ *mcp.CallToolRequest, input *discardInput) (*mcp.CallToolResult, map[string]bool, error) {
				err := runtime.Discard(ctx, input.PlanID, input.ExecutionID)
				return nil, map[string]bool{"discarded": err == nil}, err
			})
	}
	adminHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return adminServer }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return Handlers{Read: readHandler, Admin: requireBearer(adminToken, adminHandler)}
}

func baseServer(workspace *repository.Workspace) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "codeatlas-repository", Version: "0.3.0"}, &mcp.ServerOptions{
		Instructions: "Repository-grounded tools. Paths are relative to the configured repository. Read tools never return files outside that root.",
	})
	mcp.AddTool(server, &mcp.Tool{Name: "repo_summary", Description: "Return repository revision, file count, Go module count and top-level services."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, *model.RepositoryInfo, error) {
			info := workspace.Info()
			return nil, &info, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_files", Description: "List repository-relative text and source files inside a directory."},
		func(_ context.Context, _ *mcp.CallToolRequest, input *listInput) (*mcp.CallToolResult, *listOutput, error) {
			files, err := workspace.ListFiles(input.Path, input.Limit)
			return nil, &listOutput{Files: files}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "search_code", Description: "Find literal text in code and documentation, returning file and line citations."},
		func(_ context.Context, _ *mcp.CallToolRequest, input *searchInput) (*mcp.CallToolResult, *searchOutput, error) {
			citations, err := workspace.Search(input.Query, input.Path, input.Limit)
			return nil, &searchOutput{Citations: citations}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "read_file", Description: "Read at most 200 lines from a repository-relative text file."},
		func(_ context.Context, _ *mcp.CallToolRequest, input *readInput) (*mcp.CallToolResult, *readOutput, error) {
			citations, err := workspace.ReadFile(input.Path, input.StartLine, input.EndLine)
			return nil, &readOutput{Citations: citations}, err
		})
	return server
}

func requireBearer(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if expected == "" || len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="codeatlas-admin"`)
			http.Error(w, fmt.Sprintf("admin MCP authorization required"), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

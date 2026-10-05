package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/repository"
)

type Result struct {
	Output    string
	Citations []model.Citation
}

type Dispatcher struct {
	workspace *repository.Workspace
}

func NewDispatcher(workspace *repository.Workspace) *Dispatcher {
	return &Dispatcher{workspace: workspace}
}

func (d *Dispatcher) Definitions() string {
	return `Available read-only tools:
- repo_summary: input {}
- list_files: input {"path":"optional relative directory","limit":200}
- search_code: input {"query":"literal text","path":"optional relative directory","limit":30}
- read_file: input {"path":"relative file path","start_line":1,"end_line":120}`
}

func (d *Dispatcher) Execute(ctx context.Context, mode, name string, input map[string]any) (Result, error) {
	if mode != "ask" && mode != "plan" {
		return Result{}, errors.New("unsupported harness mode")
	}
	if name != "repo_summary" && name != "list_files" && name != "search_code" && name != "read_file" {
		return Result{}, fmt.Errorf("tool %q is not visible in %s mode", name, mode)
	}
	toolCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-toolCtx.Done():
		return Result{}, toolCtx.Err()
	default:
	}
	switch name {
	case "repo_summary":
		encoded, _ := json.Marshal(d.workspace.Info())
		return Result{Output: string(encoded)}, nil
	case "list_files":
		files, err := d.workspace.ListFiles(stringValue(input, "path"), intValue(input, "limit", 200))
		if err != nil {
			return Result{}, err
		}
		return Result{Output: strings.Join(files, "\n")}, nil
	case "search_code":
		citations, err := d.workspace.Search(stringValue(input, "query"), stringValue(input, "path"), intValue(input, "limit", 30))
		if err != nil {
			return Result{}, err
		}
		return Result{Output: formatCitations(citations), Citations: citations}, nil
	case "read_file":
		citations, err := d.workspace.ReadFile(stringValue(input, "path"), intValue(input, "start_line", 1), intValue(input, "end_line", 120))
		if err != nil {
			return Result{}, err
		}
		return Result{Output: formatCitations(citations), Citations: citations}, nil
	default:
		return Result{}, errors.New("unreachable tool")
	}
}

func formatCitations(citations []model.Citation) string {
	if len(citations) == 0 {
		return "No matching evidence."
	}
	var b strings.Builder
	for _, citation := range citations {
		fmt.Fprintf(&b, "[%s] %s:%d-%d\n%s\n", citation.ID, citation.Path, citation.StartLine, citation.EndLine, citation.Snippet)
	}
	return b.String()
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func intValue(input map[string]any, key string, fallback int) int {
	switch value := input[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return fallback
	}
}

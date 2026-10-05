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
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/tool"
)

const reactSystemPrompt = `You are the Repository Researcher in CodeAtlas.
Use ReAct internally: decide one tool call at a time, inspect its observation, then continue.
Repository facts must come from tools. Never invent paths, symbols, services, or configuration.
Return exactly one JSON object, without Markdown fences.
To use a tool: {"thought":"short reason","action":{"name":"search_code","input":{"query":"..."}}}
To finish: {"thought":"short reason","final":{"answer":"Chinese answer with [C1] citations"}}
Only cite evidence IDs that appear in observations. If evidence is insufficient, say so explicitly.`

type decision struct {
	Thought string  `json:"thought"`
	Action  *action `json:"action,omitempty"`
	Final   *final  `json:"final,omitempty"`
}

type action struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type final struct {
	Answer string `json:"answer"`
}

type ReAct struct {
	client        llm.Client
	tools         *tool.Dispatcher
	workspace     *repository.Workspace
	maxIterations int
}

func NewReAct(client llm.Client, tools *tool.Dispatcher, workspace *repository.Workspace, maxIterations int) *ReAct {
	return &ReAct{client: client, tools: tools, workspace: workspace, maxIterations: maxIterations}
}

func (r *ReAct) Run(ctx context.Context, runID, question string) (model.AskResponse, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return model.AskResponse{}, errors.New("question is required")
	}
	trace := make([]model.TraceEvent, 0, r.maxIterations*2)
	evidence := make([]model.Citation, 0)
	scratchpad := "Question: " + question + "\n\n" + r.tools.Definitions()
	for iteration := 1; iteration <= r.maxIterations; iteration++ {
		started := time.Now()
		raw, err := r.client.Complete(ctx, reactSystemPrompt, scratchpad)
		trace = append(trace, model.TraceEvent{At: time.Now().UTC(), Role: "researcher", Type: "model", Name: "decide", Success: err == nil, DurationMS: time.Since(started).Milliseconds()})
		if err != nil {
			return model.AskResponse{}, err
		}
		var next decision
		if err := json.Unmarshal([]byte(stripFence(raw)), &next); err != nil {
			return model.AskResponse{}, fmt.Errorf("model returned invalid ReAct JSON: %w", err)
		}
		if next.Final != nil {
			selected := selectCitations(next.Final.Answer, evidence)
			if err := verifyAnswer(next.Final.Answer, selected, r.workspace); err != nil {
				return model.AskResponse{}, err
			}
			trace = append(trace, model.TraceEvent{At: time.Now().UTC(), Role: "reviewer", Type: "verification", Name: "citation_gate", Success: true, Message: "answer and citations verified"})
			return model.AskResponse{RunID: runID, Revision: r.workspace.Info().Revision, Answer: next.Final.Answer, Citations: selected, Trace: trace}, nil
		}
		if next.Action == nil {
			return model.AskResponse{}, errors.New("model returned neither action nor final")
		}
		started = time.Now()
		result, toolErr := r.tools.Execute(ctx, "ask", next.Action.Name, next.Action.Input)
		trace = append(trace, model.TraceEvent{At: time.Now().UTC(), Role: "researcher", Type: "tool", Name: next.Action.Name, Input: next.Action.Input, Success: toolErr == nil, DurationMS: time.Since(started).Milliseconds()})
		observation := result.Output
		if toolErr != nil {
			observation = "Tool error: " + toolErr.Error()
		} else {
			var normalized []model.Citation
			evidence, normalized = mergeCitations(evidence, result.Citations)
			if len(normalized) > 0 {
				observation = renderCitations(normalized)
			}
		}
		scratchpad += fmt.Sprintf("\n\nStep %d thought: %s\nAction: %s\nObservation:\n%s", iteration, next.Thought, next.Action.Name, observation)
		if len(scratchpad) > 50000 {
			scratchpad = scratchpad[:50000]
		}
	}
	return model.AskResponse{}, fmt.Errorf("agent stopped after %d iterations without a verified answer", r.maxIterations)
}

func stripFence(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

func mergeCitations(existing, incoming []model.Citation) ([]model.Citation, []model.Citation) {
	seen := make(map[string]model.Citation)
	for _, citation := range existing {
		seen[citation.Path+fmt.Sprint(citation.StartLine, citation.EndLine)] = citation
	}
	normalized := make([]model.Citation, 0, len(incoming))
	for _, citation := range incoming {
		key := citation.Path + fmt.Sprint(citation.StartLine, citation.EndLine)
		if previous, ok := seen[key]; ok {
			citation.ID = previous.ID
		} else {
			citation.ID = fmt.Sprintf("C%d", len(existing)+1)
			existing = append(existing, citation)
			seen[key] = citation
		}
		normalized = append(normalized, citation)
	}
	return existing, normalized
}

func renderCitations(citations []model.Citation) string {
	var b strings.Builder
	for _, citation := range citations {
		fmt.Fprintf(&b, "[%s] %s:%d-%d\n%s\n", citation.ID, citation.Path, citation.StartLine, citation.EndLine, citation.Snippet)
	}
	return b.String()
}

func selectCitations(answer string, evidence []model.Citation) []model.Citation {
	selected := make([]model.Citation, 0)
	for _, citation := range evidence {
		if strings.Contains(answer, "["+citation.ID+"]") {
			selected = append(selected, citation)
		}
	}
	return selected
}

func verifyAnswer(answer string, citations []model.Citation, workspace *repository.Workspace) error {
	if strings.TrimSpace(answer) == "" {
		return errors.New("citation gate rejected an empty answer")
	}
	if len(citations) == 0 {
		return errors.New("citation gate rejected an ungrounded answer")
	}
	for _, citation := range citations {
		if citation.StartLine < 1 || citation.EndLine < citation.StartLine || !workspace.ResolveCitation(citation.Path) {
			return fmt.Errorf("citation gate rejected invalid citation %s", citation.ID)
		}
	}
	return nil
}

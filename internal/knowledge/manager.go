package knowledge

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/security"
	"github.com/lizhao/codeatlas/internal/store"
)

type Manager struct {
	workspace *repository.Workspace
	store     *store.Store
}

func New(workspace *repository.Workspace, persistence *store.Store) *Manager {
	return &Manager{workspace: workspace, store: persistence}
}

func (m *Manager) Refresh(ctx context.Context) (*model.KnowledgeReport, error) {
	before := m.workspace.Info().Revision
	afterInfo, err := m.workspace.Refresh(ctx)
	if err != nil {
		return nil, err
	}
	if before == afterInfo.Revision {
		return &model.KnowledgeReport{FromRevision: before, ToRevision: afterInfo.Revision, ChangedFiles: []string{}, ImpactedModules: []string{}, SuggestedDocs: []string{}, Summary: "Repository revision is unchanged; no knowledge update is required.", Status: "no_change", CreatedAt: time.Now().UTC()}, nil
	}
	files, err := changedFiles(ctx, m.workspace.Root(), before, afterInfo.Revision)
	if err != nil {
		return nil, err
	}
	modules := uniqueTopLevel(files)
	suggestedDocs := suggestDocs(files, modules)
	report := &model.KnowledgeReport{
		ID: security.ID("knowledge"), FromRevision: before, ToRevision: afterInfo.Revision,
		ChangedFiles: files, ImpactedModules: modules, SuggestedDocs: suggestedDocs,
		Summary: fmt.Sprintf("Revision changed from %s to %s. Re-indexed repository metadata; %d file(s) across %d top-level module(s) require knowledge review.", short(before), short(afterInfo.Revision), len(files), len(modules)),
		Status:  "pending_human_review", CreatedAt: time.Now().UTC(),
	}
	if err := m.store.SaveKnowledgeReport(ctx, report); err != nil {
		return nil, err
	}
	return report, nil
}

func (m *Manager) Reports(ctx context.Context) ([]model.KnowledgeReport, error) {
	return m.store.ListKnowledgeReports(ctx)
}

func changedFiles(ctx context.Context, root, from, to string) ([]string, error) {
	if from == "" || from == "unknown" || to == "" || to == "unknown" {
		return []string{}, nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-only", from+".."+to, "--")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read revision diff: %w: %s", err, string(output))
	}
	result := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, filepath.ToSlash(strings.TrimSpace(line)))
		}
	}
	sort.Strings(result)
	return result, nil
}

func uniqueTopLevel(files []string) []string {
	seen := make(map[string]bool)
	for _, file := range files {
		part := strings.Split(filepath.ToSlash(file), "/")[0]
		if part != "" {
			seen[part] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func suggestDocs(files, modules []string) []string {
	seen := map[string]bool{"README.md": true}
	for _, file := range files {
		lower := strings.ToLower(file)
		if strings.HasSuffix(lower, ".thrift") || strings.HasSuffix(lower, ".proto") {
			seen["document/api-contracts.md"] = true
		}
		if strings.Contains(lower, "config") || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
			seen["document/deployment.md"] = true
		}
	}
	for _, module := range modules {
		seen["document/modules/"+module+".md"] = true
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func short(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

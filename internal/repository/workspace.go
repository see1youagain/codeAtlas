package repository

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
)

const maxReadableBytes = 1 << 20

var ignoredDirectories = map[string]bool{
	".git": true, ".idea": true, ".vscode": true, "node_modules": true,
	"vendor": true, "dist": true, "build": true, "bin": true,
	"kitex_gen": true, "__pycache__": true, ".venv": true,
}

type Workspace struct {
	root string
	mu   sync.RWMutex
	info model.RepositoryInfo
}

func New(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	stat, err := os.Stat(abs)
	if err != nil || !stat.IsDir() {
		return nil, fmt.Errorf("repository root is not a directory: %s", abs)
	}
	rootInfo, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("inspect repository root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("repository root cannot be a symbolic link")
	}
	w := &Workspace{root: filepath.Clean(abs)}
	if _, err := w.Refresh(context.Background()); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Workspace) Root() string { return w.root }

func (w *Workspace) Info() model.RepositoryInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info
}

func (w *Workspace) Refresh(ctx context.Context) (model.RepositoryInfo, error) {
	files := 0
	modules := 0
	services := make([]string, 0)
	entries, _ := os.ReadDir(w.root)
	for _, entry := range entries {
		if entry.IsDir() && !ignoredDirectories[entry.Name()] {
			services = append(services, entry.Name())
		}
	}
	err := filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != w.root && ignoredDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		files++
		if entry.Name() == "go.mod" {
			modules++
		}
		return nil
	})
	if err != nil {
		return model.RepositoryInfo{}, fmt.Errorf("scan repository: %w", err)
	}
	sort.Strings(services)
	info := model.RepositoryInfo{
		Name: filepath.Base(w.root), Root: w.root, Revision: w.revision(ctx),
		Files: files, GoModules: modules, Services: services,
		LastScanAt: time.Now().UTC().Format(time.RFC3339),
	}
	w.mu.Lock()
	w.info = info
	w.mu.Unlock()
	return info, nil
}

func (w *Workspace) ListFiles(relative string, limit int) ([]string, error) {
	base, err := w.resolve(relative)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	result := make([]string, 0, limit)
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != base && ignoredDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(result) >= limit {
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(w.root, path)
		result = append(result, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(result)
	return result, err
}

func (w *Workspace) Search(query, relative string, limit int) ([]model.Citation, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is required")
	}
	base, err := w.resolve(relative)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	needle := strings.ToLower(query)
	result := make([]model.Citation, 0, limit)
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != base && ignoredDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(result) >= limit || !isTextFile(path) {
			return nil
		}
		stat, statErr := entry.Info()
		if statErr != nil || stat.Size() > maxReadableBytes {
			return nil
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		line := 0
		for scanner.Scan() && len(result) < limit {
			line++
			text := scanner.Text()
			if strings.Contains(strings.ToLower(text), needle) {
				rel, _ := filepath.Rel(w.root, path)
				result = append(result, model.Citation{Path: filepath.ToSlash(rel), StartLine: line, EndLine: line, Snippet: strings.TrimSpace(text)})
			}
		}
		return nil
	})
	assignCitationIDs(result)
	return result, err
}

func (w *Workspace) ReadFile(relative string, start, end int) ([]model.Citation, error) {
	path, err := w.resolve(relative)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil || stat.IsDir() || stat.Size() > maxReadableBytes {
		return nil, fmt.Errorf("file is unavailable or too large: %s", relative)
	}
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start + 79
	}
	if end-start > 199 {
		end = start + 199
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := make([]string, 0, end-start+1)
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		if line >= start && line <= end {
			lines = append(lines, fmt.Sprintf("%d: %s", line, scanner.Text()))
		}
		if line > end {
			break
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("line range is outside file: %s", relative)
	}
	actualEnd := start + len(lines) - 1
	result := []model.Citation{{ID: "C1", Path: filepath.ToSlash(relative), StartLine: start, EndLine: actualEnd, Snippet: strings.Join(lines, "\n")}}
	return result, scanner.Err()
}

func (w *Workspace) ResolveCitation(path string) bool {
	_, err := w.resolve(path)
	return err == nil
}

func (w *Workspace) resolve(relative string) (string, error) {
	clean := filepath.Clean(relative)
	if clean == "." || clean == "" {
		return w.root, nil
	}
	if filepath.IsAbs(clean) {
		return "", errors.New("absolute paths are not allowed")
	}
	candidate := filepath.Join(w.root, clean)
	rel, err := filepath.Rel(w.root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository root")
	}
	for current := candidate; current != w.root; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symbolic links are not readable through repository tools")
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect repository path: %w", statErr)
		}
	}
	return candidate, nil
}

func (w *Workspace) revision(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "git", "-C", w.root, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func isTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".md", ".yaml", ".yml", ".json", ".toml", ".proto", ".thrift", ".sql", ".txt", ".sh", ".ps1", ".dockerfile":
		return true
	default:
		return filepath.Base(path) == "Dockerfile" || filepath.Base(path) == "Makefile"
	}
}

func assignCitationIDs(citations []model.Citation) {
	for i := range citations {
		citations[i].ID = fmt.Sprintf("C%d", i+1)
	}
}

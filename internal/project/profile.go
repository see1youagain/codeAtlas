package project

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizhao/codeatlas/internal/model"
)

type Profile struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	TestSuites     []model.TestSuite `json:"test_suites"`
	DeniedPrefixes []string          `json:"denied_prefixes"`
}

func Detect(repositoryRoot string) (*Profile, error) {
	profile := &Profile{
		Name:           "video-platform-microservice",
		Description:    "Go multi-module video platform with gateway, user, upload, manager and transcode services",
		DeniedPrefixes: []string{".git", "deploy/secrets", "runtime", ".env"},
	}
	err := filepath.WalkDir(repositoryRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "kitex_gen" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "go.mod" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(repositoryRoot, dir)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		idPart := strings.ReplaceAll(rel, "/", "-")
		if idPart == "." {
			idPart = "root"
		}
		profile.TestSuites = append(profile.TestSuites, model.TestSuite{
			ID: "go-test-" + idPart, Description: "Run unit tests for Go module " + rel,
			Directory: rel, Command: "go", Args: []string{"test", "./..."}, TimeoutSec: 180,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("detect project profile: %w", err)
	}
	sort.Slice(profile.TestSuites, func(i, j int) bool { return profile.TestSuites[i].ID < profile.TestSuites[j].ID })
	return profile, nil
}

func (p *Profile) Suite(id string) (model.TestSuite, bool) {
	for _, suite := range p.TestSuites {
		if suite.ID == id {
			return suite, true
		}
	}
	return model.TestSuite{}, false
}

func (p *Profile) PathAllowed(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, prefix := range p.DeniedPrefixes {
		prefix = strings.TrimSuffix(filepath.ToSlash(prefix), "/")
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return false
		}
	}
	return true
}

func EnsureDirectory(path string) error {
	return os.MkdirAll(path, 0o700)
}

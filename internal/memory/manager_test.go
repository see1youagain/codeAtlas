package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/store"
)

func fixture(t *testing.T) (*Manager, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("upload uses Kafka\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "README.md")
	git(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture")
	w, err := repository.New(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := New(s, w)
	return m, s, root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, output)
	}
}

func remember(t *testing.T, m *Manager) {
	t.Helper()
	saved, err := m.Remember(context.Background(), m.workspace.CurrentRevision(context.Background()), "run-1", "上传后怎么转码 Kafka", "通过 Kafka 触发转码 [C1]", []model.Citation{{ID: "C1", Path: "README.md", StartLine: 1, EndLine: 1}})
	if err != nil || !saved {
		t.Fatalf("remember: %t %v", saved, err)
	}
}

func TestMemoryRecallAndLifecycle(t *testing.T) {
	ctx := context.Background()
	m, s, _ := fixture(t)
	remember(t, m)
	// New manager proves this is persisted cross-task history, not local state.
	restarted := New(s, m.workspace)
	hints, count, err := restarted.Recall(ctx, "Kafka 上传转码")
	if err != nil || count != 1 || !strings.Contains(hints, "locations_to_recheck") {
		t.Fatalf("recall: %d %v %s", count, err, hints)
	}
	if _, count, err := restarted.Recall(ctx, "鉴权 JWT"); err != nil || count != 0 {
		t.Fatalf("irrelevant recall: %d %v", count, err)
	}
	records, err := restarted.List(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("list: %v %v", records, err)
	}
	if err := restarted.Delete(ctx, records[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, count, err := restarted.Recall(ctx, "Kafka 上传转码"); err != nil || count != 0 {
		t.Fatalf("deleted recall: %d %v", count, err)
	}
}

func TestMemoryStaleness(t *testing.T) {
	for _, kind := range []string{"expired", "stale_revision", "stale_evidence", "missing_file"} {
		t.Run(kind, func(t *testing.T) {
			m, _, root := fixture(t)
			remember(t, m)
			switch kind {
			case "expired":
				m.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
			case "stale_revision":
				git(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "new revision")
			case "stale_evidence":
				if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("upload now uses HTTP\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing_file":
				if err := os.Remove(filepath.Join(root, "README.md")); err != nil {
					t.Fatal(err)
				}
			}
			if _, count, err := m.Recall(context.Background(), "Kafka 上传转码"); err != nil || count != 0 {
				t.Fatalf("stale recall: %d %v", count, err)
			}
			records, err := m.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if records[0].Status == "active" {
				t.Fatal("stale record marked active")
			}
		})
	}
}

func TestRepositoryIsolationAndBoundedRetention(t *testing.T) {
	m, s, _ := fixture(t)
	remember(t, m)
	other, err := repository.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if records, err := New(s, other).List(context.Background()); err != nil || len(records) != 0 {
		t.Fatalf("repository leak: %v %v", records, err)
	}
	for i := 0; i < 103; i++ {
		remember(t, m)
	}
	records, err := m.List(context.Background())
	if err != nil || len(records) != 100 {
		t.Fatalf("retention: %d %v", len(records), err)
	}
}

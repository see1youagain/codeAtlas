package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConversationPersistsAndExpiresWithoutCrossSessionLeak(t *testing.T) {
	m, s, _ := fixture(t)
	ctx := context.Background()
	if err := m.RememberTurn(ctx, "session_one", "Kafka 上传流程", "Kafka 调用链 [C1]"); err != nil {
		t.Fatal(err)
	}
	restarted := New(s, m.workspace)
	prompt, count, err := restarted.Conversation(ctx, "session_one")
	if err != nil || count != 1 || !strings.Contains(prompt, "Kafka 调用链") {
		t.Fatalf("restore: %d %v", count, err)
	}
	if _, count, err := restarted.Conversation(ctx, "session_other"); err != nil || count != 0 {
		t.Fatal("cross-session leak")
	}
	restarted.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if _, count, err := restarted.Conversation(ctx, "session_one"); err != nil || count != 0 {
		t.Fatal("expired session reused")
	}
	if err := m.DeleteConversation(ctx, "session_one"); err != nil {
		t.Fatal(err)
	}
	if _, count, err := m.Conversation(ctx, "session_one"); err != nil || count != 0 {
		t.Fatal("deleted session reused")
	}
}

func TestConversationKeepsRecentTurnsAndValidatesIDs(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		if err := m.RememberTurn(ctx, "session_one", "上传", "回答"); err != nil {
			t.Fatal(err)
		}
	}
	if _, count, err := m.Conversation(ctx, "session_one"); err != nil || count != 4 {
		t.Fatalf("prompt turn bound: %d %v", count, err)
	}
	if err := m.RememberTurn(ctx, "../invalid", "question", "answer"); err == nil {
		t.Fatal("invalid session allowed")
	}
}

package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"busy"}}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()
	client := NewOpenAICompatibleClient(server.URL, "key", "model", 2*time.Second)
	result, err := client.Complete(context.Background(), "system", "user")
	if err != nil || result != "ok" || calls.Load() != 3 {
		t.Fatalf("unexpected retry result: %q %v calls=%d", result, err, calls.Load())
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"bad request"}}`)
	}))
	defer server.Close()
	client := NewOpenAICompatibleClient(server.URL, "key", "model", time.Second)
	client.consecutiveFails = 4
	if _, err := client.Complete(context.Background(), "system", "user"); err == nil {
		t.Fatal("expected model failure")
	}
	if _, err := client.Complete(context.Background(), "system", "user"); err == nil {
		t.Fatal("expected open breaker rejection")
	}
	if calls.Load() != 1 {
		t.Fatalf("open breaker should reject before network call, got %d calls", calls.Load())
	}
}

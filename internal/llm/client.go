package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

type breakerState string

const (
	breakerClosed   breakerState = "closed"
	breakerOpen     breakerState = "open"
	breakerHalfOpen breakerState = "half_open"
)

type OpenAICompatibleClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client

	mu               sync.Mutex
	state            breakerState
	consecutiveFails int
	openUntil        time.Time
	halfOpenInFlight bool
}

func NewOpenAICompatibleClient(baseURL, apiKey, model string, timeout time.Duration) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model,
		http: &http.Client{Timeout: timeout}, state: breakerClosed,
	}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("model returned %d: %s", e.status, e.message)
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("MODEL_API_KEY is not configured")
	}
	if err := c.acquireBreaker(); err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		content, err := c.completeOnce(ctx, systemPrompt, userPrompt)
		if err == nil {
			c.recordSuccess()
			return content, nil
		}
		lastErr = err
		if !retryable(err) || attempt == 2 {
			break
		}
		timer := time.NewTimer(time.Duration(250*(1<<attempt)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			lastErr = ctx.Err()
			attempt = 2
		case <-timer.C:
		}
	}
	c.recordFailure()
	return "", lastErr
}

func (c *OpenAICompatibleClient) completeOnce(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	payload := chatRequest{
		Model: c.model, Temperature: 0.1,
		Messages: []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("model request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("decode model response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := http.StatusText(resp.StatusCode)
		if decoded.Error != nil && decoded.Error.Message != "" {
			message = decoded.Error.Message
		}
		return "", &statusError{status: resp.StatusCode, message: message}
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", errors.New("model returned no content")
	}
	return decoded.Choices[0].Message.Content, nil
}

func (c *OpenAICompatibleClient) acquireBreaker() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.state == breakerOpen {
		if now.Before(c.openUntil) {
			return fmt.Errorf("model circuit breaker is open until %s", c.openUntil.UTC().Format(time.RFC3339))
		}
		c.state = breakerHalfOpen
	}
	if c.state == breakerHalfOpen {
		if c.halfOpenInFlight {
			return errors.New("model circuit breaker permits only one half-open probe")
		}
		c.halfOpenInFlight = true
	}
	return nil
}

func (c *OpenAICompatibleClient) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = breakerClosed
	c.consecutiveFails = 0
	c.halfOpenInFlight = false
}

func (c *OpenAICompatibleClient) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.halfOpenInFlight = false
	c.consecutiveFails++
	if c.state == breakerHalfOpen || c.consecutiveFails >= 5 {
		c.state = breakerOpen
		c.openUntil = time.Now().Add(30 * time.Second)
	}
}

func retryable(err error) bool {
	var status *statusError
	if errors.As(err, &status) {
		return status.status == http.StatusTooManyRequests || status.status >= 500
	}
	return strings.Contains(err.Error(), "model request failed")
}

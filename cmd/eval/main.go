package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
)

type dataset struct {
	Repository string     `json:"repository"`
	Cases      []evalCase `json:"cases"`
}

type evalCase struct {
	ID          string   `json:"id"`
	Question    string   `json:"question,omitempty"`
	Requirement string   `json:"requirement,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	MustCite    bool     `json:"must_cite,omitempty"`
	Focus       []string `json:"focus,omitempty"`
	MustInclude []string `json:"must_include,omitempty"`
}

type caseResult struct {
	ID            string   `json:"id"`
	Passed        bool     `json:"passed"`
	HTTPStatus    int      `json:"http_status"`
	CitationCount int      `json:"citation_count"`
	Matched       []string `json:"matched"`
	Missing       []string `json:"missing"`
	LatencyMS     int64    `json:"latency_ms"`
	Error         string   `json:"error,omitempty"`
}

type report struct {
	Repository string       `json:"repository"`
	BaseURL    string       `json:"base_url"`
	Passed     int          `json:"passed"`
	Total      int          `json:"total"`
	PassRate   float64      `json:"pass_rate"`
	CreatedAt  time.Time    `json:"created_at"`
	Cases      []caseResult `json:"cases"`
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:8080", "running CodeAtlas server")
	datasetPath := flag.String("dataset", "evals/video-platform-qa.json", "evaluation dataset")
	outputPath := flag.String("out", "artifacts/eval-reports/latest.json", "report output")
	flag.Parse()
	raw, err := os.ReadFile(*datasetPath)
	check(err)
	var data dataset
	check(json.Unmarshal(raw, &data))
	result := report{Repository: data.Repository, BaseURL: strings.TrimRight(*baseURL, "/"), Total: len(data.Cases), CreatedAt: time.Now().UTC(), Cases: make([]caseResult, 0, len(data.Cases))}
	client := &http.Client{Timeout: 3 * time.Minute}
	for _, item := range data.Cases {
		caseResult := runCase(client, result.BaseURL, item)
		result.Cases = append(result.Cases, caseResult)
		if caseResult.Passed {
			result.Passed++
		}
	}
	if result.Total > 0 {
		result.PassRate = float64(result.Passed) / float64(result.Total)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	check(err)
	check(os.MkdirAll(filepathDir(*outputPath), 0o700))
	check(os.WriteFile(*outputPath, encoded, 0o600))
	fmt.Printf("CodeAtlas eval: %d/%d passed (%.1f%%)\nreport: %s\n", result.Passed, result.Total, result.PassRate*100, *outputPath)
	if result.Passed != result.Total {
		os.Exit(1)
	}
}

func runCase(client *http.Client, baseURL string, item evalCase) caseResult {
	started := time.Now()
	result := caseResult{ID: item.ID, Matched: []string{}, Missing: []string{}}
	path := "/api/v1/ask"
	payload := any(model.AskRequest{Question: item.Question})
	if item.Mode == "plan" || item.Requirement != "" {
		path = "/api/v1/plan"
		payload = model.PlanRequest{Requirement: item.Requirement}
	}
	body, _ := json.Marshal(payload)
	response, err := client.Post(baseURL+path, "application/json", bytes.NewReader(body))
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Error = string(raw)
		return result
	}
	var decoded struct {
		Answer    string           `json:"answer"`
		Plan      string           `json:"plan"`
		Citations []model.Citation `json:"citations"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		result.Error = err.Error()
		return result
	}
	result.CitationCount = len(decoded.Citations)
	text := strings.ToLower(decoded.Answer + "\n" + decoded.Plan)
	required := append(append([]string{}, item.Focus...), item.MustInclude...)
	for _, keyword := range required {
		if strings.Contains(text, strings.ToLower(keyword)) {
			result.Matched = append(result.Matched, keyword)
		} else {
			result.Missing = append(result.Missing, keyword)
		}
	}
	result.Passed = len(result.Missing) == 0 && (!item.MustCite || result.CitationCount > 0)
	return result
}

func filepathDir(path string) string {
	index := strings.LastIndexAny(path, `/\`)
	if index < 0 {
		return "."
	}
	return path[:index]
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

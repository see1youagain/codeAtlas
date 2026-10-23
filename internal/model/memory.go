package model

import "time"

type ConversationTurn struct {
	RepositoryID string    `json:"repository_id"`
	SessionID    string    `json:"session_id"`
	Question     string    `json:"question"`
	Answer       string    `json:"answer"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ResearchMemory is an episodic investigation hint, never an approval or fact.
type ResearchMemory struct {
	ID           string           `json:"id"`
	RepositoryID string           `json:"repository_id"`
	Revision     string           `json:"revision"`
	SourceRunID  string           `json:"source_run_id"`
	Question     string           `json:"question"`
	Summary      string           `json:"summary"`
	Evidence     []MemoryEvidence `json:"evidence"`
	CreatedAt    time.Time        `json:"created_at"`
	ExpiresAt    time.Time        `json:"expires_at"`
	Status       string           `json:"status"`
}

type MemoryEvidence struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	FileHash  string `json:"file_hash"`
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lizhao/codeatlas/internal/model"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("record not found")

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", abs)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS plans (id TEXT PRIMARY KEY, status TEXT NOT NULL, updated_at TEXT NOT NULL, payload BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS approvals (id TEXT PRIMARY KEY, plan_id TEXT NOT NULL UNIQUE, token_hash TEXT NOT NULL, created_at TEXT NOT NULL, payload BLOB NOT NULL, FOREIGN KEY(plan_id) REFERENCES plans(id))`,
		`CREATE TABLE IF NOT EXISTS executions (id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, status TEXT NOT NULL, updated_at TEXT NOT NULL, payload BLOB NOT NULL, FOREIGN KEY(plan_id) REFERENCES plans(id))`,
		`CREATE TABLE IF NOT EXISTS knowledge_reports (id TEXT PRIMARY KEY, created_at TEXT NOT NULL, payload BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events (sequence INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, created_at TEXT NOT NULL, payload BLOB NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_events_run_id ON events(run_id, sequence)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	return nil
}

func (s *Store) SavePlan(ctx context.Context, plan *model.ChangePlan) error {
	payload, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO plans(id,status,updated_at,payload) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at,payload=excluded.payload`,
		plan.ID, string(plan.Status), plan.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func (s *Store) GetPlan(ctx context.Context, id string) (*model.ChangePlan, error) {
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT payload FROM plans WHERE id=?`, id).Scan(&payload); err != nil {
		return nil, normalizeNotFound(err)
	}
	var plan model.ChangePlan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *Store) ListPlans(ctx context.Context) ([]model.ChangePlan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM plans ORDER BY updated_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.ChangePlan, 0)
	for rows.Next() {
		var payload []byte
		var plan model.ChangePlan
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &plan); err != nil {
			return nil, err
		}
		result = append(result, plan)
	}
	return result, rows.Err()
}

func (s *Store) SaveApproval(ctx context.Context, approval *model.Approval) error {
	payload, err := json.Marshal(approval)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO approvals(id,plan_id,token_hash,created_at,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(plan_id) DO UPDATE SET id=excluded.id,token_hash=excluded.token_hash,created_at=excluded.created_at,payload=excluded.payload`,
		approval.ID, approval.PlanID, approval.TokenHash, approval.CreatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func (s *Store) GetApprovalByPlan(ctx context.Context, planID string) (*model.Approval, error) {
	var payload []byte
	var tokenHash string
	if err := s.db.QueryRowContext(ctx, `SELECT payload,token_hash FROM approvals WHERE plan_id=?`, planID).Scan(&payload, &tokenHash); err != nil {
		return nil, normalizeNotFound(err)
	}
	var approval model.Approval
	if err := json.Unmarshal(payload, &approval); err != nil {
		return nil, err
	}
	approval.TokenHash = tokenHash
	return &approval, nil
}

func (s *Store) SaveExecution(ctx context.Context, execution *model.Execution) error {
	payload, err := json.Marshal(execution)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO executions(id,plan_id,status,updated_at,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at,payload=excluded.payload`,
		execution.ID, execution.PlanID, execution.Status, time.Now().UTC().Format(time.RFC3339Nano), payload)
	return err
}

func (s *Store) GetExecution(ctx context.Context, id string) (*model.Execution, error) {
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT payload FROM executions WHERE id=?`, id).Scan(&payload); err != nil {
		return nil, normalizeNotFound(err)
	}
	var execution model.Execution
	if err := json.Unmarshal(payload, &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}

func (s *Store) SaveKnowledgeReport(ctx context.Context, report *model.KnowledgeReport) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO knowledge_reports(id,created_at,payload) VALUES(?,?,?)`, report.ID, report.CreatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func (s *Store) ListKnowledgeReports(ctx context.Context) ([]model.KnowledgeReport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM knowledge_reports ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.KnowledgeReport, 0)
	for rows.Next() {
		var payload []byte
		var report model.KnowledgeReport
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &report); err != nil {
			return nil, err
		}
		result = append(result, report)
	}
	return result, rows.Err()
}

func (s *Store) AppendEvent(ctx context.Context, runID string, event model.TraceEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO events(run_id,created_at,payload) VALUES(?,?,?)`, runID, event.At.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func (s *Store) Events(ctx context.Context, runID string) ([]model.TraceEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM events WHERE run_id=? ORDER BY sequence`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.TraceEvent, 0)
	for rows.Next() {
		var payload []byte
		var event model.TraceEvent
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

// RecoverInterrupted never replays write side effects. It marks runs that were
// in progress during a process crash as failed so a human can inspect the
// retained worktree and create a fresh plan/approval.
func (s *Store) RecoverInterrupted(ctx context.Context) error {
	plans, err := s.plansByStatus(ctx, string(model.PlanRunning))
	if err != nil {
		return err
	}
	for index := range plans {
		plans[index].Status = model.PlanFailed
		plans[index].UpdatedAt = time.Now().UTC()
		if err := s.SavePlan(ctx, &plans[index]); err != nil {
			return err
		}
	}
	executions, err := s.executionsByNonTerminalStatus(ctx)
	if err != nil {
		return err
	}
	for index := range executions {
		executions[index].Status = "failed"
		executions[index].Error = "process stopped during execution; side effects were not replayed automatically"
		now := time.Now().UTC()
		executions[index].FinishedAt = &now
		if err := s.SaveExecution(ctx, &executions[index]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) plansByStatus(ctx context.Context, status string) ([]model.ChangePlan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM plans WHERE status=?`, status)
	if err != nil {
		return nil, err
	}
	var result []model.ChangePlan
	for rows.Next() {
		var payload []byte
		var plan model.ChangePlan
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(payload, &plan); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, plan)
	}
	err = rows.Err()
	rows.Close()
	return result, err
}

func (s *Store) executionsByNonTerminalStatus(ctx context.Context) ([]model.Execution, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM executions WHERE status NOT IN ('completed','failed','discarded')`)
	if err != nil {
		return nil, err
	}
	var result []model.Execution
	for rows.Next() {
		var payload []byte
		var execution model.Execution
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(payload, &execution); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, execution)
	}
	err = rows.Err()
	rows.Close()
	return result, err
}

func normalizeNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

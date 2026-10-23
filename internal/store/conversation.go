package store

import (
	"context"
	"encoding/json"
	"github.com/lizhao/codeatlas/internal/model"
	"time"
)

func (s *Store) SaveTurn(ctx context.Context, turn model.ConversationTurn) error {
	payload, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM conversation_turns WHERE expires_at<=?`, turn.CreatedAt.Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO conversation_turns(repository_id,session_id,created_at,expires_at,payload) VALUES(?,?,?,?,?)`, turn.RepositoryID, turn.SessionID, turn.CreatedAt.Unix(), turn.ExpiresAt.Unix(), payload); err != nil {
		return err
	}
	// At most ten turns per session and one thousand turns per repository.
	if _, err = tx.ExecContext(ctx, `DELETE FROM conversation_turns WHERE repository_id=? AND session_id=? AND sequence NOT IN (SELECT sequence FROM conversation_turns WHERE repository_id=? AND session_id=? ORDER BY sequence DESC LIMIT 10)`, turn.RepositoryID, turn.SessionID, turn.RepositoryID, turn.SessionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM conversation_turns WHERE repository_id=? AND sequence NOT IN (SELECT sequence FROM conversation_turns WHERE repository_id=? ORDER BY sequence DESC LIMIT 1000)`, turn.RepositoryID, turn.RepositoryID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Turns(ctx context.Context, repositoryID, sessionID string, now time.Time) ([]model.ConversationTurn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM conversation_turns WHERE repository_id=? AND session_id=? AND expires_at>? ORDER BY sequence DESC LIMIT 4`, repositoryID, sessionID, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.ConversationTurn{}
	for rows.Next() {
		var payload []byte
		var turn model.ConversationTurn
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &turn); err != nil {
			return nil, err
		}
		result = append(result, turn)
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, rows.Err()
}

func (s *Store) DeleteConversation(ctx context.Context, repositoryID, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversation_turns WHERE repository_id=? AND session_id=?`, repositoryID, sessionID)
	return err
}

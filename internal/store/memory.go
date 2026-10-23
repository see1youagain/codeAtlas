package store

import (
	"context"
	"encoding/json"
	"github.com/lizhao/codeatlas/internal/model"
)

func (s *Store) SaveMemory(ctx context.Context, m *model.ResearchMemory) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO research_memories(id,repository_id,created_at,payload) VALUES(?,?,?,?)`, m.ID, m.RepositoryID, m.CreatedAt.UnixNano(), payload)
	return err
}

// Bound both stored history and prompt retrieval work for the single-user MVP.
func (s *Store) PruneMemories(ctx context.Context, repositoryID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM research_memories WHERE repository_id=? AND id NOT IN
		(SELECT id FROM research_memories WHERE repository_id=? ORDER BY created_at DESC,id DESC LIMIT 100)`, repositoryID, repositoryID)
	return err
}

func (s *Store) Memories(ctx context.Context, repositoryID string) ([]model.ResearchMemory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM research_memories WHERE repository_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.ResearchMemory{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var m model.ResearchMemory
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *Store) DeleteMemory(ctx context.Context, repositoryID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM research_memories WHERE repository_id=? AND id=?`, repositoryID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

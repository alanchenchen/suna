package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type EntityStore struct {
	db *sql.DB
}

func NewEntityStore(db *sql.DB) *EntityStore {
	return &EntityStore{db: db}
}

type Entity struct {
	Name      string
	MemoryIDs []string
	UpdatedAt time.Time
}

func (s *EntityStore) Store(ctx context.Context, name string, memoryID string) error {
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT memory_ids FROM entities WHERE name = ?`, name).Scan(&existing)
	if err == sql.ErrNoRows {
		ids, _ := json.Marshal([]string{memoryID})
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO entities (name, memory_ids, updated_at)
			VALUES (?, ?, ?)`,
			name, string(ids), time.Now(),
		)
		return err
	}
	if err != nil {
		return fmt.Errorf("query entity: %w", err)
	}

	var ids []string
	json.Unmarshal([]byte(existing), &ids)
	for _, id := range ids {
		if id == memoryID {
			return nil
		}
	}
	ids = append(ids, memoryID)
	if len(ids) > 50 {
		ids = ids[len(ids)-50:]
	}
	idsJSON, _ := json.Marshal(ids)
	_, err = s.db.ExecContext(ctx, `
		UPDATE entities SET memory_ids = ?, updated_at = ? WHERE name = ?`,
		string(idsJSON), time.Now(), name,
	)
	return err
}

func (s *EntityStore) StoreBatch(ctx context.Context, entities []string, memoryID string) error {
	for _, e := range entities {
		if err := s.Store(ctx, e, memoryID); err != nil {
			return err
		}
	}
	return nil
}

func (s *EntityStore) List(ctx context.Context, limit int) ([]*Entity, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, memory_ids, updated_at
		FROM entities
		ORDER BY updated_at DESC
		LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()

	var entities []*Entity
	for rows.Next() {
		e := &Entity{}
		var idsJSON string
		var ts string
		if err := rows.Scan(&e.Name, &idsJSON, &ts); err != nil {
			continue
		}
		json.Unmarshal([]byte(idsJSON), &e.MemoryIDs)
		e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) Search(ctx context.Context, query string, limit int) ([]*Entity, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, memory_ids, updated_at
		FROM entities
		WHERE name LIKE ?
		ORDER BY updated_at DESC
		LIMIT ?`, "%"+query+"%", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search entities: %w", err)
	}
	defer rows.Close()

	var entities []*Entity
	for rows.Next() {
		e := &Entity{}
		var idsJSON string
		var ts string
		if err := rows.Scan(&e.Name, &idsJSON, &ts); err != nil {
			continue
		}
		json.Unmarshal([]byte(idsJSON), &e.MemoryIDs)
		e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) TopEntities(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM entities
		ORDER BY updated_at DESC
		LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func (s *EntityStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities").Scan(&count)
	return count, err
}

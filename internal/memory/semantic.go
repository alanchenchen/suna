package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type SemanticFact struct {
	ID     int64
	Type   string
	Key    string
	Value  string
	Source string
	Ts     time.Time
}

type SemanticStore struct {
	db *sql.DB
}

func NewSemanticStore(db *sql.DB) *SemanticStore {
	return &SemanticStore{db: db}
}

func (s *SemanticStore) Store(ctx context.Context, factType, key, value, source string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO semantic_facts (type, key, value, source, ts)
		VALUES (?, ?, ?, ?, ?)`,
		factType, key, value, source, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("insert semantic fact: %w", err)
	}
	return nil
}

func (s *SemanticStore) Latest(ctx context.Context, factType, key string) (*SemanticFact, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, key, value, source, ts
		FROM semantic_facts
		WHERE type = ? AND key = ?
		ORDER BY ts DESC
		LIMIT 1`,
		factType, key,
	)
	return scanFact(row)
}

func (s *SemanticStore) GetAll(ctx context.Context, factType string) ([]*SemanticFact, error) {
	query := `
		SELECT id, type, key, value, source, ts
		FROM semantic_facts`
	args := []any{}
	if factType != "" {
		query += ` WHERE type = ?`
		args = append(args, factType)
	}
	query += ` ORDER BY ts DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query semantic facts: %w", err)
	}
	defer rows.Close()

	var facts []*SemanticFact
	for rows.Next() {
		fact, err := scanFactRow(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func (s *SemanticStore) Summary(ctx context.Context) (string, error) {
	facts, err := s.GetAll(ctx, "preference")
	if err != nil {
		return "", err
	}
	if len(facts) == 0 {
		return "", nil
	}
	latest := make(map[string]string)
	for _, f := range facts {
		if _, seen := latest[f.Key]; !seen {
			latest[f.Key] = f.Value
		}
	}
	result := ""
	for k, v := range latest {
		result += fmt.Sprintf("- %s: %s\n", k, v)
	}
	return result, nil
}

func scanFact(row *sql.Row) (*SemanticFact, error) {
	fact := &SemanticFact{}
	var ts string
	err := row.Scan(&fact.ID, &fact.Type, &fact.Key, &fact.Value, &fact.Source, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fact.Ts, _ = time.Parse("2006-01-02 15:04:05", ts)
	return fact, nil
}

func scanFactRow(rows *sql.Rows) (*SemanticFact, error) {
	fact := &SemanticFact{}
	var ts string
	err := rows.Scan(&fact.ID, &fact.Type, &fact.Key, &fact.Value, &fact.Source, &ts)
	if err != nil {
		return nil, err
	}
	fact.Ts, _ = time.Parse("2006-01-02 15:04:05", ts)
	return fact, nil
}

package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

// Store 写入一条语义事实。仅添加式：不覆盖旧事实，新旧共存。
// 同 key 的新旧事实通过 ts 区分，读取时按 ts DESC 取最新。
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

func (s *SemanticStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM semantic_facts").Scan(&count)
	return count, err
}

// Summary 返回用户知识和偏好的摘要文本，用于注入 system prompt。
// 设计原则（06-memory.md）：新旧事实共存，用时间戳区分，LLM 做时间推理。
// 同 key 最多保留最近 3 条（新+旧），总条数限制。
func (s *SemanticStore) Summary(ctx context.Context) (string, error) {
	const maxPerKey = 3
	const maxTotal = 20

	facts, err := s.GetAll(ctx, "")
	if err != nil {
		return "", err
	}
	if len(facts) == 0 {
		return "", nil
	}

	// 同 key 最多保留 maxPerKey 条（GetAll 已按 ts DESC）
	countPerKey := make(map[string]int)
	var lines []string
	for _, f := range facts {
		if countPerKey[f.Key] >= maxPerKey {
			continue
		}
		countPerKey[f.Key]++
		dateStr := f.Ts.Format("2006-01-02")
		lines = append(lines, fmt.Sprintf("- %s: %s (%s, %s)", f.Key, f.Value, f.Source, dateStr))
		if len(lines) >= maxTotal {
			break
		}
	}

	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n"), nil
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

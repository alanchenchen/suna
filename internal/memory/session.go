package memory

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// SessionMeta 是持久化的会话元信息；运行态 status/client_count 由 daemon 内存派生。
type SessionMeta struct {
	ID             string
	Title          string
	CWD            string
	MessageCount   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastAttachedAt time.Time
}

type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(db *sql.DB) *SessionStore { return &SessionStore{db: db} }

func (s *SessionStore) Create(ctx context.Context, meta SessionMeta) error {
	now := time.Now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, title, cwd, message_count, created_at, updated_at, last_attached_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL)`, meta.ID, strings.TrimSpace(meta.Title), strings.TrimSpace(meta.CWD), meta.MessageCount, meta.CreatedAt, meta.UpdatedAt)
	return err
}

func (s *SessionStore) Get(ctx context.Context, id string) (*SessionMeta, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, title, cwd, message_count, created_at, updated_at, last_attached_at FROM sessions WHERE id = ?`, id)
	meta, err := scanSessionMeta(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return meta, nil
}

func (s *SessionStore) List(ctx context.Context) ([]SessionMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, cwd, message_count, created_at, updated_at, last_attached_at FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionMeta
	for rows.Next() {
		meta, err := scanSessionMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *meta)
	}
	return out, rows.Err()
}

func (s *SessionStore) UpdateTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(title), time.Now(), id)
	return err
}

func (s *SessionStore) TouchAttached(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_attached_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

func (s *SessionStore) SetMessageCount(ctx context.Context, id string, count int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET message_count = ?, updated_at = ? WHERE id = ?`, count, time.Now(), id)
	return err
}

func (s *SessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *SessionStore) PruneInactive(ctx context.Context, olderThan time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions WHERE message_count > 0 AND updated_at < ?`, olderThan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

func scanSessionMeta(scanner interface{ Scan(dest ...any) error }) (*SessionMeta, error) {
	var meta SessionMeta
	var created, updated, attached sql.NullString
	if err := scanner.Scan(&meta.ID, &meta.Title, &meta.CWD, &meta.MessageCount, &created, &updated, &attached); err != nil {
		return nil, err
	}
	meta.CreatedAt = parseDBTime(created.String)
	meta.UpdatedAt = parseDBTime(updated.String)
	meta.LastAttachedAt = parseDBTime(attached.String)
	return &meta, nil
}

// UsageStore 记录模型调用用量。它和会话元信息分离，避免和 SessionStore 语义混淆。
type UsageStore struct {
	db *sql.DB
}

func NewUsageStore(db *sql.DB) *UsageStore { return &UsageStore{db: db} }

// SaveUsage records one LLM call's token usage.
func (s *UsageStore) SaveUsage(ctx context.Context, sessionID, model string, inputTokens, outputTokens int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_log (session_id, model, input_tokens, output_tokens, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		sessionID, model, inputTokens, outputTokens, time.Now(),
	)
	return err
}

func (s *UsageStore) UsageSummary(ctx context.Context, since time.Time) (*UsageSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COUNT(*)
		FROM usage_log
		WHERE created_at >= ?`, since)
	var out UsageSummary
	if err := row.Scan(&out.InputTokens, &out.OutputTokens, &out.Requests); err != nil {
		return nil, err
	}
	return &out, nil
}

type UsageSummary struct {
	InputTokens  int
	OutputTokens int
	Requests     int
}

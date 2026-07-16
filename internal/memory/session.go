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
	ModelRef       string
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
		INSERT INTO sessions (id, title, cwd, model_ref, message_count, created_at, updated_at, last_attached_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, NULL)`, meta.ID, strings.TrimSpace(meta.Title), strings.TrimSpace(meta.CWD), strings.TrimSpace(meta.ModelRef), meta.MessageCount, meta.CreatedAt, meta.UpdatedAt)
	return err
}

func (s *SessionStore) Get(ctx context.Context, id string) (*SessionMeta, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, title, cwd, model_ref, message_count, created_at, updated_at, last_attached_at FROM sessions WHERE id = ?`, id)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, cwd, model_ref, message_count, created_at, updated_at, last_attached_at FROM sessions ORDER BY updated_at DESC`)
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

// UpdateMetadata 在同一 SQLite 事务中更新请求指定的会话元数据字段。
// 调用者应先完成模型引用校验，确保持久化元数据与运行态切换可按顺序发布。
func (s *SessionStore) UpdateMetadata(ctx context.Context, id, title string, titleSet bool, modelRef string, modelRefSet bool) error {
	if !titleSet && !modelRefSet {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	var result sql.Result
	switch {
	case titleSet && modelRefSet:
		result, err = tx.ExecContext(ctx, `
			UPDATE sessions
			SET title = ?, model_ref = NULLIF(?, ''), updated_at = ?
			WHERE id = ?`, strings.TrimSpace(title), strings.TrimSpace(modelRef), now, id)
	case titleSet:
		result, err = tx.ExecContext(ctx, `
			UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(title), now, id)
	case modelRefSet:
		result, err = tx.ExecContext(ctx, `
			UPDATE sessions SET model_ref = NULLIF(?, ''), updated_at = ? WHERE id = ?`, strings.TrimSpace(modelRef), now, id)
	}
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// MaterializeModelRefIfEmpty 只为尚未选择模型的旧会话固化一次模型引用。
// 返回值表示本次调用是否取得了固化权；并发调用者必须重新读取最终值。
func (s *SessionStore) MaterializeModelRefIfEmpty(ctx context.Context, id, modelRef string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET model_ref = ?, updated_at = ?
		WHERE id = ? AND (model_ref IS NULL OR TRIM(model_ref) = '')`, strings.TrimSpace(modelRef), time.Now(), id)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}

func (s *SessionStore) TouchAttached(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_attached_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

func (s *SessionStore) SetMessageCount(ctx context.Context, id string, count int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET message_count = ?, updated_at = ? WHERE id = ?`, count, time.Now(), id)
	return err
}

// DeleteWithState 在同一事务中删除会话元信息与工作状态。附件属于文件系统资源，
// 由 daemon 在事务成功后清理，避免持久化删除失败时错误删除用户附件。
func (s *SessionStore) DeleteWithState(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_state WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func scanSessionMeta(scanner interface{ Scan(dest ...any) error }) (*SessionMeta, error) {
	var meta SessionMeta
	var created, updated, attached, modelRef sql.NullString
	if err := scanner.Scan(&meta.ID, &meta.Title, &meta.CWD, &modelRef, &meta.MessageCount, &created, &updated, &attached); err != nil {
		return nil, err
	}
	meta.ModelRef = modelRef.String
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

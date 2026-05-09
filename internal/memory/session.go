package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SessionStore 管理会话持久化。
// 每轮对话结束后保存消息到 session_messages 表，
// 启动时从最近的 active 会话恢复。
type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(db *sql.DB) *SessionStore {
	return &SessionStore{db: db}
}

// CreateSession 创建新会话记录
func (s *SessionStore) CreateSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO sessions (id, status, created_at, updated_at)
		VALUES (?, 'active', ?, ?)`,
		sessionID, time.Now(), time.Now(),
	)
	return err
}

// SaveMessage 保存一条消息到当前会话
func (s *SessionStore) SaveMessage(ctx context.Context, sessionID string, turn int, role, content string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_messages (session_id, turn, role, content, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		sessionID, turn, role, content, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("save session message: %w", err)
	}
	// 更新 session 的 updated_at
	_, err = s.db.ExecContext(ctx, `
		UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now(), sessionID,
	)
	return err
}

// SaveToolCall 保存工具调用记录
func (s *SessionStore) SaveToolCall(ctx context.Context, sessionID string, turn int, toolCall, toolResult string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_messages (session_id, turn, role, content, tool_call, tool_result, created_at)
		VALUES (?, ?, 'tool', '', ?, ?, ?)`,
		sessionID, turn, toolCall, toolResult, time.Now(),
	)
	return err
}

// SaveMessageWithSignificance 保存消息并附带显著性标记
func (s *SessionStore) SaveMessageWithSignificance(ctx context.Context, sessionID string, turn int, role, content, significance string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_messages (session_id, turn, role, content, significance, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, turn, role, content, significance, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("save session message: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now(), sessionID,
	)
	return err
}

// LoadMessages 加载会话的所有消息，按时间排序
func (s *SessionStore) LoadMessages(ctx context.Context, sessionID string) ([]SessionMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, turn, role, content, tool_call, tool_result, created_at
		FROM session_messages
		WHERE session_id = ?
		ORDER BY turn ASC, created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("load session messages: %w", err)
	}
	defer rows.Close()

	var msgs []SessionMessage
	for rows.Next() {
		var m SessionMessage
		var ts string
		err := rows.Scan(&m.SessionID, &m.Turn, &m.Role, &m.Content, &m.ToolCall, &m.ToolResult, &ts)
		if err != nil {
			continue
		}
		m.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// LastActiveSession 查找最近的 active 会话
func (s *SessionStore) LastActiveSession(ctx context.Context) (*SessionInfo, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, updated_at, summary, status
		FROM sessions
		WHERE status = 'active'
		ORDER BY updated_at DESC
		LIMIT 1`,
	)
	var info SessionInfo
	var created, updated string
	err := row.Scan(&info.ID, &created, &updated, &info.Summary, &info.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	info.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updated)
	return &info, nil
}

// CompleteSession 标记会话为已完成
func (s *SessionStore) CompleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = 'completed', updated_at = ? WHERE id = ?`,
		time.Now(), sessionID,
	)
	return err
}

// CompleteOtherSessions 将除 keepID 外的所有 active sessions 标记为 completed
func (s *SessionStore) CompleteOtherSessions(ctx context.Context, keepID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = 'completed', updated_at = ?
		WHERE status = 'active' AND id != ?`,
		time.Now(), keepID,
	)
	return err
}

// SaveUsage 记录一次 LLM 调用的 token 使用量
func (s *SessionStore) SaveUsage(ctx context.Context, sessionID, model string, inputTokens, outputTokens int, cost float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_log (session_id, model, input_tokens, output_tokens, cost, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, model, inputTokens, outputTokens, cost, time.Now(),
	)
	return err
}

// UsageSummary 返回指定时间段的用量汇总
func (s *SessionStore) UsageSummary(ctx context.Context, since time.Time) (*UsageSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cost), 0),
			COUNT(*)
		FROM usage_log
		WHERE created_at >= ?`, since,
	)
	var sum UsageSummary
	err := row.Scan(&sum.InputTokens, &sum.OutputTokens, &sum.Cost, &sum.Requests)
	if err != nil {
		return nil, err
	}
	return &sum, nil
}

func (s *SessionStore) ExpireOldSessions(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge)
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = 'completed', updated_at = ?
		WHERE status = 'active' AND updated_at < ?`,
		time.Now(), cutoff,
	)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (s *SessionStore) CountByStatus(ctx context.Context, status string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE status = ?", status).Scan(&count)
	return count, err
}

func (s *SessionStore) LoadUnextractedMessages(ctx context.Context, sessionID string, limit int) ([]SessionMessage, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, turn, role, content, tool_call, tool_result, created_at
		FROM session_messages
		WHERE session_id = ? AND (memory_extracted = 0 OR memory_extracted IS NULL)
		ORDER BY turn DESC
		LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []SessionMessage
	for rows.Next() {
		var m SessionMessage
		var ts string
		err := rows.Scan(&m.SessionID, &m.Turn, &m.Role, &m.Content, &m.ToolCall, &m.ToolResult, &ts)
		if err != nil {
			continue
		}
		m.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

type SessionInfo struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
	Summary   string
	Status    string
}

type SessionMessage struct {
	SessionID  string
	Turn       int
	Role       string
	Content    string
	ToolCall   string
	ToolResult string
	Timestamp  time.Time
}

type UsageSummary struct {
	InputTokens  int
	OutputTokens int
	Cost         float64
	Requests     int
}

// marshalJSON 工具函数
func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

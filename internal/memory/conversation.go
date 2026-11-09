package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SessionState struct {
	SessionID string
	Summary   string
	Messages  []Message
	UpdatedAt time.Time
}

type SessionStateStore struct{ db *sql.DB }

func NewSessionStateStore(db *sql.DB) *SessionStateStore { return &SessionStateStore{db: db} }

func (s *SessionStateStore) Load(ctx context.Context, sessionID string) (*SessionState, error) {
	var state SessionState
	var raw string
	var updated sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT session_id, summary, messages, updated_at FROM session_state WHERE session_id = ?`, sessionID).Scan(&state.SessionID, &state.Summary, &raw, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &state.Messages); err != nil {
		return nil, fmt.Errorf("decode session messages: %w", err)
	}
	state.UpdatedAt = parseDBTime(updated.String)
	return &state, nil
}

func (s *SessionStateStore) Save(ctx context.Context, sessionID, summary string, messages []Message) error {
	// 直接编码到字符串，避免 Marshal 的字节副本再转换字符串，减少长历史检查点的大块分配。
	var raw strings.Builder
	err := json.NewEncoder(&raw).Encode(ProjectMessages(messages))
	if err != nil {
		return fmt.Errorf("encode session messages: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO session_state (session_id, summary, messages, updated_at) VALUES (?, ?, ?, ?)
 ON CONFLICT(session_id) DO UPDATE SET summary=excluded.summary, messages=excluded.messages, updated_at=excluded.updated_at`, sessionID, summary, strings.TrimSuffix(raw.String(), "\n"), dbTime(time.Now()))
	return err
}
func (s *SessionStateStore) Clear(ctx context.Context, sessionID string) error {
	return s.Save(ctx, sessionID, "", nil)
}
func (s *SessionStateStore) Delete(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_state WHERE session_id = ?`, sessionID)
	return err
}

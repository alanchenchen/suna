package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

type SessionState struct {
	SessionID    string
	Compacted    string
	LastMessages []model.Message
	ToolSummary  ToolSummary
	UpdatedAt    time.Time
}

type SessionStateStore struct {
	db *sql.DB
}

func NewSessionStateStore(db *sql.DB) *SessionStateStore { return &SessionStateStore{db: db} }

func (s *SessionStateStore) Load(ctx context.Context, sessionID string) (*SessionState, error) {
	row := s.db.QueryRowContext(ctx, `SELECT session_id, compacted_state, last_messages, tool_summary, updated_at FROM session_state WHERE session_id = ?`, sessionID)
	var st SessionState
	var lastMessages, toolSummary string
	var updated sql.NullString
	if err := row.Scan(&st.SessionID, &st.Compacted, &lastMessages, &toolSummary, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(lastMessages), &st.LastMessages)
	st.ToolSummary = decodeToolSummary(toolSummary)
	st.UpdatedAt = parseDBTime(updated.String)
	return &st, nil
}

func (s *SessionStateStore) Save(ctx context.Context, sessionID, compacted string, msgs []model.Message, tools ToolSummary) error {
	msgs = visibleMessages(msgs)
	msgJSON, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	toolJSON, err := json.Marshal(tools.Normalize())
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO session_state (session_id, compacted_state, last_messages, tool_summary, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			compacted_state = excluded.compacted_state,
			last_messages = excluded.last_messages,
			tool_summary = excluded.tool_summary,
			updated_at = excluded.updated_at`, sessionID, strings.TrimSpace(compacted), string(msgJSON), string(toolJSON), now)
	return err
}

func (s *SessionStateStore) Clear(ctx context.Context, sessionID string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_state (session_id, compacted_state, last_messages, tool_summary, updated_at)
		VALUES (?, '', '[]', '[]', ?)
		ON CONFLICT(session_id) DO UPDATE SET compacted_state = '', last_messages = '[]', tool_summary = '[]', updated_at = excluded.updated_at`, sessionID, now)
	return err
}

func (s *SessionStateStore) Delete(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_state WHERE session_id = ?`, sessionID)
	return err
}

func visibleMessages(msgs []model.Message) []model.Message {
	visible := make([]model.Message, 0, len(msgs))
	for _, m := range msgs {
		// 恢复会话只还原用户和 Suna 的可见对话；tool fact 由 session_state 承载。
		if m.Role == model.RoleUser || m.Role == model.RoleAssistant {
			text := strings.TrimSpace(m.Text())
			if text == "" {
				continue
			}
			visible = append(visible, model.NewTextMessage(m.Role, text))
		}
	}
	return visible
}

func decodeToolSummary(raw string) ToolSummary {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "{}" {
		return ToolSummary{}
	}
	var summary ToolSummary
	if err := json.Unmarshal([]byte(raw), &summary); err == nil && !summary.Empty() {
		return summary.Normalize()
	}
	// 旧版本 tool_summary 是数组；只转换为有界聚合摘要，解析失败则当无摘要处理。
	var items []ToolSummaryItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return ToolSummary{}
	}
	return BuildToolSummary(items)
}

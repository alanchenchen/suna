package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

type ConversationState struct {
	UserID            string
	SessionState      string
	LastMessages      []model.Message
	ToolSummary       ToolSummary
	MemoryProcessedAt time.Time
	UpdatedAt         time.Time
}

type ConversationStore struct {
	db *sql.DB
}

func NewConversationStore(db *sql.DB) *ConversationStore { return &ConversationStore{db: db} }

func (s *ConversationStore) Load(ctx context.Context, userID string) (*ConversationState, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	row := s.db.QueryRowContext(ctx, `SELECT user_id, session_state, last_messages, tool_summary, memory_processed_at, updated_at FROM conversation_state WHERE user_id = ?`, userID)
	var st ConversationState
	var lastMessages, toolSummary string
	var processed, updated sql.NullString
	if err := row.Scan(&st.UserID, &st.SessionState, &lastMessages, &toolSummary, &processed, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(lastMessages), &st.LastMessages)
	st.ToolSummary = decodeToolSummary(toolSummary)
	st.MemoryProcessedAt = parseDBTime(processed.String)
	st.UpdatedAt = parseDBTime(updated.String)
	return &st, nil
}

func (s *ConversationStore) Save(ctx context.Context, userID, sessionState string, msgs []model.Message, tools ToolSummary) error {
	if userID == "" {
		userID = DefaultUserID
	}
	// last_messages 保存 TUI 恢复需要展示的真实可见对话；模型恢复时另行使用 session_state 控制 token。
	msgs = visibleMessages(msgs)
	msgJSON, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	// tool_summary 只服务 TUI 恢复展示，不作为原始 tool 上下文恢复给模型。
	toolJSON, err := json.Marshal(tools.Normalize())
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO conversation_state (user_id, session_state, last_messages, tool_summary, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			session_state = excluded.session_state,
			last_messages = excluded.last_messages,
			tool_summary = excluded.tool_summary,
			updated_at = excluded.updated_at`, userID, strings.TrimSpace(sessionState), string(msgJSON), string(toolJSON), now)
	return err
}

func (s *ConversationStore) ClearLastMessages(ctx context.Context, userID string) error {
	if userID == "" {
		userID = DefaultUserID
	}
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_state (user_id, session_state, last_messages, tool_summary, updated_at)
		VALUES (?, '', '[]', '[]', ?)
		ON CONFLICT(user_id) DO UPDATE SET session_state = '', last_messages = '[]', tool_summary = '[]', updated_at = excluded.updated_at`, userID, now)
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

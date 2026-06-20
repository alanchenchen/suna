package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

type ConversationState struct {
	UserID            string
	SessionState      string
	LastMessages      []model.Message
	ToolSummary       []ToolSummaryItem
	MemoryProcessedAt time.Time
	UpdatedAt         time.Time
}

type ToolSummaryItem struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
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
	_ = json.Unmarshal([]byte(toolSummary), &st.ToolSummary)
	st.MemoryProcessedAt = parseDBTime(processed.String)
	st.UpdatedAt = parseDBTime(updated.String)
	return &st, nil
}

func (s *ConversationStore) Save(ctx context.Context, userID, sessionState string, msgs []model.Message, tools []ToolSummaryItem) error {
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
	toolJSON, err := json.Marshal(normalizeToolSummary(tools))
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

func normalizeToolSummary(items []ToolSummaryItem) []ToolSummaryItem {
	out := make([]ToolSummaryItem, 0, len(items))
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Status = strings.TrimSpace(item.Status)
		item.Summary = strings.TrimSpace(item.Summary)
		if item.Name == "" || item.Summary == "" {
			continue
		}
		if item.Status == "" {
			item.Status = "done"
		}
		// 摘要只用于恢复 UI 的“上一轮操作摘要”块，必须短，不能变成另一个历史日志。
		if len([]rune(item.Summary)) > 180 {
			item.Summary = truncateRunes(item.Summary, 180)
		}
		out = append(out, item)
	}
	return out
}

func FormatToolSummary(items []ToolSummaryItem) string {
	items = normalizeToolSummary(items)
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, 6)
	lines = append(lines, "上一轮工具操作摘要：")
	lines = append(lines, formatToolSummaryStats(items))
	if failure := formatToolSummaryFailures(items); failure != "" {
		lines = append(lines, failure)
	}
	if changes := formatToolSummaryChanges(items); changes != "" {
		lines = append(lines, changes)
	}
	lines = append(lines, formatToolSummaryRecent(items))
	if hidden := len(items) - min(len(items), 4); hidden > 0 {
		lines = append(lines, fmt.Sprintf("已折叠 %d 次较早操作", hidden))
	}
	return strings.Join(lines, "\n")
}

func formatToolSummaryStats(items []ToolSummaryItem) string {
	failures := 0
	for _, item := range items {
		if isToolSummaryFailure(item.Status) {
			failures++
		}
	}
	if failures == 0 {
		return fmt.Sprintf("%d 次 · 全部成功", len(items))
	}
	return fmt.Sprintf("%d 次 · %d 成功 / %d 失败", len(items), len(items)-failures, failures)
}

func formatToolSummaryFailures(items []ToolSummaryItem) string {
	parts := make([]string, 0, 2)
	for _, item := range items {
		if !isToolSummaryFailure(item.Status) {
			continue
		}
		parts = append(parts, item.Name+" · "+truncateRunes(item.Summary, 72))
		if len(parts) >= 2 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "失败：" + strings.Join(parts, "；")
}

func formatToolSummaryChanges(items []ToolSummaryItem) string {
	counts := make(map[string]int)
	order := make([]string, 0, 3)
	for _, item := range items {
		name := canonicalToolSummaryName(item.Name)
		if !isToolSummaryChangeTool(name) {
			continue
		}
		if counts[name] == 0 {
			order = append(order, name)
		}
		counts[name]++
	}
	if len(order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, counts[name]))
	}
	return "变更：" + strings.Join(parts, "，")
}

func formatToolSummaryRecent(items []ToolSummaryItem) string {
	start := max(0, len(items)-4)
	names := make([]string, 0, len(items)-start)
	for _, item := range items[start:] {
		names = append(names, canonicalToolSummaryName(item.Name))
	}
	return "最近：" + strings.Join(names, " → ")
}

func isToolSummaryFailure(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "error") || strings.Contains(status, "fail")
}

func canonicalToolSummaryName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, "."); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	return name
}

func isToolSummaryChangeTool(name string) bool {
	switch strings.ToLower(name) {
	case "editfile", "writefile", "filesystem":
		return true
	default:
		return false
	}
}

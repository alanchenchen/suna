package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

/*
ExtractItem 待提取的交互记录。
包含足够的上下文供 Memory Worker 进行 LLM 提取。
*/
type ExtractItem struct {
	SessionID    string
	Turn         int
	UserInput    string
	AgentOutput  string
	Significance Significance
}

/*
ExtractQueue 记忆提取队列。

设计原则（06-memory.md 提取流程）：
  - Agent Loop 不等待提取完成，直接入队
  - Memory Worker 独立消费，不阻塞主循环
  - Daemon 重启后扫描 memory_extracted=0 补处理
*/
type ExtractQueue struct {
	ch chan ExtractItem
	db *sql.DB
}

// extractTurnPair 用于把同一 session+turn 的 user/assistant 消息聚合成一次提取单元。
type extractTurnPair struct {
	sessionID    string
	turn         int
	userInput    string
	agentOutput  string
	significance Significance
}

// extractTurnKey 是提取恢复阶段的稳定键，必须包含 sessionID 以避免跨会话串数据。
type extractTurnKey struct {
	sessionID string
	turn      int
}

const extractQueueSize = 64

func NewExtractQueue(db *sql.DB) *ExtractQueue {
	return &ExtractQueue{
		ch: make(chan ExtractItem, extractQueueSize),
		db: db,
	}
}

func (q *ExtractQueue) Push(item ExtractItem) bool {
	select {
	case q.ch <- item:
		return true
	default:
		log.Printf("[memory] extract queue full, dropping turn %d", item.Turn)
		return false
	}
}

func (q *ExtractQueue) Ch() <-chan ExtractItem {
	return q.ch
}

func (q *ExtractQueue) Close() {
	close(q.ch)
}

func (q *ExtractQueue) EnqueueSession(ctx context.Context, sessionID string) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT session_id, turn, role, content, significance
		FROM session_messages
		WHERE session_id = ? AND (memory_extracted = 0 OR memory_extracted IS NULL)
		ORDER BY turn ASC`,
		sessionID,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	pairs := make(map[extractTurnKey]*extractTurnPair)

	for rows.Next() {
		var sid, role, content string
		var turn int
		var sig sql.NullString
		if err := rows.Scan(&sid, &turn, &role, &content, &sig); err != nil {
			continue
		}
		key := extractTurnKey{sessionID: sid, turn: turn}
		p, ok := pairs[key]
		if !ok {
			p = &extractTurnPair{sessionID: sid, turn: turn}
			pairs[key] = p
		}
		if sig.Valid {
			p.significance = Significance(sig.String)
		}
		switch role {
		case "user":
			p.userInput = content
		case "assistant":
			p.agentOutput = content
		}
	}

	for _, p := range pairs {
		if p.userInput == "" && p.agentOutput == "" {
			continue
		}
		if p.significance == "" {
			p.significance = SignificanceMedium
		}
		q.Push(ExtractItem{
			SessionID:    p.sessionID,
			Turn:         p.turn,
			UserInput:    p.userInput,
			AgentOutput:  p.agentOutput,
			Significance: p.significance,
		})
	}
}

/*
RecoverUnextracted 扫描 session_messages 中 memory_extracted=0 的记录，
恢复到提取队列中。用于 daemon 重启后的冷恢复。

策略：取最近 50 条未提取的记录，按 turn ASC 排序（保证时序正确）。
*/
func (q *ExtractQueue) RecoverUnextracted(ctx context.Context) (int, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT session_id, turn, role, content, significance
		FROM session_messages
		WHERE memory_extracted = 0
		ORDER BY created_at ASC
		LIMIT 100`,
	)
	if err != nil {
		return 0, fmt.Errorf("recover unextracted: %w", err)
	}
	defer rows.Close()

	pairs := make(map[extractTurnKey]*extractTurnPair)

	for rows.Next() {
		var sessionID, role, content string
		var turn int
		var sig sql.NullString
		if err := rows.Scan(&sessionID, &turn, &role, &content, &sig); err != nil {
			continue
		}

		key := extractTurnKey{sessionID: sessionID, turn: turn}
		p, ok := pairs[key]
		if !ok {
			p = &extractTurnPair{sessionID: sessionID, turn: turn}
			pairs[key] = p
		}
		if sig.Valid {
			p.significance = Significance(sig.String)
		}
		switch role {
		case "user":
			p.userInput = content
		case "assistant":
			p.agentOutput = content
		}
	}

	recovered := 0
	for _, p := range pairs {
		if p.userInput == "" && p.agentOutput == "" {
			continue
		}
		if p.significance == "" {
			p.significance = SignificanceMedium
		}
		item := ExtractItem{
			SessionID:    p.sessionID,
			Turn:         p.turn,
			UserInput:    p.userInput,
			AgentOutput:  p.agentOutput,
			Significance: p.significance,
		}
		if q.Push(item) {
			recovered++
		}
	}

	if recovered > 0 {
		log.Printf("[memory] recovered %d unextracted turns", recovered)
	}
	return recovered, nil
}

/*
MarkExtracted 标记指定 session+turn 的消息为已提取。
*/
func MarkExtracted(ctx context.Context, db *sql.DB, sessionID string, turn int) error {
	_, err := db.ExecContext(ctx, `
		UPDATE session_messages
		SET memory_extracted = 1
		WHERE session_id = ? AND turn = ?`,
		sessionID, turn,
	)
	return err
}

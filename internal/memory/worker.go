package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"database/sql"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

/*
Worker 异步批量处理记忆提取。

设计原则（06-memory.md Memory Worker）：
  - 独立 goroutine，常驻 daemon 进程
  - 积攒到 N 轮或空闲 M 秒后批量提取
  - 不阻塞 Agent Loop
  - TUI 关闭后 worker 继续处理
  - 单次 LLM 调用同时产出情景记忆 + 语义记忆 + 实体

触发条件（满足任一）：
  - 队列积攒 ≥ 5 轮未提取的交互
  - 距上次提取 ≥ 60 秒
  - 队列中存在高显著性交互
*/
type Worker struct {
	queue    *ExtractQueue
	episodic *EpisodicStore
	semantic *SemanticStore
	entities *EntityStore
	sessions *SessionStore
	db       *sql.DB
	provider model.Provider
	prompts  *prompt.Loader
	closed   chan struct{}
}

const (
	batchSize    = 5
	batchTimeout = 60 * time.Second
)

func NewWorker(
	queue *ExtractQueue,
	episodic *EpisodicStore,
	semantic *SemanticStore,
	entities *EntityStore,
	sessions *SessionStore,
	provider model.Provider,
) *Worker {
	return &Worker{
		queue:    queue,
		episodic: episodic,
		semantic: semantic,
		entities: entities,
		sessions: sessions,
		provider: provider,
		closed:   make(chan struct{}),
	}
}

func (w *Worker) SetPrompts(p *prompt.Loader) {
	w.prompts = p
}

/*
Run 启动 worker 主循环。阻塞直到 channel 关闭。
*/
func (w *Worker) Run() {
	defer close(w.closed)

	var batch []ExtractItem
	timer := time.NewTimer(batchTimeout)
	defer timer.Stop()

	for {
		select {
		case item, ok := <-w.queue.Ch():
			if !ok {
				if len(batch) > 0 {
					w.processBatch(batch)
				}
				return
			}
			batch = append(batch, item)

			hasHigh := false
			for _, it := range batch {
				if it.Significance == SignificanceHigh {
					hasHigh = true
					break
				}
			}

			if len(batch) >= batchSize || hasHigh {
				w.processBatch(batch)
				batch = nil
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(batchTimeout)
			}

		case <-timer.C:
			if len(batch) > 0 {
				w.processBatch(batch)
				batch = nil
			}
			timer.Reset(batchTimeout)
		}
	}
}

func (w *Worker) Wait() {
	<-w.closed
}

/*
processBatch 批量处理一组交互记录。

一次 LLM 调用同时产出：
  - 情景记忆（episodic_memories）
  - 语义事实（semantic_facts）
  - 实体（entities）

然后标记 session_messages 为 memory_extracted=1。
*/
func (w *Worker) processBatch(items []ExtractItem) {
	if len(items) == 0 {
		return
	}

	// 1. 存精简版交互摘要到 episodic（零 LLM 成本）
	for _, item := range items {
		w.storeEpisodicSummary(item)
	}

	// 2. LLM 提取事实和实体。失败时保留 memory_extracted=0，交给后续恢复/重试处理。
	if w.provider != nil {
		if err := w.extractBatch(items); err != nil {
			log.Printf("[memory] extract batch error: %v", err)
			return
		}
	}

	// 3. 标记为已提取
	for _, item := range items {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		MarkExtracted(ctx, w.sessions.db, item.SessionID, item.Turn)
		cancel()
	}
}

func (w *Worker) storeEpisodicSummary(item ExtractItem) {
	if w.episodic == nil {
		return
	}
	ctx := context.Background()
	content := fmt.Sprintf("User: %s\nAssistant: %s",
		truncateStr(item.UserInput, 500),
		truncateStr(item.AgentOutput, 500),
	)
	mem := &EpisodicMemory{
		Content:   content,
		Type:      "interaction",
		Source:    "auto",
		SessionID: item.SessionID,
	}
	if err := w.episodic.Store(ctx, mem); err != nil {
		log.Printf("[memory] episodic store error: %v", err)
	}
}

type extractResult struct {
	Episodes []extractEpisode `json:"episodes"`
	Facts    []extractFact    `json:"facts"`
}

type extractEpisode struct {
	Content  string   `json:"content"`
	Type     string   `json:"type"`
	Entities []string `json:"entities"`
}

type extractFact struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

func (w *Worker) extractBatch(items []ExtractItem) error {
	var systemPrompt string
	if w.prompts != nil {
		interactions := make([]prompt.ExtractInteraction, len(items))
		for i, item := range items {
			interactions[i] = prompt.ExtractInteraction{
				Index:       i + 1,
				UserInput:   truncateStr(item.UserInput, 300),
				AgentOutput: truncateStr(item.AgentOutput, 300),
			}
		}
		rendered, err := w.prompts.RenderExtractBatch(interactions)
		if err == nil && rendered != "" {
			systemPrompt = rendered
		}
	}
	if systemPrompt == "" {
		var sb strings.Builder
		sb.WriteString("Extract from these interactions:\n1. Memorable fact fragments (episodes)\n2. Structured user preferences/constraints/habits (facts)\n3. Key entity names\n\n")
		for i, item := range items {
			sb.WriteString(fmt.Sprintf("--- Interaction %d ---\nUser: %s\nAssistant: %s\n\n", i+1,
				truncateStr(item.UserInput, 300), truncateStr(item.AgentOutput, 300)))
		}
		sb.WriteString(`Output JSON:{"episodes":[{"content":"...","type":"preference|action|fact|decision","entities":["..."]}],"facts":[{"key":"...","value":"...","type":"preference|habit|constraint|fact","source":"user_stated|observed"}]}`)
		systemPrompt = sb.String()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := w.provider.Complete(ctx, &model.CompletionRequest{
		System:    systemPrompt,
		Messages:  []model.Message{model.NewTextMessage(model.RoleUser, "Extract facts now.")},
		MaxTokens: 2048,
	})
	if err != nil {
		return err
	}

	var full string
	for chunk := range ch {
		if chunk.Content != "" {
			full += chunk.Content
		}
		if chunk.Error != "" {
			return fmt.Errorf("provider stream: %s", chunk.Error)
		}
		if chunk.Done {
			break
		}
	}

	if full == "" {
		return fmt.Errorf("empty extract response")
	}

	result := parseExtractResult(full)
	if result == nil {
		return fmt.Errorf("failed to parse extract result: %s", truncateStr(full, 200))
	}

	ctx2 := context.Background()
	for _, ep := range result.Episodes {
		mem := &EpisodicMemory{
			Content:   ep.Content,
			Type:      ep.Type,
			Source:    "extracted",
			Entities:  ep.Entities,
			SessionID: items[0].SessionID,
		}
		if w.episodic != nil {
			if err := w.episodic.Store(ctx2, mem); err != nil {
				log.Printf("[memory] store episode error: %v", err)
			}
		}
		if w.entities != nil && len(ep.Entities) > 0 && mem.ID != "" {
			w.entities.StoreBatch(ctx2, ep.Entities, mem.ID)
		}
	}

	for _, f := range result.Facts {
		if f.Key == "" || f.Value == "" {
			continue
		}
		if w.semantic != nil {
			if err := w.semantic.Store(ctx2, f.Type, f.Key, f.Value, f.Source); err != nil {
				log.Printf("[memory] store fact error: %v", err)
			}
		}
	}

	log.Printf("[memory] extracted %d episodes, %d facts from %d items",
		len(result.Episodes), len(result.Facts), len(items))
	return nil
}

func parseExtractResult(raw string) *extractResult {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}

	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return nil
	}

	var result extractResult
	if err := json.Unmarshal([]byte(s[start:end+1]), &result); err != nil {
		return nil
	}
	return &result
}

func truncateStr(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	for i := range s {
		if i > max {
			return s[:i] + "..."
		}
	}
	return s
}

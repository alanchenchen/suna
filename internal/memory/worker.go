package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/google/uuid"
)

// BindingResolver 按队列事件持久化的 model_ref 解析不可变模型绑定，不能回退到当前默认模型。
type BindingResolver func(modelRef string) (*model.ModelBinding, error)

type Worker struct {
	queue      *ExtractQueue
	memories   *MemoryStore
	db         *sql.DB
	resolver   BindingResolver
	resolverMu sync.RWMutex
	prompts    *prompt.Loader
	closed     chan struct{}
}

const (
	batchSize    = 5
	batchTimeout = 60 * time.Second
)

func NewWorker(queue *ExtractQueue, memories *MemoryStore, db *sql.DB, resolver BindingResolver) *Worker {
	return &Worker{queue: queue, memories: memories, db: db, resolver: resolver, closed: make(chan struct{})}
}

func (w *Worker) SetPrompts(p *prompt.Loader) { w.prompts = p }

func (w *Worker) SetResolver(resolver BindingResolver) {
	w.resolverMu.Lock()
	defer w.resolverMu.Unlock()
	w.resolver = resolver
}

func (w *Worker) resolve(modelRef string) (*model.ModelBinding, error) {
	w.resolverMu.RLock()
	resolver := w.resolver
	w.resolverMu.RUnlock()
	if resolver == nil {
		return nil, fmt.Errorf("memory extraction model resolver is not configured")
	}
	return resolver(modelRef)
}

func (w *Worker) Run() {
	defer close(w.closed)
	timer := time.NewTimer(batchTimeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-w.queue.Ch():
			if !ok {
				// 关闭 worker 只负责停止后台循环，不启动新的记忆整理；pending queue 已持久化在 SQLite，等待下次启动恢复。
				return
			}
			// 普通事件攒批处理；high significance 事件尽快处理，减少“用户刚纠正但记忆还没更新”的窗口。
			if w.pendingCount() >= batchSize || w.hasHighPending() {
				w.processPending()
				resetTimer(timer)
			}
		case <-timer.C:
			// timeout 是 medium 候选的最长等待时间；攒不够 batchSize 也要落库合并，避免少量稳定偏好永远停在队列里。
			if w.pendingCount() > 0 {
				w.processPending()
			}
			timer.Reset(batchTimeout)
		}
	}
}

func (w *Worker) Wait() { <-w.closed }

func (w *Worker) pendingCount() int {
	if w.db == nil {
		return 0
	}
	return QueueDueCount(context.Background(), w.db, DefaultUserID)
}

func (w *Worker) hasHighPending() bool {
	if w.db == nil {
		return false
	}
	var n int
	_ = w.db.QueryRow(`SELECT COUNT(*) FROM memory_queue WHERE user_id = ? AND processed_at IS NULL AND significance = ? AND attempts < ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)`, DefaultUserID, SignificanceHigh, maxQueueAttempts, time.Now()).Scan(&n)
	return n > 0
}

func (w *Worker) processPending() {
	if w == nil || w.db == nil || w.memories == nil {
		return
	}
	loadCtx, loadCancel := context.WithTimeout(context.Background(), model.LLMMemoryCompactTimeout)
	items, err := LoadDueQueue(loadCtx, w.db, DefaultUserID, 50)
	loadCancel()
	if err != nil {
		logging.Error("memory", "load_queue_failed", err, nil)
		return
	}
	if len(items) == 0 {
		return
	}

	// 每个 model_ref 独立整理，避免一批候选意外使用其他会话切换后的活动模型。
	groups := make(map[string][]QueueItem)
	refs := make([]string, 0)
	for _, item := range items {
		ref := strings.TrimSpace(item.ModelRef)
		if _, exists := groups[ref]; !exists {
			refs = append(refs, ref)
		}
		groups[ref] = append(groups[ref], item)
	}
	w.processModelGroups(refs, groups, newMemoryCompactContext)
}

func newMemoryCompactContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), model.LLMMemoryCompactTimeout)
}

func (w *Worker) processModelGroups(refs []string, groups map[string][]QueueItem, newContext func() (context.Context, context.CancelFunc)) {
	for _, ref := range refs {
		group := groups[ref]
		if ref == "" {
			// 空 model_ref 同样按失败退避并最终丢弃，避免每分钟热循环；绝不回退到 active model。
			err := fmt.Errorf("memory extraction model_ref is empty")
			logging.Info("memory", "compaction_missing_model_ref", logging.Event{"queue_events": len(group), "queue_ids": compactQueueIDs(group)})
			if retryErr := RetryQueueItems(context.Background(), w.db, queueIDs(group), err); retryErr != nil {
				logging.Error("memory", "retry_queue_failed", retryErr, nil)
			}
			continue
		}
		ctx, cancel := newContext()
		w.processModelGroup(ctx, ref, group)
		cancel()
	}
}

func (w *Worker) processModelGroup(ctx context.Context, modelRef string, items []QueueItem) {
	if len(items) == 0 {
		return
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	binding, err := w.resolve(modelRef)
	if err != nil || binding == nil {
		if err == nil {
			err = fmt.Errorf("memory extraction model %q is unavailable", modelRef)
		}
		logging.Info("memory", "compaction_model_unresolved", logging.Event{"model_ref": modelRef, "queue_events": len(items), "queue_ids": compactQueueIDs(items)})
		if retryErr := RetryQueueItems(context.Background(), w.db, ids, err); retryErr != nil {
			logging.Error("memory", "retry_queue_failed", retryErr, nil)
		}
		return
	}
	current, err := w.memories.List(ctx, DefaultUserID, MaxActiveMemories)
	if err != nil {
		logging.Error("memory", "load_active_memory_failed", err, logging.Event{"model_ref": modelRef, "queue_events": len(items)})
		w.retry(ids, err)
		return
	}
	requestID := uuid.New().String()
	metadata := logging.Event{"request_id": requestID, "model_ref": modelRef, "queue_events": len(items), "queue_ids": compactQueueIDs(items), "attempts": compactAttempts(items), "significance": compactSignificance(items), "active_memories_before": len(current)}
	logging.Info("memory", "compaction_start", metadata)
	newList, err := w.compact(ctx, binding, current, items, requestID)
	if err != nil {
		metadata["will_retry"] = true
		logging.Error("memory", "compaction_failed", err, metadata)
		w.retry(ids, err)
		return
	}
	if err := w.memories.CommitQueueCompaction(ctx, DefaultUserID, ids, newList); err != nil {
		logging.Error("memory", "commit_compaction_failed", err, logging.Event{"model_ref": modelRef, "queue_events": len(items), "active_memories_after": len(newList)})
		w.retry(ids, err)
		return
	}
	logging.Info("memory", "compaction_success", logging.Event{"request_id": requestID, "model_ref": modelRef, "queue_events": len(items), "active_memories_before": len(current), "active_memories_after": len(newList)})
}

type compactionMemory struct {
	ID         string   `json:"id,omitempty"`
	Kind       string   `json:"kind"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	Source     string   `json:"source"`
	Confidence float64  `json:"confidence"`
	Priority   int      `json:"priority"`
	IsCore     bool     `json:"is_core"`
	Evidence   string   `json:"evidence,omitempty"`
}

type compactionResult struct {
	Memories []compactionMemory `json:"memories"`
}

func (w *Worker) retry(ids []string, cause error) {
	if err := RetryQueueItems(context.Background(), w.db, ids, cause); err != nil {
		// retry 的读取/更新错误必须可观察，避免数据层故障被后续处理掩盖。
		logging.Error("memory", "retry_queue_failed", err, logging.Event{"queue_ids": strings.Join(ids, ",")})
	}
}

func (w *Worker) compact(ctx context.Context, binding *model.ModelBinding, current []UserMemory, items []QueueItem, requestID string) ([]UserMemory, error) {
	systemPrompt := w.renderCompactionPrompt(current, items)
	// 记忆整理是异步 LLM 调用，一次处理多条 queue event，并要求模型返回完整的新列表。
	// 主请求链路不会等待这个调用，因此不会影响用户看到回复的延迟。
	ch, err := binding.Complete(ctx, &model.CompletionRequest{Purpose: "memory_compact", RequestID: requestID, System: systemPrompt, Messages: []model.Message{model.NewTextMessage(model.RoleUser, "Return the new user profile memory JSON now.")}})
	if err != nil {
		return nil, err
	}
	full, err := model.ReadStreamTextWithIdle(ctx, ch, model.LLMMemoryCompactTimeout, "memory compact LLM stream timeout")
	if err != nil {
		return nil, fmt.Errorf("provider stream: %w", err)
	}
	result := parseCompactionResult(full)
	if result == nil {
		return nil, fmt.Errorf("failed to parse compaction result: %s", truncateRunes(full, 300))
	}
	out := make([]UserMemory, 0, len(result.Memories))
	for _, m := range result.Memories {
		out = append(out, UserMemory{ID: m.ID, Kind: m.Kind, Content: m.Content, Tags: m.Tags, Source: m.Source, Confidence: m.Confidence, Priority: m.Priority, IsCore: m.IsCore, Evidence: m.Evidence})
	}
	return out, nil
}

func (w *Worker) renderCompactionPrompt(current []UserMemory, items []QueueItem) string {
	data := memoryPromptData(current, items)
	b, _ := json.MarshalIndent(data, "", "  ")
	if w.prompts != nil {
		data["input_json"] = string(b)
		if rendered, err := w.prompts.RenderMemoryCompact(data); err == nil && rendered != "" {
			return rendered
		}
	}
	return "You maintain Suna long-term user profile memory. Return JSON {\"memories\":[...]} with at most 30 concise profile memories. Prefer updating/merging over adding. Keep only durable user communication preferences, workflow habits, constraints, corrections, and explicitly provided user facts. Do not store project facts, implementation details, task history, tool schemas, UI shortcuts, paths, logs, test results, or session decisions. Current user instruction overrides old memory.\n\nInput:\n" + string(b)
}

func memoryPromptData(current []UserMemory, items []QueueItem) map[string]any {
	cur := make([]compactionMemory, 0, len(current))
	for _, m := range current {
		cur = append(cur, compactionMemory{ID: m.ID, Kind: m.Kind, Content: m.Content, Tags: m.Tags, Source: m.Source, Confidence: m.Confidence, Priority: m.Priority, IsCore: m.IsCore, Evidence: m.Evidence})
	}
	candidates := make([]compactionMemory, 0, len(items))
	for _, it := range items {
		candidates = append(candidates, compactionMemory{Kind: it.Kind, Content: it.Content, Tags: it.Tags, Source: it.Source, Confidence: it.Confidence, Priority: priorityForSignificance(it.Significance), Evidence: truncateRunes(it.Evidence, 180)})
	}
	return map[string]any{"current_memories": cur, "candidates": candidates, "max_memories": MaxActiveMemories, "max_core": MaxCoreMemories}
}

func priorityForSignificance(sig Significance) int {
	switch sig {
	case SignificanceHigh:
		return 80
	case SignificanceMedium:
		return 60
	default:
		return 50
	}
}

func parseCompactionResult(raw string) *compactionResult {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		// 兼容模型把 JSON 包在 markdown code fence 里的情况。
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
	if start < 0 || end <= start {
		return nil
	}
	var result compactionResult
	if err := json.Unmarshal([]byte(s[start:end+1]), &result); err != nil {
		return nil
	}
	return &result
}

func queueIDs(items []QueueItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func compactQueueIDs(items []QueueItem) string {
	parts := make([]string, 0, min(len(items), 8))
	for i, it := range items {
		if i >= 8 {
			parts = append(parts, fmt.Sprintf("+%d", len(items)-i))
			break
		}
		parts = append(parts, it.ID)
	}
	return strings.Join(parts, ",")
}

func compactAttempts(items []QueueItem) string {
	parts := make([]string, 0, min(len(items), 8))
	for i, it := range items {
		if i >= 8 {
			parts = append(parts, fmt.Sprintf("+%d", len(items)-i))
			break
		}
		parts = append(parts, fmt.Sprintf("%d", it.Attempts))
	}
	return strings.Join(parts, ",")
}

func compactSignificance(items []QueueItem) string {
	parts := make([]string, 0, min(len(items), 8))
	for i, it := range items {
		if i >= 8 {
			parts = append(parts, fmt.Sprintf("+%d", len(items)-i))
			break
		}
		parts = append(parts, string(it.Significance))
	}
	return strings.Join(parts, ",")
}

func resetTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(batchTimeout)
}

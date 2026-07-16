package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/google/uuid"
)

const maxQueueAttempts = 3

type QueueItem struct {
	ID            string
	UserID        string
	ModelRef      string
	Kind          string
	Content       string
	Tags          []string
	Source        string
	Confidence    float64
	Evidence      string
	Significance  Significance
	CreatedAt     time.Time
	NextAttemptAt time.Time
	Attempts      int
}

type ExtractQueue struct {
	ch chan struct{}
	db *sql.DB

	// close 与非阻塞 Signal 共用此锁，防止 close(send channel) 的并发 panic。
	mu     sync.Mutex
	closed bool
}

const extractQueueSize = 1

func NewExtractQueue(db *sql.DB) *ExtractQueue {
	return &ExtractQueue{ch: make(chan struct{}, extractQueueSize), db: db}
}

func (q *ExtractQueue) Push(ctx context.Context, userID, modelRef string, candidate Candidate) bool {
	if q == nil || q.db == nil {
		return false
	}
	if userID == "" {
		userID = DefaultUserID
	}
	modelRef = strings.TrimSpace(modelRef)
	candidate.UserID = userID
	candidate, ok := normalizeCandidate(candidate)
	if !ok {
		return false
	}
	// memory_queue 只保存结构化用户画像候选，不保存原始对话，避免 assistant 总结或任务日志进入长期记忆。
	_, err := q.db.ExecContext(ctx, `
		INSERT INTO memory_queue (id, user_id, model_ref, kind, content, tags, source, confidence, evidence, significance, created_at, next_attempt_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uuid.New().String(), userID, modelRef, candidate.Kind, candidate.Content, marshalStringSlice(candidate.Tags), candidate.Source, candidate.Confidence, candidate.Evidence, string(candidate.Significance), time.Now(), time.Now())
	if err != nil {
		logging.Error("memory", "queue_insert_failed", err, logging.Event{"model_ref": modelRef, "queue_kind": candidate.Kind, "significance": string(candidate.Significance)})
		return false
	}
	q.Signal()
	return true
}

func (q *ExtractQueue) Signal() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	select {
	case q.ch <- struct{}{}:
	default:
	}
}

func (q *ExtractQueue) Ch() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.ch
}

// Close 是幂等的；与 Push/Signal 并发时不会向已关闭 channel 发送。
func (q *ExtractQueue) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.ch)
}

func (q *ExtractQueue) RecoverUnextracted(ctx context.Context) (int, error) {
	if q == nil || q.db == nil {
		return 0, nil
	}
	var count int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_queue WHERE processed_at IS NULL`).Scan(&count); err != nil {
		return 0, fmt.Errorf("recover memory queue: %w", err)
	}
	if count > 0 {
		q.Signal()
		logging.Info("memory", "queue_recovered", logging.Event{"pending_queue_events": count})
	}
	return count, nil
}

// LoadDueQueue 由唯一的 worker goroutine 调用。无需 claim/lease，下一次循环只会在本次处理完成后进入。
func LoadDueQueue(ctx context.Context, db *sql.DB, userID string, limit int) ([]QueueItem, error) {
	if db == nil {
		return nil, fmt.Errorf("load memory queue: nil database")
	}
	if userID == "" {
		userID = DefaultUserID
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, model_ref, kind, content, tags, source, confidence, evidence, significance, created_at, next_attempt_at, attempts
		FROM memory_queue
		WHERE user_id = ? AND processed_at IS NULL AND attempts < ?
		  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY created_at ASC LIMIT ?`, userID, maxQueueAttempts, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("load due memory queue: %w", err)
	}
	defer rows.Close()
	items := make([]QueueItem, 0, limit)
	for rows.Next() {
		item, err := scanQueueItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan due memory queue: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due memory queue: %w", err)
	}
	return items, nil
}

func scanQueueItem(rows interface{ Scan(...any) error }) (QueueItem, error) {
	var item QueueItem
	var tags, sig, created, nextAttempt sql.NullString
	if err := rows.Scan(&item.ID, &item.UserID, &item.ModelRef, &item.Kind, &item.Content, &tags, &item.Source, &item.Confidence, &item.Evidence, &sig, &created, &nextAttempt, &item.Attempts); err != nil {
		return QueueItem{}, err
	}
	item.ModelRef = strings.TrimSpace(item.ModelRef)
	item.Kind = normalizeKind(item.Kind)
	item.Tags = normalizeTags(unmarshalStringSlice(tags.String))
	item.Source = normalizeSource(item.Source)
	item.Confidence = clampConfidence(item.Confidence)
	item.Significance = Significance(sig.String)
	item.CreatedAt = parseDBTime(created.String)
	item.NextAttemptAt = parseDBTime(nextAttempt.String)
	return item, nil
}

// RetryQueueItems 为失败项记录普通退避。单 worker 不会和另一 worker 争抢同一队列项。
func RetryQueueItems(ctx context.Context, db *sql.DB, ids []string, cause error) error {
	if len(ids) == 0 {
		return nil
	}
	if db == nil {
		return fmt.Errorf("retry memory queue: nil database")
	}
	errText := ""
	if cause != nil {
		errText = truncateRunes(cause.Error(), 500)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin queue retry: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		var attempts int
		err := tx.QueryRowContext(ctx, `SELECT attempts FROM memory_queue WHERE id = ? AND processed_at IS NULL`, id).Scan(&attempts)
		if err == sql.ErrNoRows {
			continue // 项目已被原子提交或删除，无需再更新。
		}
		if err != nil {
			return fmt.Errorf("read retry attempts for %s: %w", id, err)
		}
		nextAttempts := attempts + 1
		if nextAttempts >= maxQueueAttempts {
			if _, err := tx.ExecContext(ctx, `DELETE FROM memory_queue WHERE id = ? AND processed_at IS NULL`, id); err != nil {
				return fmt.Errorf("drop queue item %s: %w", id, err)
			}
			logging.Error("memory", "queue_drop_after_retries", cause, logging.Event{"attempts": nextAttempts, "queue_id": id})
			continue
		}
		next := time.Now().Add(queueBackoff(nextAttempts))
		if _, err := tx.ExecContext(ctx, `UPDATE memory_queue SET attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ? AND processed_at IS NULL`, nextAttempts, next, errText, id); err != nil {
			return fmt.Errorf("retry queue item %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit queue retry: %w", err)
	}
	return nil
}

func queueBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 5 * time.Minute
	case 2:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func QueueCount(ctx context.Context, db *sql.DB, userID string) int {
	if userID == "" {
		userID = DefaultUserID
	}
	var count int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_queue WHERE user_id = ? AND processed_at IS NULL`, userID).Scan(&count)
	return count
}

func QueueDueCount(ctx context.Context, db *sql.DB, userID string) int {
	if userID == "" {
		userID = DefaultUserID
	}
	var count int
	now := time.Now()
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_queue WHERE user_id = ? AND processed_at IS NULL AND attempts < ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)`, userID, maxQueueAttempts, now).Scan(&count)
	return count
}

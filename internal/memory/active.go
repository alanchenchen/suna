package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultUserID       = "default"
	MaxActiveMemories   = 30
	MaxCoreMemories     = 5
	MaxInjectedMemories = 5
)

type UserMemory struct {
	ID         string
	UserID     string
	Kind       string
	Content    string
	Tags       []string
	Source     string
	Confidence float64
	Priority   int
	IsCore     bool
	UseCount   int
	LastUsedAt time.Time
	Evidence   string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type MemoryStore struct {
	db *sql.DB
}

func NewMemoryStore(db *sql.DB) *MemoryStore {
	return &MemoryStore{db: db}
}

func (s *MemoryStore) List(ctx context.Context, userID string, limit int) ([]UserMemory, error) {
	return s.list(ctx, s.db, userID, limit)
}

func (s *MemoryStore) list(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, userID string, limit int) ([]UserMemory, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	if limit <= 0 {
		limit = MaxActiveMemories
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, user_id, kind, content, tags, source, confidence, priority, is_core, use_count,
		       last_used_at, evidence, expires_at, created_at, updated_at
		FROM user_profile_memory
		WHERE user_id = ? AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY is_core DESC, priority DESC, confidence DESC, COALESCE(last_used_at, updated_at) DESC, updated_at DESC, id ASC
		LIMIT ?`, userID, time.Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserMemory
	for rows.Next() {
		m, err := scanUserMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *MemoryStore) Count(ctx context.Context, userID string) (active, core int) {
	if userID == "" {
		userID = DefaultUserID
	}
	now := time.Now()
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_profile_memory WHERE user_id = ? AND (expires_at IS NULL OR expires_at > ?)`, userID, now).Scan(&active)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_profile_memory WHERE user_id = ? AND is_core = 1 AND (expires_at IS NULL OR expires_at > ?)`, userID, now).Scan(&core)
	return active, core
}

func (s *MemoryStore) Delete(ctx context.Context, userID, id string) (bool, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_profile_memory WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *MemoryStore) Clear(ctx context.Context, userID string) (int, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_profile_memory WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *MemoryStore) BuildBrief(ctx context.Context, userID, query string) (string, []UserMemory, error) {
	mems, err := s.List(ctx, userID, MaxActiveMemories)
	if err != nil || len(mems) == 0 {
		return "", nil, err
	}
	// 召回不使用 embedding/LLM：长期画像总量很小，规则召回更稳定、成本更低。
	// 这里限制 core 数量，再用 query/tag/content 命中补足，避免每轮都注入同一批高优先级 coding 记忆。
	selected := selectMemories(mems, query)
	if len(selected) == 0 {
		return "", nil, nil
	}
	var lines []string
	ids := make([]string, 0, len(selected))
	for _, m := range selected {
		lines = append(lines, "- "+m.Content)
		ids = append(ids, m.ID)
	}
	_ = s.MarkUsed(ctx, ids)
	return strings.Join(lines, "\n"), selected, nil
}

func (s *MemoryStore) MarkUsed(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	for _, id := range ids {
		_, _ = s.db.ExecContext(ctx, `UPDATE user_profile_memory SET use_count = use_count + 1, last_used_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	}
	return nil
}

func (s *MemoryStore) ReplaceAll(ctx context.Context, userID string, newList []UserMemory) error {
	if userID == "" {
		userID = DefaultUserID
	}
	if len(newList) > MaxActiveMemories {
		newList = newList[:MaxActiveMemories]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := replaceAllTx(ctx, tx, userID, newList); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitQueueCompaction 在单个 SQLite transaction 中替换用户画像并删除本批队列项。
// 当前队列只由一个 worker 串行消费；任一队列项缺失时整笔事务回滚。
func (s *MemoryStore) CommitQueueCompaction(ctx context.Context, userID string, ids []string, newList []UserMemory) error {
	if userID == "" {
		userID = DefaultUserID
	}
	if len(ids) == 0 {
		return fmt.Errorf("commit queue compaction: missing queue items")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceAllTx(ctx, tx, userID, newList); err != nil {
		return err
	}
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `DELETE FROM memory_queue WHERE id = ? AND user_id = ? AND processed_at IS NULL`, id, userID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("commit queue compaction: queue item %q is missing", id)
		}
	}
	return tx.Commit()
}

func replaceAllTx(ctx context.Context, tx *sql.Tx, userID string, newList []UserMemory) error {
	if len(newList) > MaxActiveMemories {
		newList = newList[:MaxActiveMemories]
	}

	existingRows, err := tx.QueryContext(ctx, `SELECT id, content, kind FROM user_profile_memory WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	existing := map[string]UserMemory{}
	for existingRows.Next() {
		var m UserMemory
		if err := existingRows.Scan(&m.ID, &m.Content, &m.Kind); err != nil {
			existingRows.Close()
			return err
		}
		existing[m.ID] = m
	}
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		return err
	}
	if err := existingRows.Close(); err != nil {
		return err
	}

	keep := map[string]bool{}
	now := time.Now()
	for _, m := range normalizeMemoryList(userID, newList) {
		// compactor 输出完整的新用户画像列表，不是增量 patch；这里复用旧 id，避免同一画像重复生成。
		if m.ID == "" || existing[m.ID].ID == "" {
			m.ID = matchExistingMemory(existing, keep, m)
		}
		if m.ID == "" {
			m.ID = uuid.New().String()
		}
		keep[m.ID] = true
		isCore := 0
		if m.IsCore {
			isCore = 1
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO user_profile_memory (id, user_id, kind, content, tags, source, confidence, priority, is_core, evidence, created_at, updated_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				kind = excluded.kind,
				content = excluded.content,
				tags = excluded.tags,
				source = excluded.source,
				confidence = excluded.confidence,
				priority = excluded.priority,
				is_core = excluded.is_core,
				evidence = excluded.evidence,
				updated_at = excluded.updated_at,
				expires_at = excluded.expires_at`,
			m.ID, userID, m.Kind, m.Content, marshalStringSlice(m.Tags), m.Source, m.Confidence, m.Priority, isCore, m.Evidence, now, now, nullableTime(m.ExpiresAt))
		if err != nil {
			return err
		}
	}

	for id := range existing {
		if !keep[id] {
			// 未被 compaction 返回的旧画像视为不再有效，直接删除，保持用户画像小而准。
			if _, err := tx.ExecContext(ctx, `DELETE FROM user_profile_memory WHERE id = ?`, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func selectMemories(mems []UserMemory, query string) []UserMemory {
	query = strings.ToLower(query)
	tokens := queryTokens(query)
	scored := make([]memoryScore, 0, len(mems))
	for _, m := range mems {
		score := m.Priority + int(m.Confidence*20)
		if m.IsCore {
			// core 代表跨场景稳定偏好，但最多只注入少量，避免挤掉当前 query 相关画像。
			score += 400
		}
		if m.Kind == MemoryKindCorrection {
			score += 40
		}
		if len(tokens) > 0 {
			text := strings.ToLower(m.Content + " " + strings.Join(m.Tags, " ") + " " + m.Kind)
			for _, tok := range tokens {
				if strings.Contains(text, tok) {
					score += 120
				}
			}
		}
		scored = append(scored, memoryScore{Memory: m, Score: score})
	}
	sortMemoryScores(scored)

	out := make([]UserMemory, 0, MaxInjectedMemories)
	used := map[string]bool{}
	coreCount := 0
	for _, s := range scored {
		if len(out) >= MaxInjectedMemories || coreCount >= 2 {
			break
		}
		if s.Memory.IsCore {
			out = append(out, s.Memory)
			used[s.Memory.ID] = true
			coreCount++
		}
	}
	for _, s := range scored {
		if len(out) >= MaxInjectedMemories {
			break
		}
		if used[s.Memory.ID] {
			continue
		}
		out = append(out, s.Memory)
		used[s.Memory.ID] = true
	}
	return out
}

func sortMemoryScores(scored []memoryScore) {
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].Memory.Priority != scored[j].Memory.Priority {
			return scored[i].Memory.Priority > scored[j].Memory.Priority
		}
		if scored[i].Memory.Confidence != scored[j].Memory.Confidence {
			return scored[i].Memory.Confidence > scored[j].Memory.Confidence
		}
		return scored[i].Memory.ID < scored[j].Memory.ID
	})
}

type memoryScore struct {
	Memory UserMemory
	Score  int
}

func queryTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == '。' || r == '，' || r == ':' || r == '：' || r == ';' || r == '；' || r == '?' || r == '？' || r == '!' || r == '！'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if len([]rune(f)) >= 2 {
			out = append(out, f)
		}
	}
	return out
}

func normalizeMemoryList(userID string, in []UserMemory) []UserMemory {
	out := make([]UserMemory, 0, len(in))
	core := 0
	seen := map[string]bool{}
	for _, m := range in {
		m.UserID = userID
		m.Kind = normalizeKind(m.Kind)
		m.Source = normalizeSource(m.Source)
		m.Confidence = clampConfidence(m.Confidence)
		m.Priority = clampPriority(m.Priority)
		m.Content = truncateRunes(strings.TrimSpace(m.Content), 180)
		m.Evidence = truncateRunes(strings.TrimSpace(m.Evidence), 180)
		m.Tags = normalizeTags(m.Tags)
		if m.Content == "" || seen[strings.ToLower(m.Content)] {
			continue
		}
		seen[strings.ToLower(m.Content)] = true
		if m.IsCore {
			core++
			if core > MaxCoreMemories || m.Confidence < 0.8 {
				m.IsCore = false
			}
		}
		out = append(out, m)
		if len(out) >= MaxActiveMemories {
			break
		}
	}
	return out
}

func normalizeKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case MemoryKindCommunication, MemoryKindWorkflow, MemoryKindPreference, MemoryKindConstraint, MemoryKindCorrection, MemoryKindUserFact:
		return strings.TrimSpace(strings.ToLower(kind))
	case "habit", "personality":
		return MemoryKindWorkflow
	case "fact":
		return MemoryKindUserFact
	default:
		return MemoryKindPreference
	}
}

func normalizeSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case MemorySourceExplicit, MemorySourceInferred, MemorySourceCorrection:
		return strings.TrimSpace(strings.ToLower(source))
	default:
		return MemorySourceInferred
	}
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, min(len(tags), 5))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		tag = strings.ReplaceAll(tag, " ", "-")
		if tag == "" || len([]rune(tag)) > 24 || strings.Contains(tag, "/") || strings.Contains(tag, "\\") || strings.Contains(tag, "://") || strings.Contains(tag, ".") || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func matchExistingMemory(existing map[string]UserMemory, keep map[string]bool, m UserMemory) string {
	for id, old := range existing {
		if keep[id] {
			continue
		}
		if old.Kind == m.Kind && strings.EqualFold(strings.TrimSpace(old.Content), strings.TrimSpace(m.Content)) {
			return id
		}
	}
	return ""
}

func scanUserMemory(rows interface{ Scan(dest ...any) error }) (UserMemory, error) {
	var m UserMemory
	var tags string
	var isCore int
	var lastUsed, evidence, expires, created, updated sql.NullString
	err := rows.Scan(&m.ID, &m.UserID, &m.Kind, &m.Content, &tags, &m.Source, &m.Confidence, &m.Priority, &isCore, &m.UseCount, &lastUsed, &evidence, &expires, &created, &updated)
	if err != nil {
		return m, err
	}
	m.Kind = normalizeKind(m.Kind)
	m.Tags = normalizeTags(unmarshalStringSlice(tags))
	m.Source = normalizeSource(m.Source)
	m.Confidence = clampConfidence(m.Confidence)
	m.IsCore = isCore == 1
	m.LastUsedAt = parseDBTime(lastUsed.String)
	m.Evidence = evidence.String
	m.ExpiresAt = parseDBTime(expires.String)
	m.CreatedAt = parseDBTime(created.String)
	m.UpdatedAt = parseDBTime(updated.String)
	return m, nil
}

func marshalStringSlice(v []string) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalStringSlice(s string) []string {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func clampPriority(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func clampConfidence(v float64) float64 {
	if v <= 0 {
		return 0.7
	}
	if v > 1 {
		return 1
	}
	return v
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (m UserMemory) String() string {
	return fmt.Sprintf("%s: %s", m.Kind, m.Content)
}

package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type EpisodicMemory struct {
	ID        string
	Content   string
	Type      string
	Source    string
	Entities  []string
	Embedding []float64
	Timestamp time.Time
	SessionID string
	Metadata  map[string]any
}

type EpisodicStore struct {
	db *sql.DB
}

func NewEpisodicStore(db *sql.DB) *EpisodicStore {
	return &EpisodicStore{db: db}
}

func (s *EpisodicStore) Store(ctx context.Context, mem *EpisodicMemory) error {
	if mem.ID == "" {
		mem.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if mem.Timestamp.IsZero() {
		mem.Timestamp = time.Now()
	}
	entitiesJSON, _ := json.Marshal(mem.Entities)
	metadataJSON, _ := json.Marshal(mem.Metadata)
	var embeddingBlob []byte
	if len(mem.Embedding) > 0 {
		embeddingBlob = floatsToBlob(mem.Embedding)
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO episodic_memories (id, content, type, source, entities, embedding, ts, session_id, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID, mem.Content, mem.Type, mem.Source, string(entitiesJSON), embeddingBlob,
		mem.Timestamp, mem.SessionID, string(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("insert episodic memory: %w", err)
	}

	rowID, _ := result.LastInsertId()
	_, err = s.db.ExecContext(ctx, `INSERT INTO episodic_fts(rowid, content) VALUES (?, ?)`,
		rowID, mem.Content,
	)
	if err != nil {
		return fmt.Errorf("insert fts: %w", err)
	}
	return nil
}

// SearchFTS 先尝试 FTS5 全文搜索，如果无结果或查询包含 CJK 字符则降级为 LIKE 搜索。
// FTS5 的 unicode61 tokenizer 不支持中文分词，CJK 查询直接走 LIKE。
func (s *EpisodicStore) SearchFTS(ctx context.Context, query string, limit int) ([]*EpisodicMemory, error) {
	if limit <= 0 {
		limit = 10
	}

	// 包含 CJK 字符时直接走 LIKE
	if hasCJK(query) {
		return s.searchLike(ctx, query, limit)
	}

	// 先尝试 FTS
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.content, m.type, m.source, m.entities, m.ts, m.session_id, m.metadata
		FROM episodic_fts f
		JOIN episodic_memories m ON m.id = f.rowid
		WHERE episodic_fts MATCH ?
		ORDER BY rank
		LIMIT ?`,
		query, limit,
	)
	if err != nil {
		// FTS 语法错误时降级到 LIKE
		return s.searchLike(ctx, query, limit)
	}
	defer rows.Close()

	results, scanErr := scanMemories(rows)
	if scanErr != nil {
		return nil, scanErr
	}
	if len(results) > 0 {
		return results, nil
	}

	// FTS 无结果，降级到 LIKE
	return s.searchLike(ctx, query, limit)
}

// searchLike 使用 LIKE 模糊搜索，支持中文
func (s *EpisodicStore) searchLike(ctx context.Context, query string, limit int) ([]*EpisodicMemory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, type, source, entities, ts, session_id, metadata
		FROM episodic_memories
		WHERE content LIKE ?
		ORDER BY ts DESC
		LIMIT ?`,
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("like search: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

type RewriteProvider interface {
	RewriteQuery(ctx context.Context, query string) (string, error)
}

func (s *EpisodicStore) SearchWithRewrite(ctx context.Context, query string, limit int, rewriter RewriteProvider) ([]*EpisodicMemory, error) {
	results, err := s.SearchFTS(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if len(results) >= 3 || rewriter == nil {
		return results, nil
	}

	rewritten, rwErr := rewriter.RewriteQuery(ctx, query)
	if rwErr != nil || rewritten == "" || rewritten == query {
		return results, nil
	}

	rewrittenResults, rwErr := s.SearchFTS(ctx, rewritten, limit)
	if rwErr != nil {
		return results, nil
	}

	seen := make(map[string]bool)
	for _, m := range results {
		seen[m.ID] = true
	}
	for _, m := range rewrittenResults {
		if !seen[m.ID] {
			results = append(results, m)
			seen[m.ID] = true
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// hasCJK 判断字符串是否包含 CJK 字符
func hasCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) ||
			(r >= 0x2E80 && r <= 0x2EFF) || (r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE4F) {
			return true
		}
	}
	return false
}

func (s *EpisodicStore) SearchByEmbedding(ctx context.Context, queryVec []float64, limit int) ([]*EpisodicMemory, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, type, source, entities, embedding, ts, session_id, metadata
		FROM episodic_memories
		WHERE embedding IS NOT NULL
		ORDER BY ts DESC
		LIMIT 1000`,
	)
	if err != nil {
		return nil, fmt.Errorf("query for embedding search: %w", err)
	}
	defer rows.Close()

	type scored struct {
		mem   *EpisodicMemory
		score float64
	}
	var results []scored
	for rows.Next() {
		mem, err := scanMemoryWithEmbedding(rows)
		if err != nil {
			continue
		}
		if len(mem.Embedding) == 0 {
			continue
		}
		score := cosineSim(queryVec, mem.Embedding)
		results = append(results, scored{mem: mem, score: score})
	}

	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	memories := make([]*EpisodicMemory, len(results))
	for i, r := range results {
		memories[i] = r.mem
	}
	return memories, nil
}

// Recall 跨会话检索：用 FTS+时间衰减找到最相关的历史记忆。
// 限制返回 top-N，按相关度降序。
func (s *EpisodicStore) Recall(ctx context.Context, query string, limit int) ([]*EpisodicMemory, error) {
	if limit <= 0 {
		limit = 5
	}

	results, err := s.SearchFTS(ctx, query, limit*3)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	now := time.Now()
	for i := range results {
		if results[i].Metadata == nil {
			results[i].Metadata = make(map[string]any)
		}
		results[i].Metadata["_score"] = timeDecayScore(now, results[i].Timestamp)
	}

	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			si, _ := results[i].Metadata["_score"].(float64)
			sj, _ := results[j].Metadata["_score"].(float64)
			if sj > si {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	tokenBudget := 4000
	var selected []*EpisodicMemory
	usedTokens := 0
	for _, m := range results {
		estTokens := len(m.Content) / 4
		if estTokens < 50 {
			estTokens = 50
		}
		if usedTokens+estTokens > tokenBudget {
			break
		}
		selected = append(selected, m)
		usedTokens += estTokens
	}

	if len(selected) > limit {
		selected = selected[:limit]
	}
	return selected, nil
}

func timeDecayScore(now time.Time, ts time.Time) float64 {
	days := now.Sub(ts).Hours() / 24
	switch {
	case days <= 7:
		return 1.0
	case days <= 30:
		return 0.8
	case days <= 90:
		return 0.5
	default:
		return 0.3
	}
}

// RecallByEntities 根据实体列表查找相关记忆
func (s *EpisodicStore) RecallByEntities(ctx context.Context, entities []string, limit int) ([]*EpisodicMemory, error) {
	if len(entities) == 0 || limit <= 0 {
		return nil, nil
	}

	var conditions []string
	var args []any
	for _, e := range entities {
		conditions = append(conditions, "entities LIKE ?")
		args = append(args, "%"+e+"%")
	}

	query := `
		SELECT id, content, type, source, entities, ts, session_id, metadata
		FROM episodic_memories
		WHERE ` + joinOr(conditions) + `
		ORDER BY ts DESC
		LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("recall by entities: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func joinOr(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " OR "
		}
		result += p
	}
	return result
}

func (s *EpisodicStore) Recent(ctx context.Context, limit int) ([]*EpisodicMemory, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, type, source, entities, ts, session_id, metadata
		FROM episodic_memories
		ORDER BY ts DESC
		LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func (s *EpisodicStore) ByTimeRange(ctx context.Context, from, to time.Time) ([]*EpisodicMemory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, type, source, entities, ts, session_id, metadata
		FROM episodic_memories
		WHERE ts BETWEEN ? AND ?
		ORDER BY ts ASC`, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("query by time range: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func (s *EpisodicStore) Timeline(ctx context.Context, key string, limit int) ([]*EpisodicMemory, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, type, source, entities, ts, session_id, metadata
		FROM episodic_memories
		WHERE content LIKE ?
		ORDER BY ts DESC
		LIMIT ?`, "%"+key+"%", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("timeline query: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func scanMemories(rows *sql.Rows) ([]*EpisodicMemory, error) {
	var memories []*EpisodicMemory
	for rows.Next() {
		mem := &EpisodicMemory{Metadata: make(map[string]any)}
		var entitiesJSON, metadataJSON string
		var ts string
		err := rows.Scan(
			&mem.ID, &mem.Content, &mem.Type, &mem.Source,
			&entitiesJSON, &ts, &mem.SessionID, &metadataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		mem.Timestamp, _ = time.Parse("2006-01-02T15:04:05Z", ts)
		if mem.Timestamp.IsZero() {
			mem.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		}
		json.Unmarshal([]byte(entitiesJSON), &mem.Entities)
		json.Unmarshal([]byte(metadataJSON), &mem.Metadata)
		if mem.Metadata == nil {
			mem.Metadata = make(map[string]any)
		}
		memories = append(memories, mem)
	}
	return memories, nil
}

func scanMemoryWithEmbedding(rows *sql.Rows) (*EpisodicMemory, error) {
	mem := &EpisodicMemory{Metadata: make(map[string]any)}
	var entitiesJSON, metadataJSON string
	var ts string
	var embeddingBlob []byte
	err := rows.Scan(
		&mem.ID, &mem.Content, &mem.Type, &mem.Source,
		&entitiesJSON, &embeddingBlob, &ts, &mem.SessionID, &metadataJSON,
	)
	if err != nil {
		return nil, err
	}
	mem.Timestamp, _ = time.Parse("2006-01-02T15:04:05Z", ts)
	if mem.Timestamp.IsZero() {
		mem.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
	}
	json.Unmarshal([]byte(entitiesJSON), &mem.Entities)
	json.Unmarshal([]byte(metadataJSON), &mem.Metadata)
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]any)
	}
	if len(embeddingBlob) > 0 {
		mem.Embedding = blobToFloats(embeddingBlob)
	}
	return mem, nil
}

func cosineSim(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func floatsToBlob(v []float64) []byte {
	buf := make([]byte, len(v)*8)
	for i, f := range v {
		bits := math.Float64bits(f)
		buf[i*8+0] = byte(bits >> 56)
		buf[i*8+1] = byte(bits >> 48)
		buf[i*8+2] = byte(bits >> 40)
		buf[i*8+3] = byte(bits >> 32)
		buf[i*8+4] = byte(bits >> 24)
		buf[i*8+5] = byte(bits >> 16)
		buf[i*8+6] = byte(bits >> 8)
		buf[i*8+7] = byte(bits)
	}
	return buf
}

func blobToFloats(buf []byte) []float64 {
	n := len(buf) / 8
	v := make([]float64, n)
	for i := 0; i < n; i++ {
		bits := uint64(buf[i*8+0])<<56 |
			uint64(buf[i*8+1])<<48 |
			uint64(buf[i*8+2])<<40 |
			uint64(buf[i*8+3])<<32 |
			uint64(buf[i*8+4])<<24 |
			uint64(buf[i*8+5])<<16 |
			uint64(buf[i*8+6])<<8 |
			uint64(buf[i*8+7])
		v[i] = math.Float64frombits(bits)
	}
	return v
}

func (s *EpisodicStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM episodic_memories").Scan(&count)
	return count, err
}

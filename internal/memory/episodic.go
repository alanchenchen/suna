package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
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
		mem.ID = uuid.New().String()
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO episodic_memories (id, content, type, source, entities, embedding, ts, session_id, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID, mem.Content, mem.Type, mem.Source, string(entitiesJSON), embeddingBlob,
		mem.Timestamp, mem.SessionID, string(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("insert episodic memory: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO episodic_fts(rowid, content) VALUES (?, ?)`,
		mem.ID, mem.Content,
	)
	if err != nil {
		return fmt.Errorf("insert fts: %w", err)
	}
	return nil
}

func (s *EpisodicStore) SearchFTS(ctx context.Context, query string, limit int) ([]*EpisodicMemory, error) {
	if limit <= 0 {
		limit = 10
	}
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
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
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

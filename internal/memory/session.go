package memory

import (
	"context"
	"database/sql"
	"time"
)

// SessionStore only tracks model usage. Conversation recovery lives in conversation_state.
type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(db *sql.DB) *SessionStore {
	return &SessionStore{db: db}
}

// SaveUsage records one LLM call's token usage.
func (s *SessionStore) SaveUsage(ctx context.Context, sessionID, model string, inputTokens, outputTokens int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_log (session_id, model, input_tokens, output_tokens, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		sessionID, model, inputTokens, outputTokens, time.Now(),
	)
	return err
}

func (s *SessionStore) UsageSummary(ctx context.Context, since time.Time) (*UsageSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COUNT(*)
		FROM usage_log
		WHERE created_at >= ?`, since,
	)
	var sum UsageSummary
	if err := row.Scan(&sum.InputTokens, &sum.OutputTokens, &sum.Requests); err != nil {
		return nil, err
	}
	return &sum, nil
}

type UsageSummary struct {
	InputTokens  int
	OutputTokens int
	Requests     int
}

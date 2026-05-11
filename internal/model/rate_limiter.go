package model

import (
	"context"
	"sync"
	"time"
)

type RateLimiter struct {
	mu     sync.Mutex
	tokens map[string][]time.Time
	maxRPS int
}

func NewRateLimiter(maxRPS int) *RateLimiter {
	if maxRPS <= 0 {
		maxRPS = 5
	}
	return &RateLimiter{
		tokens: make(map[string][]time.Time),
		maxRPS: maxRPS,
	}
}

func (r *RateLimiter) Wait(ctx context.Context, ref string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-time.Second)
		tokens := r.tokens[ref]
		valid := tokens[:0]
		for _, t := range tokens {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) < r.maxRPS {
			r.tokens[ref] = append(valid, now)
			r.mu.Unlock()
			return nil
		}
		r.tokens[ref] = valid
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

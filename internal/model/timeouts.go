package model

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	LLMCompactIdleTimeout   = 5 * time.Minute
	LLMGuardReviewTimeout   = 60 * time.Second
	LLMSkillReviewTimeout   = 2 * time.Minute
	LLMMemoryCompactTimeout = 60 * time.Second
)

// ReadStreamTextWithIdle 读取只关心文本结果的 LLM stream，每收到 chunk 后重置空闲计时器。
func ReadStreamTextWithIdle(ctx context.Context, ch <-chan Chunk, timeout time.Duration, timeoutMessage string) (string, error) {
	if timeout <= 0 {
		return "", fmt.Errorf("LLM stream idle timeout must be positive")
	}
	if timeoutMessage == "" {
		timeoutMessage = fmt.Sprintf("LLM stream idle timeout (%s)", timeout)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(timeout)
	}

	var out strings.Builder
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return out.String(), nil
			}
			resetTimer()
			if chunk.Error != nil {
				return "", chunk.Error
			}
			out.WriteString(chunk.Content)
			if chunk.Done {
				return out.String(), nil
			}
		case <-timer.C:
			return "", fmt.Errorf("%s", timeoutMessage)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

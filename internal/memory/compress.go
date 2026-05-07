package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/model"
)

const (
	compressThreshold    = 0.8
	keepRecentTurns      = 10
	maxToolOutputLines   = 500
	maxToolOutputBytes   = 50 * 1024
)

type Compressor struct {
	fastProvider model.Provider
}

func NewCompressor(fastProvider model.Provider) *Compressor {
	return &Compressor{fastProvider: fastProvider}
}

func (c *Compressor) ShouldCompress(messages []model.Message, contextWindow int) bool {
	tokens := model.EstimateMessagesTokens(messages)
	return float64(tokens) > float64(contextWindow)*compressThreshold
}

func (c *Compressor) TruncateToolOutput(content string) string {
	if len(content) <= maxToolOutputBytes {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxToolOutputLines {
		return content[:maxToolOutputBytes] + "\n... (truncated)"
	}
	kept := lines[:maxToolOutputLines]
	result := strings.Join(kept, "\n")
	if len(result) > maxToolOutputBytes {
		result = result[:maxToolOutputBytes]
	}
	return result + "\n... (truncated, %d lines total)"
}

func (c *Compressor) CompressHistory(ctx context.Context, messages []model.Message) ([]model.Message, string, error) {
	if len(messages) <= keepRecentTurns {
		return messages, "", nil
	}

	keepStart := len(messages) - keepRecentTurns
	compressRegion := messages[:keepStart]
	keepRegion := messages[keepStart:]

	var sb strings.Builder
	for _, m := range compressRegion {
		role := string(m.Role)
		text := m.Text()
		if text != "" {
			sb.WriteString(fmt.Sprintf("[%s]: %s\n", role, text))
		}
		for _, tc := range m.ToolCalls {
			sb.WriteString(fmt.Sprintf("[%s/tool_call]: %s(%s)\n", role, tc.Name, tc.Arguments))
		}
	}

	summary := sb.String()
	if c.fastProvider != nil && len(summary) > 500 {
		ch, err := c.fastProvider.Complete(ctx, &model.CompletionRequest{
			System: "你是一个对话摘要助手。将以下对话历史压缩为简洁摘要。保留：用户意图、已完成操作、关键决策、当前进展。忽略：具体代码细节、工具返回细节、中间调试过程。用中文回复。",
			Messages: []model.Message{
				model.NewTextMessage(model.RoleUser, summary),
			},
			MaxTokens: 1000,
		})
		if err == nil {
			var full string
			for chunk := range ch {
				if chunk.Content != "" {
					full += chunk.Content
				}
				if chunk.Done {
					break
				}
			}
			if full != "" {
				summary = full
			}
		}
	}

	result := []model.Message{
		{
			Role:        model.RoleSystem,
			TextContent: "之前的对话摘要: " + summary,
			Content:     []model.ContentBlock{{Type: model.ContentText, Text: "之前的对话摘要: " + summary}},
		},
	}
	result = append(result, keepRegion...)
	return result, summary, nil
}

package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

const (
	compressThreshold   = 0.8
	keepRecentTurns     = 10
	maxToolOutputLines  = 500
	maxToolOutputBytes  = 50 * 1024
	AutoCompactMinTurns = 6
)

type Compressor struct {
	fastProvider model.Provider
	prompts      *prompt.Loader
}

func NewCompressor(fastProvider model.Provider) *Compressor {
	return &Compressor{fastProvider: fastProvider}
}

func (c *Compressor) SetPrompts(p *prompt.Loader) {
	c.prompts = p
}

func (c *Compressor) ShouldCompress(messages []model.Message, contextWindow int) bool {
	tokens := model.EstimateMessagesTokens(messages)
	return float64(tokens) > float64(contextWindow)*compressThreshold
}

// EstimateTokens 返回消息列表的估算 token 数
func (c *Compressor) EstimateTokens(messages []model.Message) int {
	return model.EstimateMessagesTokens(messages)
}

func (c *Compressor) TruncateToolOutput(content string) string {
	if len(content) <= maxToolOutputBytes {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxToolOutputLines {
		return truncateUTF8(content, maxToolOutputBytes) + "\n... (truncated)"
	}
	kept := lines[:maxToolOutputLines]
	result := strings.Join(kept, "\n")
	if len(result) > maxToolOutputBytes {
		result = truncateUTF8(result, maxToolOutputBytes)
	}
	return fmt.Sprintf("%s\n... (truncated, %d lines total)", result, len(lines))
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	for i := range s {
		if i > maxBytes {
			return s[:i]
		}
	}
	return s
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
		systemPrompt := "Compress the following conversation history into a concise summary. Keep: user intent, completed operations, key decisions, current progress. Ignore: specific code details, tool output details, intermediate debugging steps."
		if c.prompts != nil {
			if rendered, err := c.prompts.RenderCompress(summary); err == nil && rendered != "" {
				systemPrompt = ""
				summary = rendered
			}
		}
		req := &model.CompletionRequest{
			Messages: []model.Message{
				model.NewTextMessage(model.RoleUser, summary),
			},
			MaxTokens: 1000,
		}
		if systemPrompt != "" {
			req.System = systemPrompt
		}
		ch, err := c.fastProvider.Complete(ctx, req)
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
			TextContent: "Conversation summary: " + summary,
			Content:     []model.ContentBlock{{Type: model.ContentText, Text: "Conversation summary: " + summary}},
		},
	}
	result = append(result, keepRegion...)
	return result, summary, nil
}

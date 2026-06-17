package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

const (
	maxToolOutputLines           = 500
	maxToolOutputBytes           = 50 * 1024
	maxCompressAssistantBytes    = 6 * 1024
	maxCompressToolResultBytes   = 4 * 1024
	maxCompressToolArgumentBytes = 2 * 1024
	maxSessionStateTokens        = 3000
	minSessionStateTokens        = 1200
	minContextMarginTokens       = 2048
	recentChatUserTurns          = 6
	recentToolUserTurns          = 2
	maxRecentMessages            = 48
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

// EstimateTokens 返回消息列表的估算 token 数。
func (c *Compressor) EstimateTokens(messages []model.Message) int {
	return model.EstimateMessagesTokens(messages)
}

func TruncateToolOutputForContext(content string) string {
	if len(content) <= maxToolOutputBytes {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxToolOutputLines {
		return truncateUTF8(content, maxToolOutputBytes) + "\n... (truncated; full tool output omitted from model context)"
	}
	kept := lines[:maxToolOutputLines]
	result := strings.Join(kept, "\n")
	if len(result) > maxToolOutputBytes {
		result = truncateUTF8(result, maxToolOutputBytes)
	}
	return fmt.Sprintf("%s\n... (truncated, %d lines total; full tool output omitted from model context)", result, len(lines))
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

func formatCompressInput(messages []model.Message) string {
	var sb strings.Builder
	for i, m := range messages {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(formatCompressMessage(i+1, m))
	}
	return sb.String()
}

func formatCompressMessage(index int, m model.Message) string {
	var sb strings.Builder
	role := string(m.Role)
	text := strings.TrimSpace(m.Text())
	switch m.Role {
	case model.RoleUser:
		if text != "" {
			sb.WriteString(fmt.Sprintf("<user_message index=%q>\n%s\n</user_message>\n", fmt.Sprint(index), text))
		}
	case model.RoleAssistant:
		if text != "" {
			sb.WriteString(fmt.Sprintf("<assistant_message index=%q note=\"assistant proposal or response; preserve only if accepted or still relevant\">\n%s\n</assistant_message>\n", fmt.Sprint(index), truncateMiddle(text, maxCompressAssistantBytes)))
		}
	case model.RoleTool:
		if text != "" {
			sb.WriteString(fmt.Sprintf("<tool_result index=%q call_id=%q note=\"convert into action facts; do not keep raw logs\">\n%s\n</tool_result>\n", fmt.Sprint(index), m.ToolCallID, truncateMiddle(text, maxCompressToolResultBytes)))
		}
	default:
		if text != "" {
			sb.WriteString(fmt.Sprintf("<%s_message index=%q>\n%s\n</%s_message>\n", role, fmt.Sprint(index), truncateMiddle(text, maxCompressAssistantBytes), role))
		}
	}
	for _, tc := range m.ToolCalls {
		sb.WriteString(fmt.Sprintf("<tool_call index=%q name=%q>\n%s\n</tool_call>\n", fmt.Sprint(index), tc.Name, truncateMiddle(tc.Arguments, maxCompressToolArgumentBytes)))
	}
	return sb.String()
}

func truncateMiddle(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	half := maxBytes / 2
	if half <= 0 {
		return "... (truncated)"
	}
	prefix := truncateUTF8(s, half)
	suffixBudget := maxBytes - len(prefix)
	if suffixBudget <= 0 {
		return prefix + "\n... (truncated)"
	}
	suffixStart := len(s) - suffixBudget
	if suffixStart < 0 {
		suffixStart = 0
	}
	for suffixStart < len(s) && !isUTF8Boundary(s[suffixStart]) {
		suffixStart++
	}
	return prefix + fmt.Sprintf("\n... (truncated, %d bytes omitted) ...\n", len(s)-len(prefix)-len(s[suffixStart:])) + s[suffixStart:]
}

func isUTF8Boundary(b byte) bool {
	return b&0xC0 != 0x80
}

func (c *Compressor) CompressHistoryWithState(ctx context.Context, messages []model.Message, previousState string, contextWindow int) ([]model.Message, string, int, error) {
	return c.CompressHistoryWithStateBudget(ctx, messages, previousState, contextWindow, c.recentWindowTokenBudget(contextWindow))
}

func (c *Compressor) CompressHistoryWithStateBudget(ctx context.Context, messages []model.Message, previousState string, contextWindow, recentTokenBudget int) ([]model.Message, string, int, error) {
	return c.compressHistoryKeepingState(ctx, messages, previousState, chooseRecentKeepWithBudget(messages, contextWindow, recentTokenBudget), contextWindow)
}

func (c *Compressor) compressHistoryKeepingState(ctx context.Context, messages []model.Message, previousState string, keepRecent int, contextWindow int) ([]model.Message, string, int, error) {
	if len(messages) == 0 {
		return messages, "", 0, nil
	}
	cleanMessages := messages
	if len(cleanMessages) == 0 {
		return cleanMessages, "", 0, nil
	}

	keep := keepRecent
	if keep <= 0 {
		keep = chooseRecentKeep(cleanMessages, contextWindow)
	}
	if len(cleanMessages) <= keep {
		keep = len(cleanMessages) - 1
	}
	if keep < 1 {
		keep = 1
	}

	keepStart := len(cleanMessages) - keep
	if keepStart <= 0 {
		if strings.TrimSpace(previousState) == "" {
			return cleanMessages, "", 0, nil
		}
		// 已有 Session State 时，即使当前消息很少，也继续让压缩器合并最新上下文，避免状态长期停留在旧 compact 结果。
		keepStart = len(cleanMessages) - 1
		if keepStart < 1 {
			keepStart = 1
		}
	}
	compressRegion := cleanMessages[:keepStart]
	keepRegion := cleanMessages[keepStart:]

	compressInput := formatCompressInput(compressRegion)
	if strings.TrimSpace(compressInput) == "" && strings.TrimSpace(previousState) == "" {
		return messages, "", 0, nil
	}
	if c.fastProvider == nil {
		return nil, "", 0, fmt.Errorf("compressor model provider is not configured")
	}

	if c.prompts == nil {
		return nil, "", 0, fmt.Errorf("compress prompt loader is not configured")
	}
	// compress prompt 是 Session State 正确性的边界；模板渲染失败时不能静默退化，否则 previous state 可能丢失。
	promptText, err := c.prompts.RenderCompressWithState(previousState, compressInput)
	if err != nil {
		return nil, "", 0, fmt.Errorf("render compress prompt: %w", err)
	}
	if strings.TrimSpace(promptText) == "" {
		return nil, "", 0, fmt.Errorf("render compress prompt: empty prompt")
	}
	req := &model.CompletionRequest{
		Purpose: "compress",
		Messages: []model.Message{
			model.NewTextMessage(model.RoleUser, promptText),
		},
	}
	ch, err := c.fastProvider.Complete(ctx, req)
	if err != nil {
		return nil, "", 0, err
	}
	state, err := model.ReadStreamTextWithIdle(ctx, ch, model.LLMCompactIdleTimeout, "compact LLM stream timeout")
	if err != nil {
		return nil, "", 0, err
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, "", 0, fmt.Errorf("compressor returned empty session state")
	}

	return keepRegion, state, len(compressRegion), nil
}

func sessionStateMaxTokens(contextWindow int) int {
	return SessionStateTokenBudget(contextWindow)
}

func SessionStateTokenBudget(contextWindow int) int {
	if contextWindow <= 0 {
		return 2000
	}
	n := contextWindow / 100
	if n < minSessionStateTokens {
		n = minSessionStateTokens
	}
	if n > maxSessionStateTokens {
		n = maxSessionStateTokens
	}
	return n
}

func chooseRecentKeep(messages []model.Message, contextWindow int) int {
	return chooseRecentKeepWithBudget(messages, contextWindow, 0)
}

func chooseRecentKeepWithBudget(messages []model.Message, contextWindow, budget int) int {
	if len(messages) <= 1 {
		return len(messages)
	}
	// recent window 由代码按用户 turn 选择，不能交给 LLM 决定；这样 compact 后上下文稳定、可测试，且不破坏缓存命中。
	targetTurns := recentChatUserTurns
	if isToolHeavy(messages) {
		// tool-heavy 场景中 tool call/result 会快速挤占消息数，因此按更少用户 turn 保留当前工具链附近上下文。
		targetTurns = recentToolUserTurns
	}
	if budget <= 0 {
		budget = recentWindowTokenBudget(contextWindow, 0)
	}
	turns := 0
	keep := 0
	tokens := 0
	for i := len(messages) - 1; i >= 0 && keep < maxRecentMessages; i-- {
		msgTokens := model.EstimateMessagesTokens([]model.Message{messages[i]})
		// 至少保留最新一条消息；之后严格服从预算，避免 compact 后仍因 recent window 过大而超限。
		if keep > 0 && budget > 0 && tokens+msgTokens > budget {
			break
		}
		tokens += msgTokens
		keep++
		if messages[i].Role == model.RoleUser {
			turns++
			if turns >= targetTurns {
				break
			}
		}
	}
	if keep >= len(messages) {
		keep = len(messages) - 1
	}
	if keep < 1 {
		keep = 1
	}
	return keep
}

func (c *Compressor) recentWindowTokenBudget(contextWindow int) int {
	outputBudget := 0
	if c != nil && c.fastProvider != nil {
		outputBudget = c.fastProvider.MaxOutputTokens()
	}
	return recentWindowTokenBudget(contextWindow, outputBudget)
}

func recentWindowTokenBudget(contextWindow, outputBudget int) int {
	if contextWindow <= 0 {
		return 0
	}
	budget := contextWindow - outputBudget - sessionStateMaxTokens(contextWindow) - contextMargin(contextWindow)
	if budget < 1 {
		return 1
	}
	return budget
}

func contextMargin(contextWindow int) int {
	margin := contextWindow / 200
	if margin < minContextMarginTokens {
		return minContextMarginTokens
	}
	return margin
}

func isToolHeavy(messages []model.Message) bool {
	if len(messages) == 0 {
		return false
	}
	start := len(messages) - 24
	if start < 0 {
		start = 0
	}
	toolLike := 0
	for _, m := range messages[start:] {
		if m.Role == model.RoleTool || len(m.ToolCalls) > 0 {
			toolLike++
		}
	}
	return toolLike >= 3
}

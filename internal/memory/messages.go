package memory

import (
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/model"
)

const (
	maxToolOutputLines = 500
	maxToolOutputBytes = 50 * 1024

	OriginUser   = "user"
	OriginTool   = "tool"
	OriginLegacy = "legacy"
)

// ProjectMessages 只在副本上把媒体转换为稳定引用；原 run 仍保有可发送的图片块。
// 消息正文只存一份，恢复后的 Text 缓存由 Content 取得，不依赖临时字段。
func ProjectMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, message := range messages {
		out[i] = ProjectMessage(message)
	}
	return out
}

func ProjectMessage(message Message) Message {
	out := message
	hasImage := false
	for _, b := range message.Content {
		if b.Type == model.ContentImage {
			hasImage = true
			break
		}
	}
	if !hasImage && len(message.Content) <= 1 {
		text := message.Text()
		out.Content = []model.ContentBlock{{Type: model.ContentText, Text: text}}
		out.TextContent = text
		out.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
		return out
	}
	var text strings.Builder
	if len(message.Content) == 0 {
		text.WriteString(message.TextContent)
	} else {
		for _, block := range message.Content {
			value := block.Text
			if block.Type == model.ContentImage && block.Media != nil {
				value = ImageReference(*block.Media)
			} else if block.Type != model.ContentText {
				continue
			}
			if value == "" {
				continue
			}
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(value)
		}
	}
	out.TextContent = text.String()
	out.Content = []model.ContentBlock{{Type: model.ContentText, Text: out.TextContent}}
	out.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
	return out
}

func ImageReference(ref model.MediaRef) string {
	parts := []string{"[image: " + ref.Name}
	if ref.MimeType != "" {
		parts = append(parts, ref.MimeType)
	}
	if ref.Size > 0 {
		parts = append(parts, media.FormatSize(ref.Size))
	}
	source := ref.Path
	switch ref.Kind {
	case model.MediaAttachment:
		source = "attachment:" + ref.Name
	case model.MediaURL:
		source = ref.URL
	}
	if source != "" {
		parts = append(parts, "source="+source)
	}
	return strings.Join(parts, ", ") + "]"
}

// RecoverMessages 把真正中断的尾组转成普通观察。缺失返回只能标为未知，不能重造或重跑。
func RecoverMessages(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for i := 0; i < len(messages); {
		m := messages[i]
		if len(m.ToolCalls) == 0 {
			if m.Role != model.RoleTool {
				out = append(out, m)
			}
			i++
			continue
		}
		end := i + 1
		results := make(map[string]Message, len(m.ToolCalls))
		for end < len(messages) && messages[end].Role == model.RoleTool {
			results[messages[end].ToolCallID] = messages[end]
			end++
		}
		complete := len(results) == len(m.ToolCalls)
		for _, call := range m.ToolCalls {
			if _, ok := results[call.ID]; !ok {
				complete = false
			}
		}
		if complete {
			out = append(out, m)
			for _, call := range m.ToolCalls {
				out = append(out, results[call.ID])
			}
		} else {
			var text strings.Builder
			text.WriteString(m.Text())
			text.WriteString("\n\n[Historical tool operations interrupted; missing results are unknown, not authorization to repeat.]\n")
			for _, call := range m.ToolCalls {
				fmt.Fprintf(&text, "%s %s\n", call.Name, call.Arguments)
				if result, ok := results[call.ID]; ok {
					fmt.Fprintf(&text, "Observed result (%s): %s\n", result.ToolStatus, result.Text())
				} else {
					text.WriteString("Result: unknown.\n")
				}
			}
			observed := NewTextMessage(model.RoleAssistant, text.String())
			// 新恢复的观察尚未交给模型，不可提前进入历史归并。
			observed.Consumed = false
			out = append(out, observed)
		}
		i = end
	}
	return out
}

func IsUserMessage(m Message) bool { return m.Role == model.RoleUser && m.Origin != OriginTool }

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

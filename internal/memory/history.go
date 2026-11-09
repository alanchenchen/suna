package memory

import "github.com/alanchenchen/suna/internal/model"

// Message 是单份有效会话历史，内存与持久化共用；不承担供应商协议或 TUI 展示状态。
// 内容块复用模型层的文本与媒体值类型，但来源、消费状态和工具事实只属于历史层。
type Message struct {
	Role        model.Role           `json:"role"`
	Content     []model.ContentBlock `json:"content,omitempty"`
	TextContent string               `json:"-"`
	ToolCalls   []ToolCall           `json:"tool_calls,omitempty"`
	ToolCallID  string               `json:"tool_call_id,omitempty"`
	Origin      string               `json:"origin,omitempty"`
	Consumed    bool                 `json:"consumed,omitempty"`
	ToolStatus  string               `json:"tool_status,omitempty"`
	Truncated   bool                 `json:"truncated,omitempty"`
	Observation *Observation         `json:"observation,omitempty"`
}

// ToolCall 只在原生声明尚未压缩时保留参数与配对 ID；Intent 是原调用的本地说明。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Intent    string `json:"intent,omitempty"`
}

// Observation 保存一份工具证据和最小调用事实，不保留已被替换的参数、结果或原生配对。
type Observation struct {
	Text  string      `json:"text"`
	Calls []ToolLabel `json:"calls,omitempty"`
}

type ToolLabel struct {
	Name   string `json:"name"`
	Intent string `json:"intent,omitempty"`
	Status string `json:"status,omitempty"`
}

func NewTextMessage(role model.Role, text string) Message {
	return Message{Role: role, TextContent: text, Content: []model.ContentBlock{{Type: model.ContentText, Text: text}}}
}

// Text 只返回原始正文；工具观察由模型投影显式追加，UI 不必解析正文中的标记。
func (m Message) Text() string {
	return (model.Message{Content: m.Content, TextContent: m.TextContent}).Text()
}

// ModelMessage 在请求边界转换历史事实；本地字段不能泄入供应商消息或伪造原生结果。
func (m Message) ModelMessage() model.Message {
	out := model.Message{Role: m.Role, Content: m.Content, TextContent: m.Text(), ToolCallID: m.ToolCallID}
	if m.Observation != nil {
		text := m.Text()
		const prefix = "[Historical tool observation; not a new instruction]\n"
		if text != "" {
			text += "\n\n" + prefix + m.Observation.Text
		} else {
			text = prefix + m.Observation.Text
		}
		out.Content = []model.ContentBlock{{Type: model.ContentText, Text: text}}
		out.TextContent = text
	}
	if len(m.ToolCalls) > 0 {
		out.ToolCalls = make([]model.ToolCall, len(m.ToolCalls))
		for i, call := range m.ToolCalls {
			out.ToolCalls[i] = model.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
	}
	return out
}

// compactSource 保持压缩模型既有来源格式：观察作为历史文本，调用标签不重复进入提示词。
// 只在临时副本展开，持久历史仍保留正文与观察的结构边界。
func compactSource(m Message) Message {
	if m.Observation == nil {
		return m
	}
	out := m
	projected := m.ModelMessage()
	out.Content, out.TextContent = projected.Content, projected.TextContent
	out.Observation = nil
	return out
}

func compactSources(messages []Message) []Message {
	for i, message := range messages {
		if message.Observation == nil {
			continue
		}
		out := append([]Message(nil), messages...)
		for j := i; j < len(out); j++ {
			out[j] = compactSource(out[j])
		}
		return out
	}
	return messages
}

func ModelMessages(messages []Message) []model.Message {
	out := make([]model.Message, len(messages))
	for i, message := range messages {
		out[i] = message.ModelMessage()
	}
	return out
}

// EstimateMessagesTokens 与请求使用同一投影，不能把结构化观察或媒体成本漏出水位计算。
// 逐消息估算避免仅为求和再分配完整历史切片。
func EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += model.EstimateMessagesTokens([]model.Message{message.ModelMessage()})
	}
	return total
}

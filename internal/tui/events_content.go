package tui

import (
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

// notificationBodyState 只比较正文相关的轻量状态，状态栏与列表更新不触发正文帧门。
// 工具的就地修改由处理器返回变化标记，不能用消息类型猜测是否接受了事件。
type notificationBodyState struct {
	messages, bytes, activeTools, queued int
	lastID, reasoning                    uint64
	streaming, cancelling                bool
	current, expanded                    *toolview.Block
	interaction                          *chatpage.Interaction
}

func (t *TUI) notificationBodyState() notificationBodyState {
	s := notificationBodyState{
		messages: len(t.chat.Messages), activeTools: len(t.chat.ActiveTools),
		queued: len(t.chat.InteractionQueue), reasoning: t.chat.ExpandedReasoningID,
		current: t.chat.CurrentToolBlock, expanded: t.chat.ExpandedBlock,
		interaction: t.chat.ActiveInteraction,
		cancelling:  t.cancelling && len(t.chat.ActiveTools) > 0,
	}
	if len(t.chat.Messages) > 0 {
		msg := &t.chat.Messages[len(t.chat.Messages)-1]
		s.lastID, s.streaming = msg.ID, msg.Streaming
		if msg.Stream != nil {
			s.bytes = msg.Stream.Raw.Len()
		} else if text, ok := msg.Content.(string); ok {
			s.bytes = len(text)
		}
	}
	return s
}

package chat

import (
	"time"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (m *Model) ResetToolState() {
	m.ActiveTools = make(map[string]*toolview.Entry)
	m.ToolStartTimes = make(map[string]time.Time)
	m.CurrentToolBlock = nil
	m.SelectedToolID = ""
	m.SubtaskCursor = 0
	m.SubtaskCursorUserSet = false
	m.SubtaskToolCursor = 0
	m.SubtaskToolCursorUserSet = false
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
}

func (m *Model) StartLLMWait(now time.Time) {
	m.Loading = true
	m.Phase = PhaseFirstLLM
	m.PhaseStart = now
	m.StreamStart = now
	m.FollowBottom = true
	m.LastWaitingTool = ""
}

func (m *Model) AppendMessage(msg Msg) {
	m.CurrentToolBlock = nil
	m.SubtaskCursor = 0
	m.SubtaskCursorUserSet = false
	m.SubtaskToolCursor = 0
	m.SubtaskToolCursorUserSet = false
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
	m.Messages = append(m.Messages, msg)
}

func (m *Model) AppendStreamMessage(role, chunk string, now time.Time) {
	if chunk == "" {
		return
	}
	if len(m.Messages) > 0 && m.Messages[len(m.Messages)-1].Role == role {
		msg := &m.Messages[len(m.Messages)-1]
		if msg.Stream == nil {
			prev, _ := msg.Content.(string)
			msg.Stream = &StreamingTextState{}
			msg.Stream.Append(prev)
		}
		msg.Stream.Append(chunk)
		msg.Streaming = true
		if msg.StartedAt.IsZero() {
			msg.StartedAt = now
		}
		msg.EndedAt = time.Time{}
		// 流式内容只追加到 builder，避免 prev+chunk 对长回复反复复制。
		return
	}
	m.FinishStreamingMessages(now)
	stream := &StreamingTextState{}
	stream.Append(chunk)
	m.AppendMessage(Msg{Role: role, Streaming: true, StartedAt: now, Stream: stream})
}

func (m *Model) FinishStreamingMessages(now time.Time) {
	for i := range m.Messages {
		if m.Messages[i].Streaming {
			m.Messages[i].Streaming = false
			if m.Messages[i].EndedAt.IsZero() {
				m.Messages[i].EndedAt = now
			}
			if m.Messages[i].Stream != nil {
				m.Messages[i].Content = m.Messages[i].Stream.Text()
				m.Messages[i].Stream = nil
			}
			m.Messages[i].Render = MsgRenderCache{}
		}
	}
}

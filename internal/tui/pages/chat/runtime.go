package chat

import (
	"time"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (m *Model) ResetToolState() {
	m.ActiveTools = make(map[string]*toolview.Entry)
	m.ToolStartTimes = make(map[string]time.Time)
	m.CurrentToolBlock = nil
	m.CloseToolBlockWhenIdle = false
	m.ExpandedBlock = nil
	m.ExpandedBoxKind = ""
	m.ExpandedBlockCursor = 0
	m.ExpandedBlockDetailScroll = 0
	m.SubtaskCursor = 0
	m.SubtaskCursorUserSet = false
	m.SubtaskToolCursor = 0
	m.SubtaskToolCursorUserSet = false
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
	m.SubtaskResultScroll = 0
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
	m.appendMessage(msg)
}

func (m *Model) appendMessage(msg Msg) {
	if msg.ID == 0 {
		m.NextMessageID++
		msg.ID = m.NextMessageID
	} else if msg.ID > m.NextMessageID {
		m.NextMessageID = msg.ID
	}
	m.Messages = append(m.Messages, msg)
	m.MarkMessageDirty(len(m.Messages) - 1)
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
		m.MarkMessageDirty(len(m.Messages) - 1)
		// 流式内容只追加到 builder，避免 prev+chunk 对长回复反复复制。
		return
	}
	m.FinishStreamingMessages(now)
	stream := &StreamingTextState{}
	stream.Append(chunk)
	m.AppendMessage(Msg{Role: role, Streaming: true, StartedAt: now, Stream: stream})
}

func (m *Model) FinishStreamingMessages(now time.Time) {
	finish := func(i int) {
		if i < 0 || i >= len(m.Messages) {
			return
		}
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
			m.MarkMessageDirty(i)
		}
	}
	if m.transcript.ready {
		// 已同步的流式消息登记在动态集合中，尚未同步的登记在脏集合中，
		// 避免每个 tool.start 都扫描整个长会话。
		for i := range m.transcript.dynamic {
			finish(i)
		}
		for i := range m.transcript.dirty {
			finish(i)
		}
		return
	}
	for i := range m.Messages {
		finish(i)
	}
}

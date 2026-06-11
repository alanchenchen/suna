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
	m.Messages = append(m.Messages, msg)
}

func (m *Model) AppendStreamMessage(role, chunk string, now time.Time) {
	if chunk == "" {
		return
	}
	if len(m.Messages) > 0 && m.Messages[len(m.Messages)-1].Role == role {
		prev, _ := m.Messages[len(m.Messages)-1].Content.(string)
		msg := &m.Messages[len(m.Messages)-1]
		msg.Content = prev + chunk
		msg.Streaming = true
		if msg.StartedAt.IsZero() {
			msg.StartedAt = now
		}
		msg.EndedAt = time.Time{}
		msg.Render = MsgRenderCache{}
		return
	}
	m.FinishStreamingMessages(now)
	m.AppendMessage(Msg{Role: role, Content: chunk, Streaming: true, StartedAt: now})
}

func (m *Model) FinishStreamingMessages(now time.Time) {
	for i := range m.Messages {
		if m.Messages[i].Streaming {
			m.Messages[i].Streaming = false
			if m.Messages[i].EndedAt.IsZero() {
				m.Messages[i].EndedAt = now
			}
			m.Messages[i].Render = MsgRenderCache{}
		}
	}
}

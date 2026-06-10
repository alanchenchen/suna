package events

import (
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

const (
	NotifyCompactError = "compact.error"
	NotifyConfigError  = "config.error"
	NotifyMCPError     = "mcp.error"
)

// Notification 是 local transport 推给 TUI event pump 的原始 daemon 事件。
type Notification struct {
	Method string
	Params json.RawMessage
}

// NotificationMsg 是进入 Bubble Tea Update 后的强类型 daemon 事件。
type NotificationMsg interface{ isNotificationMsg() }

type StreamMsg struct{ Params protocol.StreamParams }
type ReasoningMsg struct{ Params protocol.StreamParams }
type UsageMsg struct{ Params protocol.UsageParams }
type AskUserMsg struct{ Params protocol.AskUserParams }
type GuardConfirmMsg struct{ Params protocol.GuardConfirmParams }

type ToolStartMsg struct{ Params protocol.ToolStartParams }
type ToolGuardMsg struct{ Params protocol.ToolGuardParams }
type ToolEndMsg struct{ Params protocol.ToolEndParams }

type DaemonStateMsg struct{ Params protocol.DaemonStateParams }
type CompactResultMsg struct{ Params protocol.CompactResult }
type MemoryListMsg struct{ Params protocol.MemoryListResult }
type SessionRestoreStatusMsg struct{ Params protocol.SessionRestoreStatus }
type SessionRestoreMessageMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type DaemonFullStatusMsg struct{ Params protocol.DaemonStatusParams }
type ConfigStateMsg struct{ Params protocol.ConfigParams }
type SkillListMsg struct{ Params protocol.SkillListResult }
type MCPListMsg struct{ Params protocol.MCPListResult }
type SkillLoadMsg struct{ Params protocol.SkillLoadParams }
type SkillReviewMsg struct{ Params protocol.SkillReviewParams }
type AttachmentStatusMsg struct {
	Params protocol.AttachmentStatusResult
}
type RequestErrorMsg struct {
	Scope   string
	Message string
}
type UnknownNotificationMsg struct{ Raw Notification }

func (StreamMsg) isNotificationMsg()                {}
func (ReasoningMsg) isNotificationMsg()             {}
func (UsageMsg) isNotificationMsg()                 {}
func (AskUserMsg) isNotificationMsg()               {}
func (GuardConfirmMsg) isNotificationMsg()          {}
func (ToolStartMsg) isNotificationMsg()             {}
func (ToolGuardMsg) isNotificationMsg()             {}
func (ToolEndMsg) isNotificationMsg()               {}
func (DaemonStateMsg) isNotificationMsg()           {}
func (CompactResultMsg) isNotificationMsg()         {}
func (MemoryListMsg) isNotificationMsg()            {}
func (SessionRestoreStatusMsg) isNotificationMsg()  {}
func (SessionRestoreMessageMsg) isNotificationMsg() {}
func (DaemonFullStatusMsg) isNotificationMsg()      {}
func (ConfigStateMsg) isNotificationMsg()           {}
func (SkillListMsg) isNotificationMsg()             {}
func (MCPListMsg) isNotificationMsg()               {}
func (SkillLoadMsg) isNotificationMsg()             {}
func (SkillReviewMsg) isNotificationMsg()           {}
func (AttachmentStatusMsg) isNotificationMsg()      {}
func (RequestErrorMsg) isNotificationMsg()          {}
func (UnknownNotificationMsg) isNotificationMsg()   {}

func Decode(notif Notification) tea.Msg {
	switch notif.Method {
	case protocol.NotifyStream:
		return decodeParams[protocol.StreamParams](notif, func(p protocol.StreamParams) tea.Msg { return StreamMsg{Params: p} })
	case protocol.NotifyReasoning:
		return decodeParams[protocol.StreamParams](notif, func(p protocol.StreamParams) tea.Msg { return ReasoningMsg{Params: p} })
	case protocol.NotifyUsage:
		return decodeParams[protocol.UsageParams](notif, func(p protocol.UsageParams) tea.Msg { return UsageMsg{Params: p} })
	case protocol.NotifyToolStart:
		return decodeParams[protocol.ToolStartParams](notif, func(p protocol.ToolStartParams) tea.Msg { return ToolStartMsg{Params: p} })
	case protocol.NotifyToolGuard:
		return decodeParams[protocol.ToolGuardParams](notif, func(p protocol.ToolGuardParams) tea.Msg { return ToolGuardMsg{Params: p} })
	case protocol.NotifyToolEnd:
		return decodeParams[protocol.ToolEndParams](notif, func(p protocol.ToolEndParams) tea.Msg { return ToolEndMsg{Params: p} })
	case protocol.NotifyAskUser:
		return decodeParams[protocol.AskUserParams](notif, func(p protocol.AskUserParams) tea.Msg { return AskUserMsg{Params: p} })
	case protocol.NotifyGuardConfirm:
		return decodeParams[protocol.GuardConfirmParams](notif, func(p protocol.GuardConfirmParams) tea.Msg { return GuardConfirmMsg{Params: p} })
	case protocol.NotifyDaemonState:
		return decodeParams[protocol.DaemonStateParams](notif, func(p protocol.DaemonStateParams) tea.Msg { return DaemonStateMsg{Params: p} })
	case protocol.NotifyCompactResult:
		return decodeParams[protocol.CompactResult](notif, func(p protocol.CompactResult) tea.Msg { return CompactResultMsg{Params: p} })
	case NotifyCompactError, NotifyConfigError, NotifyMCPError:
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(notif.Params, &p)
		return RequestErrorMsg{Scope: notif.Method, Message: p.Message}
	case protocol.NotifyMemoryListResult:
		return decodeParams[protocol.MemoryListResult](notif, func(p protocol.MemoryListResult) tea.Msg { return MemoryListMsg{Params: p} })
	case protocol.NotifySessionRestoreMsg:
		return decodeParams[SessionRestoreMessageMsg](notif, func(p SessionRestoreMessageMsg) tea.Msg { return p })
	case protocol.NotifySessionRestoreStatus:
		return decodeParams[protocol.SessionRestoreStatus](notif, func(p protocol.SessionRestoreStatus) tea.Msg { return SessionRestoreStatusMsg{Params: p} })
	case protocol.NotifyDaemonFullStatus:
		return decodeParams[protocol.DaemonStatusParams](notif, func(p protocol.DaemonStatusParams) tea.Msg { return DaemonFullStatusMsg{Params: p} })
	case protocol.NotifyConfigState:
		return decodeParams[protocol.ConfigParams](notif, func(p protocol.ConfigParams) tea.Msg { return ConfigStateMsg{Params: p} })
	case protocol.MethodSkillList:
		return decodeParams[protocol.SkillListResult](notif, func(p protocol.SkillListResult) tea.Msg { return SkillListMsg{Params: p} })
	case protocol.MethodMCPList:
		return decodeParams[protocol.MCPListResult](notif, func(p protocol.MCPListResult) tea.Msg { return MCPListMsg{Params: p} })
	case protocol.NotifySkillLoad:
		return decodeParams[protocol.SkillLoadParams](notif, func(p protocol.SkillLoadParams) tea.Msg { return SkillLoadMsg{Params: p} })
	case protocol.NotifySkillReview:
		return decodeParams[protocol.SkillReviewParams](notif, func(p protocol.SkillReviewParams) tea.Msg { return SkillReviewMsg{Params: p} })
	case protocol.MethodAttachmentStatus:
		return decodeParams[protocol.AttachmentStatusResult](notif, func(p protocol.AttachmentStatusResult) tea.Msg { return AttachmentStatusMsg{Params: p} })
	default:
		return UnknownNotificationMsg{Raw: notif}
	}
}

func decodeParams[T any](notif Notification, wrap func(T) tea.Msg) tea.Msg {
	var p T
	if err := json.Unmarshal(notif.Params, &p); err != nil {
		return RequestErrorMsg{Scope: NotifyConfigError, Message: err.Error()}
	}
	return wrap(p)
}

const StreamFlushInterval = 8 * time.Millisecond

type Batcher struct {
	Send   func(tea.Msg)
	stream streamAccumulator
	reason streamAccumulator
	order  []string
	timer  *time.Timer
}

type streamAccumulator struct {
	params protocol.StreamParams
	has    bool
}

func (b *Batcher) Run(ch <-chan Notification) {
	for {
		select {
		case notif, ok := <-ch:
			if !ok {
				b.flushAll()
				return
			}
			b.handle(notif)
		case <-b.timerC():
			b.flushAll()
		}
	}
}

func (b *Batcher) handle(notif Notification) {
	if IsTextStreamNotification(notif) {
		b.accumulate(notif)
		b.ensureTimer()
		return
	}
	// 非文本事件必须即时显示；先 flush 已合并文本，避免 tool/done 被历史 delta 堵住。
	b.flushAll()
	b.send(notif)
}

func (b *Batcher) accumulate(notif Notification) {
	var p protocol.StreamParams
	if err := json.Unmarshal(notif.Params, &p); err != nil {
		b.flushAll()
		b.send(notif)
		return
	}
	if p.Done {
		b.flushAll()
		b.send(notif)
		return
	}
	if len(b.order) > 0 && b.order[len(b.order)-1] != notif.Method {
		b.flushAll()
	}
	acc := &b.stream
	if notif.Method == protocol.NotifyReasoning {
		acc = &b.reason
	}
	if !acc.has {
		b.order = append(b.order, notif.Method)
	}
	acc.params.Chunk += p.Chunk
	acc.params.ID = p.ID
	acc.has = true
}

func (b *Batcher) flushAll() {
	b.stopTimer()
	for _, method := range b.order {
		b.flush(method)
	}
	b.order = nil
}

func (b *Batcher) flush(method string) {
	acc := &b.stream
	if method == protocol.NotifyReasoning {
		acc = &b.reason
	}
	if !acc.has {
		return
	}
	params := acc.params
	*acc = streamAccumulator{}
	data, _ := json.Marshal(params)
	b.send(Notification{Method: method, Params: data})
}

func (b *Batcher) send(notif Notification) {
	if b.Send != nil {
		b.Send(Decode(notif))
	}
}

func (b *Batcher) ensureTimer() {
	if b.timer == nil {
		b.timer = time.NewTimer(StreamFlushInterval)
	}
}

func (b *Batcher) stopTimer() {
	if b.timer == nil {
		return
	}
	if !b.timer.Stop() {
		select {
		case <-b.timer.C:
		default:
		}
	}
	b.timer = nil
}

func (b *Batcher) timerC() <-chan time.Time {
	if b.timer == nil {
		return nil
	}
	return b.timer.C
}

func IsTextStreamNotification(notif Notification) bool {
	return notif.Method == protocol.NotifyStream || notif.Method == protocol.NotifyReasoning
}

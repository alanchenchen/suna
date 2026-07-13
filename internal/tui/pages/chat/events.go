package chat

import (
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (m *Model) HandleStreamStart(now time.Time) {
	m.ClearStatusLabel()
	m.LastWaitingTool = ""
	if m.Phase == PhaseFirstLLM || m.Phase == PhaseThinking || m.Phase == PhaseWaitingAfterTool {
		m.Phase = PhaseLLM
		m.PhaseStart = now
	}
}

func (m *Model) HandleReasoningStart(now time.Time) {
	m.ClearStatusLabel()
	m.LastWaitingTool = ""
	if m.Phase == PhaseFirstLLM || m.Phase == PhaseLLM || m.Phase == PhaseWaitingAfterTool {
		m.Phase = PhaseThinking
		m.PhaseStart = now
	}
}

func (m *Model) EnqueueAskUser(p protocol.AskUserParams) {
	ask := &AskUserView{ID: p.ID, Question: p.Question, Options: p.Options, AllowCustom: p.AllowCustom || len(p.Options) == 0}
	m.EnqueueInteraction(Interaction{Kind: InteractionAskUser, ID: p.ID, Ask: ask})
}

func (m *Model) StartTool(p protocol.ToolStartParams, id string, now time.Time) *toolview.Entry {
	if m.ActiveTools == nil {
		m.ActiveTools = make(map[string]*toolview.Entry)
	}
	if m.ToolStartTimes == nil {
		m.ToolStartTimes = make(map[string]time.Time)
	}
	m.Phase = PhaseTool
	m.PhaseStart = now
	m.StatusLabel = ""
	m.LastWaitingTool = ""
	m.Loading = true
	parentID, localID := toolview.ParseSubtaskID(id)
	if parentID != "" && !toolview.HasSubtaskParent(m.CurrentToolBlock, parentID) {
		parentID = ""
		localID = id
	}
	m.LastAssistantText = ""
	te := &toolview.Entry{
		ID:        id,
		LocalID:   localID,
		ParentID:  parentID,
		RawName:   p.Tool,
		Name:      toolview.DisplayName(p.Tool),
		Intent:    p.Intent,
		Params:    toolview.FormatParams(p.Params),
		ParamsRaw: p.Params,
		Summary:   toolview.ParamSummary(p.Tool, p.Params),
		Status:    toolview.StatusRunning,
		StartedAt: now,
	}
	m.ActiveTools[id] = te
	m.ToolStartTimes[id] = te.StartedAt
	m.EnsureToolBlock().Add(te)
	if m.SelectedToolID == "" {
		m.SelectedToolID = id
	}
	return te
}

func (m *Model) ApplyToolGuard(p protocol.ToolGuardParams) {
	if te := m.FindTool(p.ToolCallID); te != nil {
		te.Guard = &toolview.GuardInfo{Risk: p.Risk, Decision: p.Decision, Source: p.Source, Reason: p.Reason, Suggestion: p.Suggestion, ReviewCode: p.ReviewCode, ReviewMessage: p.ReviewMessage}
	}
}

func (m *Model) EndTool(p protocol.ToolEndParams, id string, now time.Time) {
	te := m.ActiveTools[id]
	if te == nil {
		// AskUser 等等待用户输入的工具会先让 Chat 退出 running 状态，active map 可能已清空；
		// tool_end 回来时仍要回到历史 transcript 里的 tool entry，把 running 更新为 done/error。
		te = m.FindTool(id)
	}
	if te != nil {
		if start, ok := m.ToolStartTimes[id]; ok {
			te.Duration = now.Sub(start)
			delete(m.ToolStartTimes, id)
		} else if !te.StartedAt.IsZero() {
			te.Duration = now.Sub(te.StartedAt)
		}
		te.EndedAt = now
		te.ResultTruncated = p.ResultTruncated
		te.ResultBytes = p.ResultBytes
		te.Metadata = p.Metadata
		te.Result = p.Result
		if p.Error {
			te.Status = toolview.StatusError
		} else {
			te.Status = toolview.StatusDone
		}
	}
	delete(m.ActiveTools, id)
	delete(m.ToolStartTimes, id)
	if te != nil && toolview.IsSubtask(te) {
		// 子任务根调用结束后，等待仍在收束的子任务事件结束，再封闭该工具块。
		m.CloseToolBlockWhenIdle = true
	}
	if !m.HasRunningTools() {
		m.Phase = PhaseWaitingAfterTool
		m.PhaseStart = now
		m.LastAssistantText = ""
		m.LastWaitingTool = ""
		if te != nil && toolview.IsSubtask(te) {
			m.LastWaitingTool = "spawn"
		}
		if m.CloseToolBlockWhenIdle {
			// 子任务根调用结束后，后续主 Agent 工具属于新的执行批次，不能继续追加到该子任务块。
			m.CurrentToolBlock = nil
			m.CloseToolBlockWhenIdle = false
			m.SelectedToolID = ""
			m.SubtaskCursor = 0
			m.SubtaskCursorUserSet = false
			m.SubtaskToolCursor = 0
			m.SubtaskToolCursorUserSet = false
			m.SubtaskToolDetailExpanded = false
			m.SubtaskToolDetailScroll = 0
		}
	}
}

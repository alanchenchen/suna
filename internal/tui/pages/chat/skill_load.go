package chat

import (
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (m *Model) StartSkillLoad(p protocol.ToolStartParams, id string, now time.Time) *SkillLoadView {
	if m.ActiveTools == nil {
		m.ActiveTools = make(map[string]*toolview.Entry)
	}
	if m.ToolStartTimes == nil {
		m.ToolStartTimes = make(map[string]time.Time)
	}
	name, _ := p.Params["name"].(string)
	view := &SkillLoadView{ID: id, Name: strings.TrimSpace(name), Status: "loading", StartedAt: now}
	entry := &toolview.Entry{ID: id, RawName: p.Tool, ParamsRaw: p.Params, Status: toolview.StatusRunning, StartedAt: now}
	m.ActiveTools[id] = entry
	m.ToolStartTimes[id] = now
	m.Messages = append(m.Messages, Msg{Role: "skill", Content: view})
	m.Phase = PhaseTool
	m.PhaseStart = now
	m.StatusLabel = ""
	m.LastWaitingTool = ""
	m.Loading = true
	m.LastAssistantText = ""
	return view
}

func (m *Model) EndSkillLoad(p protocol.ToolEndParams, id string, now time.Time) bool {
	entry := m.ActiveTools[id]
	if entry == nil || entry.RawName != "skill_load" {
		return false
	}
	view := m.findSkillLoad(id)
	if view != nil {
		view.Status = "loaded"
		view.EndedAt = now
		view.Duration = now.Sub(entry.StartedAt)
		view.Error = p.Error
	}
	delete(m.ActiveTools, id)
	delete(m.ToolStartTimes, id)
	m.finishSkillLoadPhase(now)
	return true
}

func (m *Model) finishSkillLoadPhase(now time.Time) {
	if m.HasRunningTools() {
		return
	}
	m.Phase = PhaseWaitingAfterTool
	m.PhaseStart = now
	m.LastAssistantText = ""
	m.LastWaitingTool = ""
}

func (m *Model) findSkillLoad(id string) *SkillLoadView {
	for i := len(m.Messages) - 1; i >= 0; i-- {
		view, ok := m.Messages[i].Content.(*SkillLoadView)
		if ok && view != nil && view.ID == id {
			return view
		}
	}
	return nil
}

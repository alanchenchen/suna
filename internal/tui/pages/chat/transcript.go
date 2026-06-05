package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

type TranscriptDeps struct {
	Width int

	SunaLabel     string
	AskHelp       string
	AskChoiceHelp string

	RenderSunaHeader     func(string) string
	RenderUserMessage    func(any, int) string
	RenderAssistant      func(*Msg) string
	RenderReasoning      func(*Msg) string
	RenderToolBlock      func(*toolview.Block) string
	RenderError          func(string) string
	RenderRestoreSummary func(string) string
	RenderSkillLoad      func(protocol.SkillLoadParams) string
	RenderSkillReview    func(protocol.SkillReviewParams) string
	RenderSystem         func(string) string
	RenderAskSelected    func(string) string
	RenderAskOption      func(string) string
	RenderAskHelp        func(string) string
	RenderModelPicker    func() string
	RenderStatusLine     func() string
	HasVisibleProgress   func() bool
}

func (m *Model) SyncTranscript(deps TranscriptDeps) {
	followBottom := m.ForceBottom || m.FollowBottom || m.Viewport.AtBottom()
	if m.ForceBottom {
		m.ForceBottom = false
	}
	m.Viewport.SetContent(m.RenderTranscript(deps))
	if followBottom {
		m.Viewport.GotoBottom()
		m.FollowBottom = true
		return
	}
	m.FollowBottom = m.Viewport.AtBottom()
}

// RenderTranscript 负责 Chat transcript 的结构编排；具体样式与 markdown 渲染由 root 注入。
func (m Model) RenderTranscript(deps TranscriptDeps) string {
	var sb strings.Builder
	inSunaBlock := false
	renderSunaHeader := func() {
		if inSunaBlock {
			return
		}
		if deps.RenderSunaHeader != nil {
			sb.WriteString(deps.RenderSunaHeader(deps.SunaLabel))
		}
		inSunaBlock = true
	}

	for i := range m.Messages {
		msg := &m.Messages[i]
		switch msg.Role {
		case "user":
			if deps.RenderUserMessage != nil {
				sb.WriteString("\n" + deps.RenderUserMessage(msg.Content, maxInt(20, deps.Width-8)) + "\n")
			}
			inSunaBlock = false
		case "assistant":
			renderSunaHeader()
			if deps.RenderAssistant != nil {
				sb.WriteString(deps.RenderAssistant(msg) + "\n")
			}
		case "reasoning":
			renderSunaHeader()
			if deps.RenderReasoning != nil {
				sb.WriteString(deps.RenderReasoning(msg))
			}
		case "tool":
			if v, ok := msg.Content.(*toolview.Block); ok {
				renderSunaHeader()
				if deps.RenderToolBlock != nil {
					sb.WriteString(deps.RenderToolBlock(v))
				}
			}
		case "error":
			content, _ := msg.Content.(string)
			if deps.RenderError != nil {
				sb.WriteString("\n" + deps.RenderError(content) + "\n")
			}
			inSunaBlock = false
		case "restore_summary":
			content, _ := msg.Content.(string)
			if deps.RenderRestoreSummary != nil {
				sb.WriteString("\n" + deps.RenderRestoreSummary(content) + "\n")
			}
			inSunaBlock = false
		case "panel":
			content, _ := msg.Content.(string)
			sb.WriteString("\n" + content + "\n")
			inSunaBlock = false
		case "skill":
			if p, ok := msg.Content.(protocol.SkillLoadParams); ok && deps.RenderSkillLoad != nil {
				sb.WriteString("\n" + deps.RenderSkillLoad(p) + "\n")
			}
			inSunaBlock = false
		case "skill_review":
			if p, ok := msg.Content.(protocol.SkillReviewParams); ok && deps.RenderSkillReview != nil {
				sb.WriteString("\n" + deps.RenderSkillReview(p) + "\n")
			}
			inSunaBlock = false
		default:
			content, _ := msg.Content.(string)
			if deps.RenderSystem != nil {
				sb.WriteString("\n" + deps.RenderSystem(content) + "\n")
			}
			inSunaBlock = false
		}
	}

	if m.PendingAskID != "" && len(m.PendingAskOptions) > 0 {
		for i, opt := range m.PendingAskOptions {
			if i == m.PendingAskCursor {
				if deps.RenderAskSelected != nil {
					sb.WriteString(deps.RenderAskSelected(opt))
				}
			} else if deps.RenderAskOption != nil {
				sb.WriteString(deps.RenderAskOption(opt))
			}
		}
		help := deps.AskHelp
		if !m.PendingAskCustom {
			help = deps.AskChoiceHelp
		}
		if deps.RenderAskHelp != nil {
			sb.WriteString(deps.RenderAskHelp(help))
		}
	}
	if m.ModelPickerOpen && deps.RenderModelPicker != nil {
		sb.WriteString(deps.RenderModelPicker())
	}

	visibleProgress := false
	if deps.HasVisibleProgress != nil {
		visibleProgress = deps.HasVisibleProgress()
	}
	if m.Loading && m.PhaseStart.After(time.Time{}) && !visibleProgress {
		renderSunaHeader()
		if deps.RenderStatusLine != nil {
			sb.WriteString(deps.RenderStatusLine())
		}
	}
	return sb.String()
}

func AskSelectedLine(icon, value string) string { return fmt.Sprintf("  %s %s\n", icon, value) }

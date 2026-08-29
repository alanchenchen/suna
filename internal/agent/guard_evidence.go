package agent

import (
	"encoding/json"
	"strings"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools/agenttools"
)

const (
	guardEvidenceBudget         = 2048
	guardEvidenceMessageScan    = 64
	guardEvidenceUserLimit      = 3
	guardEvidenceAskLimit       = 2
	guardEvidenceActionLimit    = 4
	guardEvidenceUserItemRunes  = 180
	guardEvidenceAskItemRunes   = 220
	guardEvidenceActionRunes    = 140
	guardEvidenceSessionRunes   = 500
	guardEvidenceRationaleRunes = 360
)

type guardEvidenceBuilder struct {
	remaining int
	sections  []string
}

func buildGuardEvidence(messages []model.Message, riskDecisions, agentActions []string, sessionState, rationale string, extraUsers ...string) string {
	users, answers := recentGuardUserEvidence(messages)
	// subtask 场景：任务描述作为注入的用户意图（不受 64 条扫描窗口限制）。
	// subtask 的任务描述是第一条也是唯一一条 user 消息，长任务（>64 条）时会被窗口挤出，
	// 导致 Guard review 看不到用户意图而保守 modify。
	for _, extra := range extraUsers {
		if trimmed := strings.TrimSpace(extra); trimmed != "" {
			// subtask 任务描述是唯一意图来源：完整注入，不做 180 截断。
			// 截断会吃掉授权范围（如文件列表），导致 Guard review 无法确认而保守 modify。
			// 预算由 addSection 兜底（subtask 场景其余证据为空，空余 1100+ runes）。
			// 窗口内同一条消息已被 recentGuardUserEvidence 截断到 180，
			// 用完整版替换截断版（前缀匹配），避免重复注入。
			replaced := false
			for i, existing := range users {
				if existing == trimmed || strings.HasPrefix(trimmed, existing) {
					users[i] = trimmed
					replaced = true
					break
				}
			}
			if !replaced {
				users = appendBoundedGuardEvidence(users, trimmed, guardEvidenceUserLimit)
			}
		}
	}
	builder := guardEvidenceBuilder{remaining: guardEvidenceBudget}
	if len(users) > 0 {
		builder.addSection("Latest direct user message", users[len(users)-1:])
	}
	builder.addSection("Resolved AskUser choices", newestFirstGuardEvidence(answers))
	builder.addSection("Recent resolved risk decisions", newestFirstGuardEvidence(riskDecisions))
	if len(users) > 1 {
		builder.addSection("Earlier recent user messages", newestFirstGuardEvidence(users[:len(users)-1]))
	}
	builder.addSection("Recent completed agent actions", newestFirstGuardEvidence(agentActions))
	if constraints := guardSessionConstraints(sessionState); constraints != "" {
		builder.addSection("Session continuity constraints (summarized background, not authorization)", []string{constraints})
	}
	if rationale = trimForGuardMiddle(rationale, guardEvidenceRationaleRunes); rationale != "" {
		builder.addSection("Agent execution rationale (not authorization)", []string{rationale})
	}
	return strings.Join(builder.sections, "\n\n")
}

func (b *guardEvidenceBuilder) addSection(title string, items []string) {
	if b.remaining <= 0 || len(items) == 0 {
		return
	}
	overhead := len([]rune(title)) + 2
	if len(b.sections) > 0 {
		overhead += 2
	}
	available := b.remaining - overhead
	if available <= 2 {
		return
	}
	lines := make([]string, 0, len(items))
	used := 0
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		line := "- " + item
		separator := 0
		if len(lines) > 0 {
			separator = 1
		}
		lineRunes := len([]rune(line))
		if lineRunes+separator > available-used {
			line = trimForGuardMiddle(line, available-used-separator)
			lineRunes = len([]rune(line))
		}
		if line == "" || lineRunes+separator > available-used {
			break
		}
		lines = append(lines, line)
		used += lineRunes + separator
	}
	if len(lines) > 0 {
		b.sections = append(b.sections, title+":\n"+strings.Join(lines, "\n"))
		b.remaining -= overhead + used
	}
}

func recentGuardUserEvidence(messages []model.Message) (users, answers []string) {
	if len(messages) > guardEvidenceMessageScan {
		messages = messages[len(messages)-guardEvidenceMessageScan:]
	}
	questions := make(map[string]string)
	for _, message := range messages {
		switch message.Role {
		case model.RoleAssistant:
			for _, call := range message.ToolCalls {
				if call.Name != agenttools.ToolAskUser {
					continue
				}
				params := model.ParseToolCallArguments(call.Arguments)
				if question, _ := params["question"].(string); strings.TrimSpace(question) != "" {
					questions[call.ID] = trimForGuardMiddle(question, guardEvidenceAskItemRunes)
				}
			}
		case model.RoleUser:
			if text := trimForGuardMiddle(message.Text(), guardEvidenceUserItemRunes); text != "" {
				users = appendBoundedGuardEvidence(users, text, guardEvidenceUserLimit)
			}
		case model.RoleTool:
			question := questions[message.ToolCallID]
			if question == "" {
				continue
			}
			var payload struct {
				Answer string `json:"answer"`
			}
			if json.Unmarshal([]byte(message.Text()), &payload) == nil {
				if answer := trimForGuardMiddle(payload.Answer, guardEvidenceAskItemRunes); answer != "" {
					answers = appendBoundedGuardEvidence(answers, "Question: "+question+"; Answer: "+answer, guardEvidenceAskLimit)
				}
			}
		}
	}
	return users, answers
}

func recentGuardActions(messages []model.Message, summary memory.ToolSummary) []string {
	if len(messages) > guardEvidenceMessageScan {
		messages = messages[len(messages)-guardEvidenceMessageScan:]
	}
	completed := make(map[string]bool)
	for _, message := range messages {
		if message.Role == model.RoleTool && strings.TrimSpace(message.ToolCallID) != "" {
			completed[message.ToolCallID] = true
		}
	}
	type action struct {
		name      string
		params    map[string]any
		status    string
		statusSet bool
	}
	var actions []action
	for _, message := range messages {
		if message.Role != model.RoleAssistant {
			continue
		}
		for _, call := range message.ToolCalls {
			if !completed[call.ID] || call.Name == agenttools.ToolAskUser {
				continue
			}
			actions = append(actions, action{name: call.Name, params: model.ParseToolCallArguments(call.Arguments)})
		}
	}
	if len(actions) > guardEvidenceActionLimit {
		actions = actions[len(actions)-guardEvidenceActionLimit:]
	}
	recent := summary.Normalize().Recent
	statusIndex := len(recent) - 1
	for actionIndex := len(actions) - 1; actionIndex >= 0; actionIndex-- {
		for statusIndex >= 0 {
			item := recent[statusIndex]
			statusIndex--
			if canonicalGuardToolName(item.Name) == canonicalGuardToolName(actions[actionIndex].name) {
				if strings.Contains(strings.ToLower(item.Status), "error") || strings.Contains(strings.ToLower(item.Status), "fail") {
					actions[actionIndex].status = "failed"
				} else {
					actions[actionIndex].status = "succeeded"
				}
				actions[actionIndex].statusSet = true
				break
			}
		}
	}
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		if !action.statusSet {
			continue
		}
		line := action.name + ": " + action.status
		if target := strings.TrimSpace(guard.SafeTarget(action.name, action.params)); target != "" {
			line += "; target=" + target
		}
		out = append(out, trimForGuardMiddle(line, guardEvidenceActionRunes))
	}
	return out
}

func canonicalGuardToolName(name string) string {
	name = strings.TrimSpace(name)
	if index := strings.LastIndex(name, "."); index >= 0 && index < len(name)-1 {
		return name[index+1:]
	}
	return name
}

func guardSessionConstraints(state string) string {
	var sections []string
	for _, title := range []string{"User requirements and decisions", "Active context"} {
		sections = append(sections, extractGuardStateSections(state, title)...)
	}
	return trimForGuardMiddle(strings.Join(sections, "\n"), guardEvidenceSessionRunes)
}

func extractGuardStateSections(state string, wanted ...string) []string {
	wantedSet := make(map[string]bool, len(wanted))
	for _, title := range wanted {
		wantedSet[title] = true
	}
	var out []string
	capture := false
	for _, line := range strings.Split(state, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			capture = wantedSet[strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))]
			continue
		}
		if capture && trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func newestFirstGuardEvidence(items []string) []string {
	out := make([]string, len(items))
	for index := range items {
		out[index] = items[len(items)-1-index]
	}
	return out
}

func appendBoundedGuardEvidence(items []string, item string, limit int) []string {
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	return append([]string(nil), items[len(items)-limit:]...)
}

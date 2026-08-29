package agent

import (
	"encoding/json"
	"strings"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools/agenttools"
)

const (
	guardEvidenceBudget         = 2048
	guardEvidenceMessageScan    = 64
	guardEvidenceUserLimit      = 3
	guardEvidenceAskLimit       = 2
	guardEvidenceUserItemRunes  = 180
	guardEvidenceAskItemRunes   = 220
	guardEvidenceRationaleRunes = 360
)

type guardEvidenceBuilder struct {
	remaining int
	sections  []string
}

// buildGuardEvidence 构建 smart review 的轻量证据：只保留用户意图来源。
// 审风险不审意图：意图对齐归 Agent（有完整上下文），Guard 只判断操作本身是否危险，
// 因此 Earlier users / Session constraints / Agent actions / risk decisions 全部移除，
// 避免保守模型因上下文不足而频繁误判。
func buildGuardEvidence(messages []model.Message, rationale string, extraUsers ...string) string {
	users, answers := recentGuardUserEvidence(messages)
	// subtask 场景：任务描述作为注入的用户意图（不受 64 条扫描窗口限制）。
	// subtask 的任务描述是第一条也是唯一一条 user 消息，长任务（>64 条）时会被窗口挤出。
	for _, extra := range extraUsers {
		if trimmed := strings.TrimSpace(extra); trimmed != "" {
			// subtask 任务描述是唯一意图来源：完整注入，不做 180 截断。
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

// trimForGuardMiddle 中间截断长文本，保留首尾（脱敏后）。
func trimForGuardMiddle(s string, max int) string {
	s = strings.TrimSpace(guard.MaskSensitiveContent(s))
	chars := []rune(s)
	if max <= 0 || len(chars) <= max {
		return s
	}
	if max <= 32 {
		return string(chars[:max])
	}
	first := (max - 18) * 2 / 3
	last := max - 18 - first
	return string(chars[:first]) + "...[omitted]..." + string(chars[len(chars)-last:])
}

package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
)

type CompactOutput struct {
	Observations []ToolObservation `json:"observations,omitempty"`
	Summary      string            `json:"summary,omitempty"`
}
type ToolObservation struct {
	ID   int    `json:"id"`
	Keep bool   `json:"keep,omitempty"`
	Text string `json:"text,omitempty"`
}

type CompactApplied struct {
	Messages      []Message
	Summary       string
	Kind          CompactKind
	RetainedTools []string
}

// SourceKey 不包含位置和消费标记；追加后缀或历史前移不能让同一组重新获得付费资格。
func (g ToolGroup) SourceKey() string { return compactSourceKey("", g.Messages) }
func (p *CompactPlan) SourceKey() string {
	h := sha256.New()
	encoder := json.NewEncoder(h)
	_ = encoder.Encode(p.Summary)
	write := func(messages []Message) {
		_ = encoder.Encode(len(messages))
		for _, m := range messages {
			m = ProjectMessage(m)
			m.Consumed = false
			_ = encoder.Encode(m)
		}
	}
	write(p.Prefix)
	for _, group := range p.Groups {
		write(group.Messages)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func compactSourceKey(summary string, messages []Message) string {
	return (&CompactPlan{Summary: summary, Prefix: messages}).SourceKey()
}

// normalizeToolObservations 只修复结构，不猜模型意图。重复条目有歧义，整组保留原文。
func normalizeToolObservations(groups []ToolGroup, out CompactOutput) CompactOutput {
	planned := make(map[int]bool, len(groups))
	for _, group := range groups {
		planned[group.Index] = true
	}
	values := make(map[int]ToolObservation, len(groups))
	unknown, duplicate, empty := 0, 0, 0
	for _, v := range out.Observations {
		if !planned[v.ID] {
			unknown++
			continue
		}
		if _, seen := values[v.ID]; seen {
			duplicate++
			values[v.ID] = ToolObservation{ID: v.ID, Keep: true}
			continue
		}
		if !v.Keep && strings.TrimSpace(v.Text) == "" {
			empty++
			v.Keep, v.Text = true, ""
		}
		values[v.ID] = v
	}
	normalized := make([]ToolObservation, 0, len(groups))
	missing := 0
	for _, group := range groups {
		v, ok := values[group.Index]
		if !ok {
			missing++
			v = ToolObservation{ID: group.Index, Keep: true}
		}
		normalized = append(normalized, v)
	}
	if missing+unknown+duplicate+empty > 0 {
		logging.Info("memory", "compact_repair", logging.Event{"groups": len(groups), "missing": missing, "unknown": unknown, "duplicate": duplicate, "empty": empty})
	}
	out.Observations = normalized
	return out
}

// Apply 在副本上同时替换头部和不重叠工具。先核验全部来源，任何来源错误都不部分提交。
// 新增后缀与消费标记变化不使候选失效；真正复制的是当前消息，而非模型输入的文本投影。
func (p *CompactPlan) Apply(messages []Message, summary string, out CompactOutput) (CompactApplied, error) {
	var applied CompactApplied
	head := len(p.Prefix)
	if head > len(messages) || (head > 0 && (p.Summary != summary || !sameSource(messages[:head], p.Prefix))) {
		return applied, fmt.Errorf("compression source changed")
	}
	end := head
	for _, group := range p.Groups {
		if group.Index < end || len(group.Messages) == 0 || group.Index > len(messages)-len(group.Messages) || !sameSource(messages[group.Index:group.Index+len(group.Messages)], group.Messages) {
			return applied, fmt.Errorf("compression source changed")
		}
		end = group.Index + len(group.Messages)
	}
	out = normalizeToolObservations(p.Groups, out)
	// 混合输出缺少有效历史摘要时保留整个头部，只采用独立的有效工具观察。
	// 绝不把部分或空摘要写入历史，也不宣称计划中的所有部分均已采用。
	history := head > 0 && strings.TrimSpace(out.Summary) != "" &&
		model.EstimateTokens(out.Summary) < EstimateMessagesTokens(p.Prefix)+model.EstimateTokens(summary)
	start := 0
	applied.Summary = summary
	if history {
		start = head
		applied.Summary = out.Summary
	}
	applied.Messages = make([]Message, 0, len(messages)-start)
	tools := false
	for i, group := range p.Groups {
		end := group.Index + len(group.Messages)
		applied.Messages = append(applied.Messages, messages[start:group.Index]...)
		observation := out.Observations[i]
		original := messages[group.Index]
		var replacement Message
		if !observation.Keep {
			replacement = compactToolReplacement(original, group.Messages, observation.Text)
		}
		// 模型可能返回比原始结果更长的观察；该组必须原样保留，而不是抵消其他组收益。
		if observation.Keep || EstimateMessagesTokens([]Message{replacement}) >= EstimateMessagesTokens(messages[group.Index:end]) {
			applied.Messages = append(applied.Messages, messages[group.Index:end]...)
			applied.RetainedTools = append(applied.RetainedTools, group.SourceKey())
		} else {
			applied.Messages = append(applied.Messages, replacement)
			tools = true
		}
		start = end
	}
	applied.Messages = append(applied.Messages, messages[start:]...)
	applied.Kind = compactKind(history, tools)
	return applied, nil
}

func compactToolReplacement(original Message, source []Message, text string) Message {
	original.TextContent = source[0].Text()
	original.Content = []model.ContentBlock{{Type: model.ContentText, Text: original.TextContent}}
	statuses := make(map[string]string, len(source)-1)
	for _, result := range source[1:] {
		statuses[result.ToolCallID] = result.ToolStatus
	}
	labels := make([]ToolLabel, len(original.ToolCalls))
	for i, call := range original.ToolCalls {
		labels[i] = ToolLabel{Name: call.Name, Intent: call.Intent, Status: statuses[call.ID]}
	}
	original.Observation = &Observation{Text: text, Calls: labels}
	original.ToolCalls = nil
	return original
}

func sameSource(a, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := ProjectMessage(a[i]), b[i]
		x.Consumed, y.Consumed = false, false
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}

// PruneToolSourceKeys 只保留仍存在的原生工具组，避免会话寿命决定抑制集合大小。
// 不依赖消费标记和绝对位置，历史前移不影响同源识别；无抑制记录时不扫描正文。
func PruneToolSourceKeys(messages []Message, keys map[string]bool) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	var kept map[string]bool
	for i := 0; i < len(messages); i++ {
		if messages[i].Role != model.RoleAssistant || len(messages[i].ToolCalls) == 0 {
			continue
		}
		end := i + 1
		for end < len(messages) && messages[end].Role == model.RoleTool {
			end++
		}
		key := (ToolGroup{Messages: messages[i:end]}).SourceKey()
		if keys[key] {
			if kept == nil {
				kept = make(map[string]bool)
			}
			kept[key] = true
			if len(kept) == len(keys) {
				break
			}
		}
		i = end - 1
	}
	return kept
}

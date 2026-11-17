package memory

import (
	"encoding/json"
	"fmt"

	"github.com/alanchenchen/suna/internal/model"
)

type CompactKind string

const (
	CompactTools   CompactKind = "tools"
	CompactHistory CompactKind = "history"
	CompactMixed   CompactKind = "mixed"
)

const (
	compactTailPercent       = 10
	compactPromptReserve     = 2048
	compactOutputDivisor     = 3
	compactMinimumToolSaving = 128
)

// CompactOptions 的目标与收益均使用主请求的校准尺度；输入限制属于摘要请求自身。
// Planner 不选择模型、不决定何时采用，也不为失败来源自动发起新请求。
type CompactOptions struct {
	Budget, TargetGain, Tolerance, MinimumGain int
	InputLimit, OutputLimit                    int
	Coefficient                                float64
	ExcludedTools                              map[string]bool
}

type ToolGroup struct {
	Index    int
	Messages []Message
}

type CompactPlan struct {
	Kind         CompactKind
	Summary      string
	Groups       []ToolGroup
	Prefix       []Message
	Background   []Message
	SourceTokens int
	OutputBudget int
	ExpectedGain int

	inputTokens int
	coefficient float64
	inputLimit  int
}

type compactToolSource struct {
	start, end int
	saving     int
	before     int
	after      int
}

// compactSelection 增量维护来源和背景成本，选头部时只向前移动，避免反复序列化整段历史。
type compactSelection struct {
	messages                    []Message
	costs, raw, prefixCost      []int
	tools                       []compactToolSource
	refs                        []int
	selected                    []compactToolSource
	head, removedTools          int
	source, input, background   int
	summaryTokens, summaryInput int
	jsonCost                    compactJSONEstimator
	options                     CompactOptions
}

// PlanCompact 先选足够的旧工具；不足时才扩选最短头部，重叠工具直接并入头部。
// 最终只构造一个不重叠计划；模型不参与选区和原话去留决策。
func PlanCompact(messages []Message, summary string, options CompactOptions) (*CompactPlan, error) {
	if options.Budget <= 0 || options.InputLimit <= 0 || options.OutputLimit < 32 || options.TargetGain <= 0 {
		return nil, fmt.Errorf("invalid compression budget")
	}
	if options.Coefficient <= 0 {
		options.Coefficient = 1
	}
	projected := ProjectMessages(messages)
	s := compactSelection{messages: projected, options: options, costs: make([]int, len(projected)), raw: make([]int, len(projected)), prefixCost: make([]int, len(projected)+1), refs: make([]int, len(projected))}
	for i := range projected {
		s.costs[i] = EstimateMessagesTokens(projected[i : i+1])
		s.prefixCost[i+1] = s.prefixCost[i] + s.costs[i]
		s.raw[i] = -1
	}
	tail := compactTail(projected, s.costs, options)
	s.tools = s.toolSources(tail)
	s.summaryInput = s.jsonCost.tokens(summary) + 2
	s.summaryTokens = model.EstimateTokens(summary)
	bestHead, bestCount, bestGain := -1, 0, 0
	// 无论后续是否需要历史，工具都先按稳定顺序选择；达到软目标即停止扩选。
	for _, source := range s.tools {
		s.addTool(source, 1)
		s.selected = append(s.selected, source)
		if !s.fits() {
			s.selected = s.selected[:len(s.selected)-1]
			s.addTool(source, -1)
			continue
		}
		if s.enough() {
			return s.plan(summary), nil
		}
	}
	if s.gain() >= max(1, options.MinimumGain) && len(s.selected) > 0 {
		bestHead, bestCount, bestGain = 0, len(s.selected), s.gain()
	}

	// 历史只能覆盖从零开始的连续已消费来源，并在完整助手响应或闭合工具组后结束。
	for end := 0; end < tail; {
		m := projected[end]
		if !m.Consumed || m.Role == model.RoleTool {
			break
		}
		next := end + 1
		if len(m.ToolCalls) > 0 {
			var ok bool
			next, ok = closedGroup(projected, end)
			if !ok || next > tail {
				break
			}
		}
		s.extendHead(next)
		end = next
		if m.Role != model.RoleAssistant {
			continue
		}
		// 为必要头部让出末尾独立工具的容量；只裁减计划，不修改原始历史。
		for !s.fits() && len(s.selected) > s.removedTools {
			last := s.selected[len(s.selected)-1]
			s.addTool(last, -1)
			s.selected = s.selected[:len(s.selected)-1]
		}
		if !s.fits() {
			break
		}
		if gain := s.gain(); gain >= max(1, options.MinimumGain) && gain > bestGain {
			bestHead, bestCount, bestGain = s.head, len(s.selected), gain
		}
		if s.enough() {
			return s.plan(summary), nil
		}
	}
	if bestHead < 0 {
		return nil, nil
	}
	// 扫描只记录边界，最后构造一次不可变来源，避免逐边界复制长历史。
	chosen := s.selected[:bestCount]
	s.selected = chosen
	s.head, s.removedTools, s.source, s.input, s.background = 0, 0, 0, 0, 0
	clear(s.refs)
	for _, tool := range chosen {
		s.addTool(tool, 1)
	}
	s.extendHead(bestHead)
	return s.plan(summary), nil
}

// compactTail 至少保护一个近期完整单元；大消息跨过保护线时整条留下，不切正文。
func compactTail(messages []Message, costs []int, q CompactOptions) int {
	tail, tokens := len(messages), 0
	for tail > 0 && (tokens == 0 || model.ApplyCoefficient(tokens, q.Coefficient) < q.Budget*compactTailPercent/100) {
		tail--
		tokens += costs[tail]
	}
	return tail
}

func (s *compactSelection) toolSources(tail int) []compactToolSource {
	nextAssistant := make([]int, len(s.messages)+1)
	nextAssistant[len(s.messages)] = -1
	for i := len(s.messages) - 1; i >= 0; i-- {
		nextAssistant[i] = nextAssistant[i+1]
		m := s.messages[i]
		if IsUserMessage(m) {
			// 下一轮用户要求之后的答复不属于前一工具组的关联解释。
			nextAssistant[i] = -1
		}
		if m.Role == model.RoleAssistant && m.Consumed && len(m.ToolCalls) == 0 && (m.Text() != "" || m.Observation != nil) {
			nextAssistant[i] = i
		}
	}
	var out []compactToolSource
	lastUser := -1
	for i := 0; i < tail; {
		m := s.messages[i]
		if IsUserMessage(m) {
			lastUser = i
		}
		if len(m.ToolCalls) == 0 {
			i++
			continue
		}
		end, ok := closedGroup(s.messages, i)
		if !ok || end > tail {
			i = max(i+1, end)
			continue
		}
		prose := model.NewTextMessage(model.RoleAssistant, m.Text())
		saving := s.prefixCost[end] - s.prefixCost[i] - model.EstimateMessagesTokens([]model.Message{prose})
		if model.ApplyCoefficient(saving-max(96, saving/compactOutputDivisor+96), s.options.Coefficient) >= compactMinimumToolSaving {
			group := ToolGroup{Index: i, Messages: s.messages[i:end]}
			if len(s.options.ExcludedTools) == 0 || !s.options.ExcludedTools[group.SourceKey()] {
				out = append(out, compactToolSource{start: i, end: end, saving: saving, before: lastUser, after: nextAssistant[end]})
			}
		}
		i = end
	}
	return out
}

// rawCost 只序列化实际入选来源及必要背景，不为未发送的长尾构造 JSON 副本。
func (s *compactSelection) rawCost(index int) int {
	if s.raw[index] < 0 {
		s.raw[index] = s.jsonCost.tokens(compactSource(s.messages[index])) + 2
	}
	return s.raw[index]
}

// compactJSONEstimator 直接统计编码器写出的 JSON，避免为估算复制完整字节和字符串。
// 单次规划复用编码器，没有跨请求缓存；有效 UTF-8 的非 ASCII 首字节恰好对应一个字符。
// Encoder 比 Marshal 多一个换行，计数时扣除，保持与既有 EstimateTokens 完全同尺度。
type compactJSONEstimator struct {
	encoder         *json.Encoder
	ascii, nonASCII int
}

func (c *compactJSONEstimator) Write(data []byte) (int, error) {
	for _, b := range data {
		if b < 0x80 {
			c.ascii++
		} else if b&0xc0 != 0x80 {
			c.nonASCII++
		}
	}
	return len(data), nil
}

func (c *compactJSONEstimator) tokens(value any) int {
	c.ascii, c.nonASCII = 0, 0
	if c.encoder == nil {
		c.encoder = json.NewEncoder(c)
	}
	// 来源只包含固定的历史字段，没有自定义编码或不可编码的动态值。
	_ = c.encoder.Encode(value)
	return max(0, c.ascii-1)/4 + c.nonASCII + 1
}

func (s *compactSelection) reference(index, delta int) {
	if index < 0 || index < s.head {
		return
	}
	if s.refs[index] == 0 && delta > 0 {
		s.background += s.rawCost(index)
	}
	s.refs[index] += delta
	if s.refs[index] == 0 {
		s.background -= s.rawCost(index)
	}
}

func (s *compactSelection) addTool(tool compactToolSource, delta int) {
	s.source += delta * tool.saving
	for i := tool.start; i < tool.end; i++ {
		s.input += delta * s.rawCost(i)
	}
	s.input += delta * 32
	s.reference(tool.before, delta)
	s.reference(tool.after, delta)
}

func (s *compactSelection) extendHead(end int) {
	// 先移出重叠工具及其背景，再将同一来源计入头部，不能重复收费或计算净收益。
	for s.removedTools < len(s.selected) && s.selected[s.removedTools].start < end {
		s.addTool(s.selected[s.removedTools], -1)
		s.removedTools++
	}
	for i := s.head; i < end; i++ {
		if s.refs[i] > 0 {
			s.background -= s.rawCost(i)
			s.refs[i] = 0
		}
		s.input += s.rawCost(i)
	}
	s.head = end
}

func (s *compactSelection) metrics() (source, output, input int) {
	source = s.source
	input = compactPromptReserve + s.input + s.background
	groups := len(s.selected) - s.removedTools
	if s.head > 0 {
		source += s.prefixCost[s.head] + s.summaryTokens
		input += s.summaryInput
	}
	output = min(s.options.OutputLimit, max(1024, source/compactOutputDivisor+groups*96))
	return
}
func (s *compactSelection) gain() int {
	source, output, _ := s.metrics()
	return model.ApplyCoefficient(source-output, s.options.Coefficient)
}
func (s *compactSelection) enough() bool {
	return s.gain() >= max(s.options.MinimumGain, s.options.TargetGain-s.options.Tolerance)
}
func (s *compactSelection) fits() bool {
	_, output, input := s.metrics()
	minimum := 32 + (len(s.selected)-s.removedTools)*16
	return s.options.OutputLimit >= minimum && model.ApplyCoefficient(input, s.options.Coefficient)+output <= s.options.InputLimit
}
func (s *compactSelection) plan(summary string) *CompactPlan {
	source, output, input := s.metrics()
	p := &CompactPlan{SourceTokens: source, OutputBudget: output, ExpectedGain: s.gain(), inputTokens: input, coefficient: s.options.Coefficient, inputLimit: s.options.InputLimit}
	if s.head > 0 {
		p.Prefix = append([]Message(nil), s.messages[:s.head]...)
		p.Summary = summary
	}
	for _, tool := range s.selected[s.removedTools:] {
		p.Groups = append(p.Groups, ToolGroup{Index: tool.start, Messages: append([]Message(nil), s.messages[tool.start:tool.end]...)})
	}
	for i, refs := range s.refs {
		if refs > 0 && i >= s.head {
			p.Background = append(p.Background, plainReference(s.messages[i]))
		}
	}
	p.Kind = compactKind(len(p.Prefix) > 0, len(p.Groups) > 0)
	return p
}

func compactKind(history, tools bool) CompactKind {
	switch {
	case history && tools:
		return CompactMixed
	case history:
		return CompactHistory
	case tools:
		return CompactTools
	default:
		return ""
	}
}

func plainReference(m Message) Message {
	out := NewTextMessage(m.Role, m.ModelMessage().Text())
	out.Origin = m.Origin
	return out
}

func closedGroup(messages []Message, index int) (int, bool) {
	calls := messages[index].ToolCalls
	if messages[index].Role != model.RoleAssistant || len(calls) == 0 {
		return index + 1, false
	}
	consumed := messages[index].Consumed
	seen := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.ID == "" || seen[call.ID] {
			return index + 1, false
		}
		seen[call.ID] = true
	}
	end := index + 1
	for end < len(messages) && messages[end].Role == model.RoleTool {
		id := messages[end].ToolCallID
		if !seen[id] {
			return end + 1, false
		}
		delete(seen, id)
		consumed = consumed && messages[end].Consumed
		end++
	}
	return end, len(seen) == 0 && consumed
}

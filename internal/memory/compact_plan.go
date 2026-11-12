package memory

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
)

type CompactKind string

const (
	CompactTools   CompactKind = "tools"
	CompactHistory CompactKind = "history"
)

// 来源筛选、近期保护和历史释放目标属于选区策略，不与 Runner 的采用门槛混用。
const (
	compactToolTailPercent           = 10 // 工具模式保护的近期完整历史。
	compactToolMinimumSourcePercent  = 8  // 工具可替换来源的普通准入下限。
	compactHistoryTargetGainPercent  = 15 // 历史扩选争取的释放量，非采用硬门槛。
	compactHistoryMinimumGainPercent = 8  // 历史候选的估算净收益下限。
	compactOutputReserveDivisor      = 3  // 期望输出按来源的三分之一预留。
)

// ToolGroup 是内存里的压缩候选单元，不直接序列化给模型；
// 发给模型时经 compactToolGroups 转成稠密编号的 toolGroupWire。
type ToolGroup struct {
	Index    int
	Messages []Message
}

// toolGroupWire 是发给模型的工具组投影。Index 是消息位置，编号稀疏跳跃，
// 模型容易自行重编或漏写；发给模型时改用稠密的 0..N-1，回来再还原。
type toolGroupWire struct {
	ID       int       `json:"id"`
	Messages []Message `json:"messages"`
}

// compactToolGroups 把计划组投影成稠密编号，模型只需按顺序数数。
func compactToolGroups(groups []ToolGroup) []toolGroupWire {
	out := make([]toolGroupWire, len(groups))
	for i, group := range groups {
		out[i] = toolGroupWire{ID: i, Messages: group.Messages}
	}
	return out
}

// restoreToolGroupIndexes 把模型返回的稠密编号还原成真实消息位置。
// 越界编号统一改成 -1，绝不能原样透传：真实位置可能恰好等于某个越界稠密编号，
// 那会把模型给的证据套到另一组上，静默污染历史。
func restoreToolGroupIndexes(groups []ToolGroup, observations []ToolObservation) []ToolObservation {
	out := make([]ToolObservation, len(observations))
	for i, v := range observations {
		if v.ID >= 0 && v.ID < len(groups) {
			v.ID = groups[v.ID].Index
		} else {
			v.ID = -1
		}
		out[i] = v
	}
	return out
}

type CompactPlan struct {
	Kind                CompactKind
	Summary             string
	Groups              []ToolGroup
	Prefix              []Message
	Background          []Message
	HistoryStart        int
	SourceTokens        int
	OutputBudget        int
	inputTokens         int
	coefficient         float64
	inputLimit          int
	outputMinimum       int
	summaryTokens       int
	backgroundPositions []int
}

// PlanCompact 只选择完整、已消费来源；任何真实输入容量限制都通过缩小完整批次满足。
func PlanCompact(messages []Message, summary string, kind CompactKind, budget, inputLimit, outputLimit int, coefficient float64, force bool, sourceStart ...int) (*CompactPlan, error) {
	start := 0
	if len(sourceStart) > 0 {
		start = sourceStart[0]
	}
	if start < 0 || start > len(messages) {
		return nil, fmt.Errorf("invalid compression source start")
	}
	projected := ProjectMessages(messages)
	if coefficient <= 0 {
		coefficient = 1
	}
	encodedSummary, _ := json.Marshal(summary)
	baseInput := model.EstimateTokens(string(encodedSummary)) + 2048
	p := &CompactPlan{Kind: kind, Summary: summary, inputTokens: baseInput, coefficient: coefficient, inputLimit: inputLimit, summaryTokens: model.EstimateTokens(summary)}
	costs := make([]int, len(projected))
	rawCosts := make([]int, len(projected))
	for i, m := range projected {
		costs[i] = EstimateMessagesTokens(projected[i : i+1])
		data, _ := json.Marshal(compactSource(m))
		rawCosts[i] = model.EstimateTokens(string(data)) + 2
	}
	var selected bool
	if kind == CompactTools {
		selected = p.planToolGroups(projected, costs, rawCosts, budget, outputLimit, start, force)
	} else {
		selected = p.planHistory(projected, costs, rawCosts, budget, outputLimit, force)
	}
	if !selected {
		return nil, nil
	}
	p.backgroundPositions = nil
	if !planFits(p, p.inputLimit, outputLimit) {
		return nil, fmt.Errorf("complete compression source exceeds request capacity")
	}
	return p, nil
}

// planToolGroups 从旧到新选完整工具组，并按消息位置去重只读背景。
func (p *CompactPlan) planToolGroups(projected []Message, costs, rawCosts []int, budget, outputLimit, start int, force bool) bool {
	backgroundSeen := make(map[int]bool)
	addBackground := func(index int) {
		if index >= 0 && !backgroundSeen[index] {
			backgroundSeen[index] = true
			p.Background = append(p.Background, plainReference(projected[index]))
			p.backgroundPositions = append(p.backgroundPositions, index)
			encoded, _ := json.Marshal(p.Background[len(p.Background)-1])
			p.inputTokens += model.EstimateTokens(string(encoded)) + 2
		}
	}

	addBackground(latestUserIndex(projected))
	tail := len(projected)
	tokens := 0
	for tail > 0 && model.ApplyCoefficient(tokens, p.coefficient) < budget*compactToolTailPercent/100 {
		tail--
		tokens += costs[tail]
	}
	nextAssistant := make([]int, len(projected)+1)
	nextAssistant[len(projected)] = -1
	for j := len(projected) - 1; j >= 0; j-- {
		nextAssistant[j] = nextAssistant[j+1]
		// 新的工具声明不是后续完整解释，也不能把尚未消费的半成品当背景。
		if projected[j].Role == model.RoleAssistant && projected[j].Consumed && len(projected[j].ToolCalls) == 0 && (projected[j].Text() != "" || projected[j].Observation != nil) {
			nextAssistant[j] = j
		}
	}
	lastUser := -1
	for i := 0; i < start; i++ {
		if IsUserMessage(projected[i]) {
			lastUser = i
		}
	}
	for i := start; i < tail; {
		if IsUserMessage(projected[i]) {
			lastUser = i
		}
		m := projected[i]
		if len(m.ToolCalls) == 0 {
			i++
			continue
		}
		end, ok := closedGroup(projected, i)
		if !ok || end > tail {
			i = max(i+1, end)
			continue
		}
		group := ToolGroup{Index: i, Messages: append([]Message(nil), projected[i:end]...)}
		added := -model.EstimateTokens(m.Text())
		for j := i; j < end; j++ {
			added += costs[j]
		}
		p.Groups = append(p.Groups, group)
		p.SourceTokens += added
		oldBackground, oldInput, oldOutput := len(p.Background), p.inputTokens, p.OutputBudget
		// 背景只取真实 user 和无 ToolCalls 的完整解释，天然不与原生组来源重叠；
		// 再按来源位置去重后才计量，不能在 fit 后删除重复内容却仍重复收费。
		addBackground(lastUser)
		if end < len(projected) {
			addBackground(nextAssistant[end])
		}
		for j := i; j < end; j++ {
			p.inputTokens += rawCosts[j]
		}
		p.inputTokens += 32
		if !planFits(p, p.inputLimit, outputLimit) {
			p.Groups = p.Groups[:len(p.Groups)-1]
			p.SourceTokens -= added
			for _, position := range p.backgroundPositions[oldBackground:] {
				delete(backgroundSeen, position)
			}
			p.Background = p.Background[:oldBackground]
			p.backgroundPositions = p.backgroundPositions[:oldBackground]
			p.inputTokens = oldInput
			p.OutputBudget = oldOutput
			break
		}

		i = end
	}
	return len(p.Groups) > 0 && (force || model.ApplyCoefficient(p.SourceTokens, p.coefficient) >= budget*compactToolMinimumSourcePercent/100)
}

// planHistory 沿完整交流边界扩选连续历史，估算收益时扣除完整原话保留成本。
func (p *CompactPlan) planHistory(projected []Message, costs, rawCosts []int, budget, outputLimit int, force bool) bool {
	baseInput := p.inputTokens
	latest := latestUserIndex(projected)
	backgroundInput := 0
	if latest >= 0 {
		p.Background = []Message{plainReference(projected[latest])}
		encoded, _ := json.Marshal(p.Background[0])
		backgroundInput = model.EstimateTokens(string(encoded)) + 2
	}
	// 必须保留未消费材料；旧来源未知的 user 整条保留，不充真实原话配额。
	retained := retainedOriginalCosts(projected)
	end, source, input := 0, 0, baseInput
	validEnd, validSource, validInput, validOutput, validMinimum := 0, 0, 0, 0, 0
	keepBytes := []int{0}
	for end < len(projected) {
		m := projected[end]
		if !m.Consumed {
			break
		}
		next := end + 1
		if len(m.ToolCalls) > 0 {
			var ok bool
			next, ok = closedGroup(projected, end)
			if !ok {
				break
			}
		}
		for j := end; j < next; j++ {
			source += costs[j]
			input += rawCosts[j]
			if IsUserMessage(projected[j]) && projected[j].Origin != OriginLegacy {
				keepBytes = append(keepBytes, keepBytes[len(keepBytes)-1]+len(strconv.Itoa(j))+1)
			}
		}
		// 完整助手消息或已消费闭合组都是交流边界；不要求等到下一个 user，
		// 否则一条要求下持续 coding 的安全历史会被整段冻结。单独 user 不选。
		end = next
		if m.Role != model.RoleAssistant {
			continue
		}
		p.SourceTokens = source
		p.inputTokens = input
		if latest >= end {
			p.inputTokens += backgroundInput
		}
		// 至少半数完整原话的编号必须能输出，不能付费后才发现 JSON 本身装不下。
		p.outputMinimum = 32 + keepBytes[len(keepBytes)/2]/4
		if !planFits(p, p.inputLimit, outputLimit) {
			break
		}
		validEnd, validSource = end, source
		validInput, validOutput, validMinimum = p.inputTokens, p.OutputBudget, p.outputMinimum
		if model.ApplyCoefficient(source+p.summaryTokens-retained[end], p.coefficient)-p.OutputBudget >= budget*compactHistoryTargetGainPercent/100 {
			break
		}
	}
	if validEnd == 0 {
		return false
	}
	p.Prefix = append([]Message(nil), projected[:validEnd]...)
	p.SourceTokens, p.inputTokens, p.OutputBudget = validSource, validInput, validOutput
	p.outputMinimum = validMinimum
	if latest >= 0 && latest < validEnd {
		// fit 已按来源只计一次；背景与其成本一起退出，而非提交前才去重。
		p.Background = nil
	}
	return force || model.ApplyCoefficient(p.SourceTokens+p.summaryTokens-retained[validEnd], p.coefficient)-p.OutputBudget >= budget*compactHistoryMinimumGainPercent/100
}

func latestUserIndex(messages []Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if IsUserMessage(messages[i]) {
			return i
		}
	}
	return -1
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

// retainedOriginalCosts 线性估算完整原话的可行保留成本。按到达次序交替分两组，
// 文本较重的一组满足半数 token；若它少一条，只补另一组最短的一条。
// 这不是语义选择或最优背包，只用来避免把一条完整长原话错误估成半条；
// Apply 仍对模型实际选择核验条数与文本双配额，采用处再计真实净收益。
func retainedOriginalCosts(messages []Message) []int {
	out := make([]int, len(messages)+1)
	var counts, sums, shortest [2]int
	known, legacy, total, longest := 0, 0, 0, 0
	for i, m := range messages {
		if IsUserMessage(m) {
			n := model.EstimateTokens(m.Text())
			if m.Origin == OriginLegacy {
				legacy += n
			} else {
				group := known % 2
				known++
				total += n
				longest = max(longest, n)
				counts[group]++
				sums[group] += n
				if counts[group] == 1 || n < shortest[group] {
					shortest[group] = n
				}
			}
		}
		group := 0
		if sums[1] > sums[0] {
			group = 1
		}
		retained := sums[group]
		if counts[group]*2 < known {
			retained += shortest[1-group]
		}
		// 极长单条以外的原话若已满足双配额，可避免离群值迫使扩大不必要的来源。
		if known > 1 && (total-longest)*2 >= total {
			retained = min(retained, total-longest)
		}
		out[i+1] = legacy + retained
	}
	return out
}

func planFits(p *CompactPlan, inputLimit, outputLimit int) bool {
	// 3:1 仅用来预留输出空间，不是来源大小的物理上限。真实 binding 较小
	// 时封顶，较大时随来源增长；只有完整请求容量或逐组结构下限才会缩批。
	want := max(1024, (p.SourceTokens+p.summaryTokens)/compactOutputReserveDivisor)
	minimum := max(32, p.outputMinimum)
	if p.Kind == CompactTools {
		want = max(1024, p.SourceTokens/compactOutputReserveDivisor+len(p.Groups)*96)
		// 即便全 keep，也须放得下每组 id/keep/text 及 JSON 外壳；16 token
		// 每项覆盖整数 id 的结构空间，96 是期望观察空间而非硬切批上限。
		minimum = 32 + len(p.Groups)*16
	}
	if outputLimit < minimum {
		return false
	}
	p.OutputBudget = min(want, outputLimit)
	return model.ApplyCoefficient(p.inputTokens, p.coefficient) <= inputLimit-p.OutputBudget
}

type CompactOutput struct {
	Observations []ToolObservation `json:"observations"`
	Summary      string            `json:"summary"`
	Keep         []int             `json:"keep"`
}
type ToolObservation struct {
	ID   int    `json:"id"`
	Keep bool   `json:"keep"`
	Text string `json:"text"`
}

// normalizeToolObservations 把模型输出对齐到计划组集合。
// 模型漏写、重复、给出计划外编号或声明可压缩却没给证据时，都退化为保留原文，
// 而不是让整批压缩失败；只有来源本身改变才属于不可恢复的错误。
func normalizeToolObservations(groups []ToolGroup, out CompactOutput) CompactOutput {
	planned := make(map[int]bool, len(groups))
	for _, group := range groups {
		planned[group.Index] = true
	}
	seen := make(map[int]bool, len(out.Observations))
	normalized := make([]ToolObservation, 0, len(groups))
	unknown, duplicate, empty := 0, 0, 0
	for _, v := range out.Observations {
		if !planned[v.ID] {
			unknown++
			continue
		}
		if seen[v.ID] {
			duplicate++
			continue
		}
		seen[v.ID] = true
		// 声明可压缩却没有证据时只能保留原文，不能写入空观察。
		if !v.Keep && strings.TrimSpace(v.Text) == "" {
			empty++
			v.Keep, v.Text = true, ""
		}
		normalized = append(normalized, v)
	}
	missing := 0
	for _, group := range groups {
		if seen[group.Index] {
			continue
		}
		missing++
		normalized = append(normalized, ToolObservation{ID: group.Index, Keep: true})
	}
	if missing+unknown+duplicate+empty > 0 {
		logging.Info("memory", "compact_repair", logging.Event{"groups": len(groups), "missing": missing, "unknown": unknown, "duplicate": duplicate, "empty": empty})
	}
	out.Observations = normalized
	return out
}

// Apply 在当前历史上精准替换，新增后缀不属于候选来源。Consumed变化不使内容来源失效。
func (p *CompactPlan) Apply(messages []Message, summary string, out CompactOutput) ([]Message, string, error) {
	if p.Kind == CompactTools {
		out = normalizeToolObservations(p.Groups, out)
		observations := make(map[int]ToolObservation, len(out.Observations))
		for _, v := range out.Observations {
			observations[v.ID] = v
		}
		result := make([]Message, 0, len(messages))
		start := 0
		for _, group := range p.Groups {
			end := group.Index + len(group.Messages)
			if end > len(messages) || !sameSource(messages[group.Index:end], group.Messages) {
				return nil, "", fmt.Errorf("compression source changed")
			}
			observation := observations[group.Index]
			result = append(result, messages[start:group.Index]...)
			if observation.Keep {
				result = append(result, messages[group.Index:end]...)
			} else {
				original := messages[group.Index]
				// 原正文与观察分别持久化；请求投影再按既有文本语义拼接。
				original.TextContent = group.Messages[0].Text()
				original.Content = []model.ContentBlock{{Type: model.ContentText, Text: original.TextContent}}
				statuses := make(map[string]string, len(group.Messages)-1)
				for _, result := range group.Messages[1:] {
					statuses[result.ToolCallID] = result.ToolStatus
				}
				labels := make([]ToolLabel, len(original.ToolCalls))
				for i, call := range original.ToolCalls {
					labels[i] = ToolLabel{Name: call.Name, Intent: call.Intent, Status: statuses[call.ID]}
				}
				original.Observation = &Observation{Text: observation.Text, Calls: labels}
				original.ToolCalls = nil
				result = append(result, original)
			}
			start = end
		}
		return append(result, messages[start:]...), summary, nil
	}
	start, end := p.HistoryStart, p.HistoryStart+len(p.Prefix)
	if start < 0 || p.Summary != summary || len(messages) < end || !sameSource(messages[start:end], p.Prefix) {
		return nil, "", fmt.Errorf("compression source changed")
	}
	if strings.TrimSpace(out.Summary) == "" {
		return nil, "", fmt.Errorf("empty historical memory")
	}
	keep := make(map[int]bool, len(out.Keep))
	count, total, kept, keptTokens := 0, 0, 0, 0
	for _, index := range out.Keep {
		if index < 0 || index >= len(p.Prefix) || keep[index] || !IsUserMessage(p.Prefix[index]) {
			return nil, "", fmt.Errorf("invalid retained user index")
		}
		keep[index] = true
	}
	result := make([]Message, 0, len(out.Keep)+len(messages)-len(p.Prefix))
	result = append(result, messages[:start]...)
	for i, m := range p.Prefix {
		if m.Role != model.RoleUser {
			continue
		}
		if m.Origin == OriginLegacy {
			keep[i] = true
		}
		if m.Origin != OriginTool && m.Origin != OriginLegacy {
			count++
			total += model.EstimateTokens(m.Text())
			if keep[i] {
				kept++
				keptTokens += model.EstimateTokens(m.Text())
			}
		}
		if keep[i] {
			result = append(result, messages[start+i])
		}
	}
	if kept*2 < count || keptTokens*2 < total {
		return nil, "", fmt.Errorf("retained user originals below required half")
	}
	return append(result, messages[end:]...), out.Summary, nil
}
func sameSource(a, b []Message) bool {
	for i := range a {
		x, y := ProjectMessage(a[i]), b[i]
		x.Consumed = false
		y.Consumed = false
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}

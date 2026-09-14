package toolview

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/tui/components/scroll"
)

// DetailLabels 是工具详情浮层所需文案，保持组件包不感知 i18n 实现。
type DetailLabels struct {
	DetailTitle        string
	SubtaskDetailTitle string
	SubtaskToolTitle   string
	Tool               string
	Intent             string
	Params             string
	Guard              string
	GuardDecision      string
	GuardReadOnly      string
	GuardSource        string
	GuardReason        string
	Result             string
	Bytes              string
	Truncated          string
	Model              string
	Tools              string
	Task               string
	Context            string
	SideEffects        string
	Scroll             string
	Prev               string
	Next               string
	Close              string
}

// DetailDeps 汇总工具详情渲染依赖。详情现在就地嵌入工具块，
// 因此不再需要浮层尺寸/位置等字段。
type DetailDeps struct {
	Width  int
	Labels DetailLabels
	Styles RenderStyles

	GuardDecisionBadge func(*GuardInfo) string
	ReadOnlyBadge      func(bool) string
}

func (d DetailDeps) width() int {
	if d.Width <= 0 {
		return 80
	}
	return d.Width
}

func (d DetailDeps) innerWidth() int {
	w := max(44, min(104, d.width()-4))
	return max(24, w-8)
}

func DetailLineSource(te *Entry, deps DetailDeps) scroll.LineSource {
	if te == nil {
		return scroll.SliceSource(nil)
	}
	return BuildDetailLineSource(te, deps.innerWidth(), deps)
}

func BuildDetailLineSource(te *Entry, inner int, deps DetailDeps) scroll.LineSource {
	if te == nil {
		return scroll.SliceSource(nil)
	}
	var sections scroll.Sections
	appendLines := func(lines ...string) {
		sections = append(sections, scroll.SliceSource(lines))
	}
	appendWrapped := func(content string) {
		sections = append(sections, scroll.NewWrappedLineSection(content, inner, deps.Styles.ToolDim))
	}
	labels := deps.Labels
	title := labels.DetailTitle
	if IsSubtask(te) {
		title = labels.SubtaskDetailTitle
	} else if IsSubtaskChild(te) {
		title = labels.SubtaskToolTitle
	}
	appendLines(deps.Styles.HL.Render(title))
	appendLines(deps.Styles.Dim.Render(labels.Tool+": ") + deps.Styles.ToolDim.Render(te.RawName))
	if strings.TrimSpace(te.Intent) != "" {
		appendLines(deps.Styles.Dim.Render(labels.Intent))
		appendWrapped(te.Intent)
	}
	if IsSubtask(te) {
		appendSubtaskParams(&sections, te, inner, deps)
	} else if te.Params != "" {
		appendLines("", deps.Styles.Dim.Render(labels.Params))
		appendWrapped(te.Params)
	}
	if te.Guard != nil {
		appendLines("", deps.Styles.Dim.Render(labels.Guard))
		decision := ""
		if deps.GuardDecisionBadge != nil {
			decision = deps.GuardDecisionBadge(te.Guard)
		}
		appendLines(deps.Styles.Dim.Render(labels.GuardDecision) + " " + decision)
		readOnly := ""
		if deps.ReadOnlyBadge != nil {
			readOnly = deps.ReadOnlyBadge(te.Guard.ReadOnly)
		}
		appendLines(deps.Styles.Dim.Render(labels.GuardReadOnly) + " " + readOnly)
		if te.Guard.Source != "" {
			appendLines(deps.Styles.Dim.Render(labels.GuardSource) + " " + deps.Styles.ToolDim.Render(te.Guard.Source))
		}
		if strings.TrimSpace(te.Guard.Reason) != "" {
			appendLines(deps.Styles.Dim.Render(labels.GuardReason))
			appendWrapped(te.Guard.Reason)
		}
	}
	if te.Result != "" {
		if IsSubtask(te) {
			// 子任务结果是 spawn 的 JSON 载荷，展示前解析为可读小节，
			// 避免把 {"status":...,"side_effects":...} 原样丢给用户。
			AppendSubtaskResult(&sections, te, inner, deps)
		} else {
			meta := labels.Result
			if te.ResultBytes > 0 {
				meta += fmt.Sprintf(" · %d %s", te.ResultBytes, labels.Bytes)
			}
			if te.ResultTruncated {
				meta += " · " + labels.Truncated
			}
			appendLines("", deps.Styles.Dim.Render(meta))
			appendWrapped(te.Result)
		}
	}
	return sections
}

func appendSubtaskParams(sections *scroll.Sections, te *Entry, width int, deps DetailDeps) {
	if sections == nil || te == nil || len(te.ParamsRaw) == 0 {
		return
	}
	appendLines := func(lines ...string) {
		*sections = append(*sections, scroll.SliceSource(lines))
	}
	appendWrapped := func(content string) {
		*sections = append(*sections, scroll.NewWrappedLineSection(content, width, deps.Styles.ToolDim))
	}
	if model, ok := te.ParamsRaw["model"]; ok {
		appendLines("", deps.Styles.Dim.Render(deps.Labels.Model))
		appendLines(deps.Styles.ToolDim.Render(fmt.Sprintf("%v", model)))
	}
	if tools, ok := te.ParamsRaw["tools"]; ok {
		appendLines("", deps.Styles.Dim.Render(deps.Labels.Tools))
		appendWrapped(fmt.Sprintf("%v", tools))
	}
	if task, ok := te.ParamsRaw["task"]; ok {
		appendLines("", deps.Styles.Dim.Render(deps.Labels.Task))
		appendWrapped(fmt.Sprintf("%v", task))
	}
	if ctx, ok := te.ParamsRaw["context"]; ok {
		if text := strings.TrimSpace(fmt.Sprintf("%v", ctx)); text != "" {
			appendLines("", deps.Styles.Dim.Render(deps.Labels.Context))
			appendWrapped(text)
		}
	}
}

// SubtaskResult 是解析后的子任务结果。
// 集中承载正文、副作用披露与错误原因，避免调用方各自解析 JSON。
type SubtaskResult struct {
	Text        string
	SideEffects string
	Error       string
}

// ParseSubtaskResult 解析 spawn 结果 JSON，返回可读正文、副作用披露与错误原因。
// 解析失败时回退为原始文本，保证任何结果都有可读输出。
func ParseSubtaskResult(result string) SubtaskResult {
	text, sideEffects, errText := SubtaskResultText(result)
	return SubtaskResult{Text: text, SideEffects: sideEffects, Error: errText}
}

// SubtaskResultText 从 spawn 结果 JSON 中提取人类可读内容。
// 返回 text 是子任务的最终回复；sideEffects 非空时是需要向用户披露的副作用摘要；
// errText 是失败原因。解析失败时回退为原始文本，保证任何结果都有可读输出。
func SubtaskResultText(result string) (text string, sideEffects string, errText string) {
	raw := strings.TrimSpace(result)
	if raw == "" {
		return "", "", ""
	}
	var payload struct {
		Status      string `json:"status"`
		Result      string `json:"result"`
		Error       string `json:"error"`
		SideEffects struct {
			Status  string `json:"status"`
			Summary string `json:"summary"`
		} `json:"side_effects"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		// 非 JSON（旧数据或纯文本结果）：按原文展示。
		return raw, "", ""
	}
	text = strings.TrimSpace(payload.Result)
	if text == "" {
		text = strings.TrimSpace(payload.Error)
	}
	if se := strings.TrimSpace(payload.SideEffects.Summary); se != "" &&
		!strings.EqualFold(strings.TrimSpace(payload.SideEffects.Status), "none") {
		sideEffects = se
	}
	return text, sideEffects, strings.TrimSpace(payload.Error)
}

// AppendSubtaskResult 追加子任务结果小节：结果正文 + 副作用披露。
// 结果正文全量写入 sections（由调用方的滚动窗口决定可见范围）。
func AppendSubtaskResult(sections *scroll.Sections, te *Entry, width int, deps DetailDeps) {
	if sections == nil || te == nil || strings.TrimSpace(te.Result) == "" {
		return
	}
	text, sideEffects, _ := SubtaskResultText(te.Result)
	if text == "" && sideEffects == "" {
		return
	}
	appendLines := func(lines ...string) {
		*sections = append(*sections, scroll.SliceSource(lines))
	}
	appendWrapped := func(content string) {
		*sections = append(*sections, scroll.NewWrappedLineSection(content, width, deps.Styles.ToolDim))
	}
	if text != "" {
		appendLines("", deps.Styles.Dim.Render(deps.Labels.Result))
		appendWrapped(text)
	}
	if sideEffects != "" {
		appendLines("", deps.Styles.Dim.Render(deps.Labels.SideEffects))
		appendWrapped(sideEffects)
	}
}

package toolview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

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
	Scroll             string
	Prev               string
	Next               string
	Close              string
}

// DetailDeps 汇总工具详情浮层渲染依赖。
type DetailDeps struct {
	Width            int
	OverlayMaxHeight int
	SelectedIndex    int
	SelectedTotal    int
	ShowPosition     bool
	Labels           DetailLabels
	Styles           RenderStyles
	Box              lipgloss.Style

	GuardDecisionBadge func(*GuardInfo) string
	ReadOnlyBadge      func(bool) string
}

func (d DetailDeps) width() int {
	if d.Width <= 0 {
		return 80
	}
	return d.Width
}

func (d DetailDeps) bodyHeight() int {
	return max(1, d.OverlayMaxHeight-7)
}

func (d DetailDeps) innerWidth() int {
	w := max(44, min(104, d.width()-4))
	return max(24, w-8)
}

// RenderDetailOverlay 渲染工具详情浮层，并通过 scrollOffset 维护虚拟滚动位置。
func RenderDetailOverlay(te *Entry, scrollOffset *int, deps DetailDeps) string {
	if te == nil {
		return ""
	}
	w := max(44, min(104, deps.width()-4))
	bodyHeight := deps.bodyHeight()
	// 工具结果可能很长；详情面板走虚拟数据源，只渲染当前可见窗口。
	source := DetailLineSource(te, deps)
	body, start, total := scroll.Window(source, bodyHeight, scrollOffset)
	lines := append([]string(nil), body...)
	lines = append(lines, "", deps.Styles.Dim.Render(DetailHelpText(start, bodyHeight, total, deps)))
	return deps.Box.Width(w).Padding(1, 2).Render(strings.Join(lines, "\n"))
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
	if deps.ShowPosition && deps.SelectedTotal > 0 {
		title += fmt.Sprintf(" · %d/%d", deps.SelectedIndex+1, deps.SelectedTotal)
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
	return sections
}

func DetailHelpText(start, height, total int, deps DetailDeps) string {
	var parts []string
	if total > height {
		parts = append(parts, fmt.Sprintf("PgUp/PgDn %s %d-%d/%d", deps.Labels.Scroll, start+1, min(total, start+height), total))
	}
	if deps.SelectedTotal > 1 {
		if deps.SelectedIndex > 0 {
			parts = append(parts, "↑ "+deps.Labels.Prev)
		}
		if deps.SelectedIndex < deps.SelectedTotal-1 {
			parts = append(parts, "↓ "+deps.Labels.Next)
		}
	}
	parts = append(parts, "Ctrl+T/Esc "+deps.Labels.Close)
	return strings.Join(parts, " · ")
}

func DetailPageStep(deps DetailDeps) int {
	return max(1, deps.bodyHeight()-1)
}

func ScrollDetail(te *Entry, scrollOffset *int, delta int, deps DetailDeps) {
	if scrollOffset == nil {
		return
	}
	if te == nil {
		*scrollOffset = 0
		return
	}
	bodyHeight := deps.bodyHeight()
	maxOffset := max(0, DetailLineSource(te, deps).Len()-bodyHeight)
	*scrollOffset += delta
	if *scrollOffset < 0 {
		*scrollOffset = 0
	}
	if *scrollOffset > maxOffset {
		*scrollOffset = maxOffset
	}
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
}

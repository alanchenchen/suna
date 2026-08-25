package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/tui/clipboard"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

// mouseSelection 记录一次鼠标拖选的状态。坐标均为终端屏幕坐标（0,0 为左上角），
// 行号是 Chat 页面内相对行（0 = transcript viewport 顶部），列号为单元格宽度。
// 拖选只在 Chat transcript 区域生效，composer/overlay 区域不参与。
type mouseSelection struct {
	// dragging 表示左键按下后尚未松开，期间 MouseMotionMsg 持续更新 anchor/cursor。
	dragging bool
	// anchor 是按下左键时的位置，cursor 是当前拖动位置；两者可任意先后，选区取 min/max。
	anchorX, anchorY int
	cursorX, cursorY int
	// copiedUntil 非零时在输入区上方显示“已复制”提示，到期自动清除。
	copiedUntil time.Time
	// lastCopied 只用于避免重复复制相同文本（如松开后再次点击同一位置）。
	lastCopied string
}

// selectionBounds 返回选区的规范化矩形（anchor 与 cursor 归一化后的起止行列）。
func (s mouseSelection) selectionBounds() (x0, y0, x1, y1 int) {
	x0, x1 = min(s.anchorX, s.cursorX), max(s.anchorX, s.cursorX)
	y0, y1 = min(s.anchorY, s.cursorY), max(s.anchorY, s.cursorY)
	return x0, y0, x1, y1
}

// transcriptViewportTop 是 Chat 页面中 transcript viewport 在屏幕上的起始行。
// Chat 布局固定为：pet 3 行 + 顶部分隔线 1 行，之后才是 viewport 内容。
const transcriptViewportTop = 4

// transcriptLineAtScreenY 返回屏幕行对应的 transcript 渲染行文本（带 ANSI 样式）。
// viewport 无 softwrap 时，其内容行与屏幕行一一对应：窗口内第 N 行就是
// Viewport.YOffset() + N 行。窗口外（overscan 未渲染）返回空。
func (t *TUI) transcriptLineAtScreenY(screenY int) string {
	line := screenY - transcriptViewportTop + t.chat.Viewport.YOffset()
	if line < 0 {
		return ""
	}
	content := t.chat.Viewport.GetContent()
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if line >= len(lines) {
		return ""
	}
	return lines[line]
}

// copyMouseSelection 提取当前选区文本并写入系统剪贴板。返回复制是否成功。
// 文本按行提取：整行选中时取完整行，部分选中时按单元格宽度切割（ansi.Cut 保持
// 宽字符完整性），然后剥离 ANSI 样式；行间用换行拼接。
func (t *TUI) copyMouseSelection() bool {
	if !t.mouseSel.dragging {
		return false
	}
	x0, y0, x1, y1 := t.mouseSel.selectionBounds()
	if x1 < x0 || y1 < y0 {
		return false
	}
	var sb strings.Builder
	for y := y0; y <= y1; y++ {
		// y 是屏幕行（anchor/cursor 直接来自鼠标事件），transcriptLineAtScreenY 内部会换算。
		rendered := t.transcriptLineAtScreenY(y)
		if rendered == "" {
			continue
		}
		line := ansi.Strip(rendered)
		lineWidth := ansi.StringWidth(line)
		// 列坐标按单元格宽度计算；超出行宽的部分（行尾空白）不产生文本。
		if x0 >= lineWidth {
			continue
		}
		end := min(x1+1, lineWidth)
		if x0 > 0 || end < lineWidth {
			line = ansi.Cut(line, x0, end)
		}
		// 行尾 padding 空格（viewport 按宽度补全）不属于内容，复制时去掉。
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
	}
	text := sb.String()
	if text == "" {
		return false
	}
	if text == t.mouseSel.lastCopied {
		return true
	}
	if !clipboardWriteText(text) {
		return false
	}
	t.mouseSel.lastCopied = text
	t.mouseSel.copiedUntil = time.Now().Add(mouseCopyFlashDuration)
	return true
}

// clipboardWriteText 是可替换的剪贴板写入入口，测试中可注入假实现验证选区文本。
var clipboardWriteText = clipboard.WriteText

// mouseCopyFlashDuration 是“已复制”提示的展示时长。
const mouseCopyFlashDuration = 1800 * time.Millisecond

// mouseCopyFlashActive 返回“已复制”提示是否仍在展示窗口内。
func (t *TUI) mouseCopyFlashActive() bool {
	return time.Now().Before(t.mouseSel.copiedUntil)
}

// clearMouseSelection 结束一次拖选（松开左键后调用），保留 copiedUntil 用于提示展示。
func (t *TUI) clearMouseSelection() {
	t.mouseSel.dragging = false
}

// chatOverlayOpen 返回 Chat 页面是否有 overlay 覆盖在 transcript 上方。
// 有 overlay 时禁用鼠标拖选，避免点击 overlay（工具详情、Guard 确认、列表等）误触发选区。
func (t *TUI) chatOverlayOpen() bool {
	return t.showHelp ||
		t.chat.ShowToolDetail ||
		t.chat.ModelPickerOpen ||
		t.chat.SkillsOverlayOpen ||
		t.chat.MCPOverlayOpen ||
		t.chat.MemoryOverlayOpen ||
		t.chat.SessionsOverlayOpen ||
		t.chat.ActiveInteractionKind() == chatpage.InteractionGuardConfirm ||
		t.chat.HasDiscardDraftConfirm() ||
		t.chat.ActiveImagePaste() != nil
}

// handleMouseSelection 处理拖选相关的鼠标事件。返回 true 表示事件已被拖选逻辑消费，
// 调用方不应继续走滚轮/viewport 等默认鼠标行为。
//
// 事件语义（bubbletea v2）：
//   - MouseClickMsg 是左键按下（Button == MouseLeft）
//   - MouseMotionMsg 是按下后的拖动（Button 可能为 MouseNone）
//   - MouseReleaseMsg 是松开（Button == MouseNone）
//
// 只有按下位置落在 transcript viewport 区域才启动拖选；composer/overlay 区域
// 保持原有行为（点击输入区、滚轮滚动等）。
func (t *TUI) handleMouseSelection(msg tea.MouseMsg) bool {
	switch mm := msg.(type) {
	case tea.MouseClickMsg:
		m := mm.Mouse()
		if m.Button != tea.MouseLeft {
			return false
		}
		// 点击发生在 composer（输入区/状态栏）时交给默认处理，不启动拖选。
		if t.mouseInComposer(mm) {
			return false
		}
		// 有 overlay 覆盖 transcript 时禁用拖选，避免点击 overlay 误触发选区。
		if t.chatOverlayOpen() {
			return false
		}
		// 只有落在 transcript viewport 屏幕范围内才启动拖选。
		if m.Y < transcriptViewportTop {
			return false
		}
		t.mouseSel.dragging = true
		t.mouseSel.anchorX, t.mouseSel.anchorY = m.X, m.Y
		t.mouseSel.cursorX, t.mouseSel.cursorY = m.X, m.Y
		t.syncContent()
		return true
	case tea.MouseMotionMsg:
		if !t.mouseSel.dragging {
			// 未在拖选中时不消费 motion，避免影响 viewport 的 hover 行为。
			return false
		}
		m := mm.Mouse()
		t.mouseSel.cursorX, t.mouseSel.cursorY = m.X, m.Y
		t.syncContent()
		return true
	case tea.MouseReleaseMsg:
		if !t.mouseSel.dragging {
			return false
		}
		m := mm.Mouse()
		t.mouseSel.cursorX, t.mouseSel.cursorY = m.X, m.Y
		t.copyMouseSelection()
		t.clearMouseSelection()
		t.syncContent()
		return true
	default:
		// 滚轮等其它鼠标事件不参与拖选。
		return false
	}
}

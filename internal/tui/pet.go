package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

type petState int

const (
	petIdle petState = iota
	petWorking
	petThinking
	petHappy
)

// 动画节奏：idle 眨眼慢速（500ms 一帧，闭眼约 1.5s，节奏自然不拖沓），
// working/thinking 快速轮换，happy 与 idle 同节奏。
const (
	petIdleBlinkInterval    = 500 * time.Millisecond
	petWorkingFrameInterval = 150 * time.Millisecond
	petThinkFrameInterval   = 200 * time.Millisecond
	// petHappyDuration 是 run 完成后开心眼的展示时长，到期自动回到 idle。
	petHappyDuration = 1600 * time.Millisecond
)

// petFace 描述一帧的眼睛内容与整体偏移；渲染时按尺寸居中。
// 形象为矩形小机器人：纯色块身体 + 一对眼睛，无嘴无边框。
// shift 表示眼睛行整体偏移（-1 左移 / 0 居中 / +1 右移），用于表达东张西望。
type petFace struct {
	eyes  string
	shift int
}

// petFaces 返回 welcome 大号宠物的帧序列；所有帧共享同一色块身体，只切换眼睛。
// 眼睛用 2 列宽方块（██），以深色前景绘制在身体色块上，保持纯色形象。
// 不用 ◉（U+25C9）等 Ambiguous 宽度字符：lipgloss 按 1 列计算、
// 中文终端按 2 列渲染，会导致眼睛行视觉偏左不居中。
// 所有帧内容宽度必须为偶数（眼睛 6 列、眨眼 4 列）：奇数宽度内容
// 在偶数宽度容器里 centerCell 只能 left/right 差 1，视觉偏右 0.5 格。
func petFaces(state petState) []petFace {
	switch state {
	case petWorking:
		// 工作：2 列宽呼吸灯（◒◒ 下半黑 → ◓◓ 上半黑），左右对称。
		return []petFace{
			{eyes: "◒◒  ◒◒"},
			{eyes: "◓◓  ◓◓"},
		}
	case petThinking:
		// 思考：2 列宽圆点旋转，末尾定格一拍。
		return []petFace{
			{eyes: "○○  ○○"},
			{eyes: "◔◔  ◔◔"},
			{eyes: "◑◑  ◑◑"},
			{eyes: "◕◕  ◕◕"},
			{eyes: "◕◕  ◕◕"},
		}
	case petHappy:
		// 完成：2 列宽笑眼眨动（▀▀ 眯眼笑 ↔ ▔▔ 半闭），保持方块形象；
		// 两帧交替有“开心得眼睛在动”的感觉，比单帧更像笑。
		return []petFace{
			{eyes: "▀▀  ▀▀"},
			{eyes: "▔▔  ▔▔"},
		}
	default:
		// 空闲：正视为主，偶尔东张西望和完整眨眼。
		return []petFace{
			{eyes: "██  ██"},
			{eyes: "██  ██", shift: -1},
			{eyes: "██  ██"},
			{eyes: "██  ██", shift: 1},
			{eyes: "██  ██"},
			{eyes: "▄▄  ▄▄"},
			{eyes: "▂▂  ▂▂"},
			{eyes: "▄▄  ▄▄"},
			{eyes: "██  ██"},
		}
	}
}

var bodyFill = lipgloss.NewStyle().Background(ColorBrand).Foreground(lipgloss.Color("0"))

// renderPet 渲染大号纯色矩形机器人（welcome 页使用）。
// 总宽固定 10 格、4 行：顶、眼睛、空、底；眼睛每只 2 列宽 ██（深色前景），
// 与 chat 迷你宠物共用同一套方块眼帧，形象一致；
// 东张西望同样通过 shift 整体偏移表达，与迷你宠物保持一致。
func renderPet(state petState, frame int) string {
	const bodyWidth = 10
	faces := petFaces(state)
	face := faces[frame%len(faces)]

	centered := centerCell(face.eyes, bodyWidth)
	if face.shift > 0 {
		centered = " " + strings.TrimSuffix(centered, " ")
	} else if face.shift < 0 {
		centered = strings.TrimPrefix(centered, " ") + " "
	}

	row0 := fillPetCell("", bodyWidth)
	row1 := renderEyeRow(centered, bodyWidth)
	row2 := fillPetCell("", bodyWidth)
	row3 := fillPetCell("", bodyWidth)

	return strings.Join([]string{row0, row1, row2, row3}, "\n")
}

// petFacesMini 返回 chat 迷你宠物的帧序列；与 welcome 共用同一形象，
// 但眼睛为 1 列宽方块（█），在 8 格宽身体里更小巧协调。
// 帧内容宽度保持偶数（眼睛 4 列、眨眼 4 列），居中精确。
func petFacesMini(state petState) []petFace {
	switch state {
	case petWorking:
		// 工作：1 列宽呼吸灯（◒ 下半黑 → ◓ 上半黑），左右对称。
		return []petFace{
			{eyes: "◒  ◒"},
			{eyes: "◓  ◓"},
		}
	case petThinking:
		// 思考：1 列宽圆点旋转，末尾定格一拍。
		return []petFace{
			{eyes: "○  ○"},
			{eyes: "◔  ◔"},
			{eyes: "◑  ◑"},
			{eyes: "◕  ◕"},
			{eyes: "◕  ◕"},
		}
	case petHappy:
		// 完成：2 列宽笑眼眨动（▀▀ 眯眼笑 ↔ ▀▔ 半闭），保持方块形象，
		// 比 idle 睁眼（1 列 ▀）更夸张；两帧交替表达“开心得眼睛在动”。
		return []petFace{
			{eyes: "▀▀  ▀▀"},
			{eyes: "▔▔  ▔▔"},
		}
	default:
		// 空闲：正视为主，偶尔东张西望和快速眨眼。
		// 眼睛用 ▀（上半块，1 列 × 1 格高 = 1:1 方形）：终端字符格宽高比 1:2，
		// 1 列全块 █ 视觉是“高矩形”，半块才是方形；
		// 用上半块而非下半块（▄）：▄ 渲染在列格下半，3 行结构里
		// 眼睛视觉中心偏下，▀ 偏上更符合机器人观感且居中。
		// 眨眼只用 1 帧 ▔（上八分之一细线）：welcome 闭眼 3 帧有
		// “半闭→全闭→半闭”过渡，而 chat 眯眼 3 帧都是同一字符，
		// 视觉上像“眯着不动”，显得久；1 帧眨眼 0.5s 干脆利落。
		return []petFace{
			{eyes: "▀  ▀"},
			{eyes: "▀  ▀", shift: -1},
			{eyes: "▀  ▀"},
			{eyes: "▀  ▀", shift: 1},
			{eyes: "▀  ▀"},
			{eyes: "▔  ▔"},
			{eyes: "▀  ▀"},
		}
	}
}

// renderMiniPet 渲染迷你纯色矩形机器人（chat 页左上角使用）。
// 总宽固定 8 格、3 行：顶、眼睛、底；眼睛在中间行，垂直居中，
// 避免 2 行结构下眼睛贴顶。
// 与 welcome 大号宠物共用同一形象，但眼睛为 1 列宽方块（petFacesMini），
// 在 8 格宽身体里更小巧；东张西望通过 shift 偏移眼睛列表达，
// 避免单眼亮暗造成“往左看”的错觉。
func renderMiniPet(state petState, frame int) string {
	const bodyWidth = 8
	faces := petFacesMini(state)
	face := faces[frame%len(faces)]

	centered := centerCell(face.eyes, bodyWidth)
	if face.shift > 0 {
		// 右移：右侧去掉一个空格，左侧补一个空格。
		centered = " " + strings.TrimSuffix(centered, " ")
	} else if face.shift < 0 {
		// 左移：左侧去掉一个空格，右侧补一个。
		centered = strings.TrimPrefix(centered, " ") + " "
	}
	row0 := fillPetCell("", bodyWidth)
	row1 := renderEyeRow(centered, bodyWidth)
	row2 := fillPetCell("", bodyWidth)

	return strings.Join([]string{row0, row1, row2}, "\n")
}

// centerCell 将内容按指定宽度居中，两侧补空格。
// 奇数余量时左侧多补 1 格，使内容视觉中心对齐容器中心
// （否则 3 格内容在 8 格容器里会偏左 1 格）。
func centerCell(s string, width int) string {
	content := strings.TrimSpace(s)
	contentWidth := lipgloss.Width(content)
	if contentWidth >= width {
		return content
	}
	totalPad := width - contentWidth
	left := (totalPad + 1) / 2
	right := totalPad - left
	return strings.Repeat(" ", left) + content + strings.Repeat(" ", right)
}

// renderEyeRow 渲染眼睛行：所有字符统一使用身体色块（bodyFill），
// 眼睛以深色前景绘制在身体上，保持纯色形象；输入已按 width 居中，逐字符着色即可。
func renderEyeRow(s string, width int) string {
	var sb strings.Builder
	for _, r := range s {
		sb.WriteString(bodyFill.Render(string(r)))
	}
	return sb.String()
}

func padCell(s string, width int) string {
	for lipgloss.Width(s) < width {
		s += " "
	}
	return s
}

func fillPetCell(s string, width int) string {
	// lipgloss 的背景色只覆盖实际渲染宽度；这里统一补齐宽度后再设置 Width，
	// 避免中文终端/不同字体下小 logo 出现蓝色填充断裂。
	return bodyFill.Width(width).Render(padCell(s, width))
}

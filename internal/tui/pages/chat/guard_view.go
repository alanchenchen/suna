package chat

type GuardOverlayLabels struct {
	Title      string
	Tool       string
	Risk       string
	Review     string
	Reason     string
	Suggestion string
	Params     string
	Approve    string
	Reject     string
	Help       string
	Hidden     string
	Scroll     string
}

type GuardOverlayView struct {
	Guard      *GuardConfirmView
	Width      int
	Inner      int
	BodyHeight int
	Labels     GuardOverlayLabels
}

// GuardOverlayView 只产出 guard 浮层结构数据；风险颜色、滚动窗口和最终样式由 root adapter 处理。
func (m Model) GuardOverlayView(width, overlayMaxHeight int, labels GuardOverlayLabels) GuardOverlayView {
	w := maxInt(44, minInt(76, width-4))
	return GuardOverlayView{
		Guard:      m.ActiveGuard(),
		Width:      w,
		Inner:      maxInt(20, w-8),
		BodyHeight: maxInt(0, minInt(12, overlayMaxHeight-12)),
		Labels:     labels,
	}
}

func GuardHelpText(start, height, total int, labels GuardOverlayLabels) string {
	base := labels.Help
	if total <= height {
		return base
	}
	if height <= 0 {
		return base + " · " + labels.Hidden
	}
	return formatGuardScrollHelp(base, labels.Scroll, start, height, total)
}

func formatGuardScrollHelp(base, scroll string, start, height, total int) string {
	end := start + height
	if end > total {
		end = total
	}
	return base + " · ↑↓ PgUp/PgDn " + scroll + " " + itoa(start+1) + "-" + itoa(end) + "/" + itoa(total)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

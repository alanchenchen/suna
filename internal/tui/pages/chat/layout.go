package chat

const (
	InputMaxHeight        = 6
	MouseWheelDelta       = 3
	MaxCommandSuggestions = 10
)

type LayoutInput struct {
	Width              int
	Height             int
	InputAreaHeight    int
	SuggestionHeight   int
	PreInputHintHeight int
}

type Layout struct {
	ViewportHeight int
	InputWidth     int
}

// ComputeLayout 只负责 Chat 页面尺寸策略；具体 Bubble components 由调用方设置。
func ComputeLayout(in LayoutInput) Layout {
	if in.Width == 0 || in.Height == 0 {
		return Layout{}
	}
	inputAreaH := max(1, in.InputAreaHeight)
	suggestionH := max(0, in.SuggestionHeight)
	preInputHintH := max(0, in.PreInputHintHeight)
	// 固定区域按实际渲染行数计算：头部 3 行、顶部分隔线、输入分隔线、状态栏。
	// 不再额外强塞底部空白行，避免终端在首轮输入/图片粘贴时出现动态多一行。
	fixedH := 6 + inputAreaH + suggestionH + preInputHintH
	return Layout{ViewportHeight: max(3, in.Height-fixedH), InputWidth: max(20, in.Width-8)}
}

type ComposerHitInput struct {
	Height             int
	Y                  int
	InputAreaHeight    int
	SuggestionHeight   int
	PreInputHintHeight int
}

func MouseInComposer(in ComposerHitInput) bool {
	if in.Height <= 0 {
		return false
	}
	inputAreaH := max(1, in.InputAreaHeight)
	suggestionH := max(0, in.SuggestionHeight)
	preInputHintH := max(0, in.PreInputHintHeight)
	composerStart := in.Height - (preInputHintH + inputAreaH + suggestionH + 2)
	return in.Y >= composerStart
}

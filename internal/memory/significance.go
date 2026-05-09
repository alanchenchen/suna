package memory

import (
	"strings"
	"unicode"
)

type Significance string

const (
	SignificanceHigh   Significance = "high"
	SignificanceMedium Significance = "medium"
	SignificanceLow    Significance = "low"
)

/*
JudgeSignificance 判断交互的显著性等级（零 LLM 成本）。

高显著性（立即触发提取）：
  - 用户说"以后都这样"/"记住"/"不要"等明确指令
  - 工具执行失败
  - Guard 拦截了操作
  - 用户纠正了 agent 的输出

中显著性（正常排队）：
  - 包含工具调用的交互
  - 用户的非简单查询消息
  - Agent 做出了决策

低显著性（跳过提取）：
  - 纯闲聊 / 简单问候
  - 用户只回复"好"/"继续"/"OK"
  - 单轮信息查询
*/
func JudgeSignificance(userInput, agentOutput string, hadToolCall, toolFailed, guardBlocked, userCorrection bool) Significance {
	if guardBlocked || userCorrection || toolFailed {
		return SignificanceHigh
	}

	userLower := strings.ToLower(strings.TrimSpace(userInput))
	if isExplicitRemember(userLower) {
		return SignificanceHigh
	}

	if hadToolCall {
		return SignificanceMedium
	}

	if isTrivialInput(userLower) {
		return SignificanceLow
	}

	if len([]rune(userInput)) > 20 || containsDecision(agentOutput) {
		return SignificanceMedium
	}

	return SignificanceLow
}

var rememberPatterns = []string{
	"记住", "以后都", "以后都这样", "不要", "别这样",
	"always", "never", "remember", "from now on",
	"don't", "do not", "make sure", "keep in mind",
	"我喜欢", "我偏好", "我习惯", "我通常",
	"i prefer", "i like", "i usually", "my preference",
}

func isExplicitRemember(input string) bool {
	for _, p := range rememberPatterns {
		if strings.Contains(input, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

var trivialInputs = map[string]bool{
	"好": true, "好的": true, "ok": true, "okay": true,
	"继续": true, "嗯": true, "对": true, "是": true,
	"yes": true, "yeah": true, "yep": true, "sure": true,
	"continue": true, "go": true, "go on": true,
	"谢谢": true, "thanks": true, "thx": true,
	"没问题": true, "算了": true, "不了": true,
	"no": true, "nope": true, "不用": true,
}

func isTrivialInput(input string) bool {
	if trivialInputs[input] {
		return true
	}
	trimmed := strings.TrimFunc(input, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	if trivialInputs[trimmed] {
		return true
	}
	if len([]rune(input)) <= 3 && !containsCJK(input) {
		return true
	}
	return false
}

var decisionPhrases = []string{
	"决定", "选择", "确认", "修改", "创建", "删除", "更新",
	"decided", "chose", "confirmed", "modified", "created", "deleted", "updated",
	"will use", "let's use", "switching to",
}

func containsDecision(output string) bool {
	outputLower := strings.ToLower(output)
	for _, p := range decisionPhrases {
		if strings.Contains(outputLower, p) {
			return true
		}
	}
	return false
}

func containsCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) {
			return true
		}
	}
	return false
}

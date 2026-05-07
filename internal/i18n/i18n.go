package i18n

import (
	"os"
	"strings"
)

// LocaleID 语言标识，目前只支持中英文
type LocaleID string

const (
	EN LocaleID = "en"
	ZH LocaleID = "zh"
)

// I18n 简单国际化框架。
// 核心设计：key → map[locale]translation，TUI 层查询渲染。
// 口子已留：LoadLocale 可从外部文件加载翻译。
type I18n struct {
	locale LocaleID
	keys   map[string]map[LocaleID]string
}

// New 创建 i18n 实例，使用默认翻译表
func New(locale LocaleID) *I18n {
	return &I18n{
		locale: locale,
		keys:   defaultKeys(),
	}
}

// T 翻译 key 对应当前 locale 的文本，找不到则返回 key 本身
func (i *I18n) T(key string) string {
	if translations, ok := i.keys[key]; ok {
		if text, ok := translations[i.locale]; ok && text != "" {
			return text
		}
		if text, ok := translations[EN]; ok && text != "" {
			return text
		}
	}
	return key
}

// Tf 翻译并格式化，等同于 T(key) + fmt.Sprintf
func (i *I18n) Tf(key string, args ...any) string {
	text := i.T(key)
	if len(args) > 0 {
		return strings.ReplaceAll(text, "{}", strings.Trim(strings.Join(strings.Fields(fmtSprintf(args...)), " "), "[]"))
	}
	return text
}

// SetLocale 切换语言
func (i *I18n) SetLocale(locale LocaleID) {
	i.locale = locale
}

// LoadLocale 从文件加载翻译，预留扩展口。
// 文件格式：每行 key=en_text|zh_text
func (i *I18n) LoadLocale(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		translations := strings.SplitN(parts[1], "|", 2)
		if i.keys[key] == nil {
			i.keys[key] = make(map[LocaleID]string)
		}
		i.keys[key][EN] = translations[0]
		if len(translations) > 1 {
			i.keys[key][ZH] = translations[1]
		}
	}
	return nil
}

func fmtSprintf(args ...any) string {
	var parts []string
	for _, a := range args {
		parts = append(parts, anyToString(a))
	}
	return strings.Join(parts, " ")
}

func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return intToStr(t)
	case int64:
		return int64ToStr(t)
	case float64:
		return float64ToStr(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := 20
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func int64ToStr(i int64) string {
	return intToStr(int(i))
}

func float64ToStr(f float64) string {
	return intToStr(int(f))
}

// defaultKeys 内置翻译表
func defaultKeys() map[string]map[LocaleID]string {
	return map[string]map[LocaleID]string{
		// 状态
		"status.thinking":    {EN: "Thinking...", ZH: "思考中..."},
		"status.done":        {EN: "Done", ZH: "完成"},
		"status.error":       {EN: "Error", ZH: "错误"},
		"status.connected":   {EN: "Connected", ZH: "已连接"},

		// TUI 标签
		"label.you":          {EN: "You", ZH: "你"},
		"label.suna":         {EN: "Suna", ZH: "Suna"},
		"label.tool":         {EN: "Tool", ZH: "工具"},
		"label.system":       {EN: "System", ZH: "系统"},
		"label.error":        {EN: "Error", ZH: "错误"},
		"label.input_hint":   {EN: "Type your message...", ZH: "输入消息..."},

		// 会话
		"session.new":        {EN: "New session started", ZH: "新会话已开始"},
		"session.restored":   {EN: "Session restored", ZH: "会话已恢复"},

		// 命令帮助
		"cmd.help_title":     {EN: "Available commands:", ZH: "可用命令："},
		"cmd.new":            {EN: "Start new session", ZH: "新建会话"},
		"cmd.model":          {EN: "List/switch models", ZH: "列出/切换模型"},
		"cmd.memory":         {EN: "Search memories", ZH: "搜索记忆"},
		"cmd.compact":        {EN: "Compress context", ZH: "压缩上下文"},
		"cmd.usage":          {EN: "Show token usage", ZH: "查看用量"},
		"cmd.help":           {EN: "Show this help", ZH: "显示帮助"},
		"cmd.unknown":        {EN: "Unknown command: {}", ZH: "未知命令：{}"},

		// 记忆
		"memory.not_found":   {EN: "No memories found", ZH: "未找到记忆"},
		"memory.search_hint": {EN: "Usage: /memory search <query>", ZH: "用法：/memory search <查询词>"},

		// 启动
		"start.no_config":    {EN: "No model configuration found. Please create ~/.suna/config.toml with at least one [models.default] section.", ZH: "未找到模型配置。请创建 ~/.suna/config.toml，至少包含一个 [models.default] 段。"},
		"start.no_api_key":   {EN: "API key not set for model '{}'. Please set environment variable '{}'.", ZH: "模型 '{}' 的 API Key 未设置。请设置环境变量 '{}'。"},
	}
}

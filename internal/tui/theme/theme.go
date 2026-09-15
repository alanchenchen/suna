// Package theme 负责主题文件的解析、校验与颜色适配。
//
// 它是纯子系统：只依赖标准库与 TOML，不引用任何 TUI 类型，
// 因此可以独立测试，也让 TUI 根包只保留"调色板 → 样式"的展示职责。
//
// 主题文件只接受 #rrggbb：只有十六进制才能反推出确定的 RGB，从而精确计算
// 对比度并做深浅适配。ANSI 索引（"14" 这类）的真实 RGB 由终端主题决定，
// Suna 无法测量，因此不支持。
package theme

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Default 是唯一的内置主题名：TUI 按终端背景自动适配深浅。
const Default = "default"

// Colors 是主题文件里的原始颜色定义（全部为 #rrggbb）。
// 必填 5 个语义色；可选 4 个中性色，缺省时按终端背景派生。
type Colors struct {
	Accent  string `toml:"accent"`
	Success string `toml:"success"`
	Info    string `toml:"info"`
	Warning string `toml:"warning"`
	Error   string `toml:"error"`

	Text    string `toml:"text"`
	Muted   string `toml:"muted"`
	Dim     string `toml:"dim"`
	Surface string `toml:"surface"`
}

// Palette 是适配后的调色板：所有颜色都已是可直接渲染的绝对值。
type Palette struct {
	Name string
	// Dark 表示该调色板适配的是深色终端背景，决定 markdown 风格与组件默认样式。
	Dark bool

	Accent  color.Color // 主题主色：标题、光标、选中背景、spinner、logo
	Success color.Color // agent 消息、成功状态、diff 新增行
	Info    color.Color // 用户消息、输入框提示符
	Warning color.Color // 工具名、Guard 警告
	Error   color.Color // 错误消息、diff 删除行

	Text    color.Color // 正文
	Muted   color.Color // 弱化文字（工具意图、建议、placeholder）
	Dim     color.Color // 装饰（边框、分隔线、提示行、元信息）
	Surface color.Color // 表面背景（代码块）

	// 色块上的文字色：按各自背景亮度自动计算，避免浅色主题下文字看不见。
	OnAccent  color.Color
	OnSuccess color.Color
	OnWarning color.Color
	OnError   color.Color
}

// Spec 是一套可用的主题：名字 + 原始颜色（未适配）。
type Spec struct {
	Name   string
	Colors Colors
	// Err 非空表示该主题不可用（解析或校验失败），列表里展示错误摘要但不阻塞启动。
	Err string
}

// DefaultColors 是内置 default 的原始配色（按深色终端设计，浅色终端由适配逻辑映射，
// 因此只需维护一套）。色相取自 Tokyo Night（冷调霓虹、现代终端主流审美），
// 亮度按语义权重统一。
//
// 五个语义色在深色终端上的对比度都被校准到约 9.5:1：语义色是"标记"而非正文，
// 需要彼此权重相当——否则某个状态色会比别的更抢眼（例如亮黄的工具名盖过正文），
// 或者本该醒目的错误色反而最弱。统一权重后，颜色只表达"是什么"，不额外表达"多重要"。
//
// 三级文字色阶：Text 是用户要读的内容（约 14:1），
// Muted 是帮助/描述/元信息（约 7:1），Dim 只用于分隔线与边框等装饰（约 4:1）。
func DefaultColors() Colors {
	return Colors{
		Accent:  "#ceb6f9", // 紫：品牌色，标题、光标、spinner、logo
		Success: "#a0cf6e", // 黄绿：agent 消息、成功状态、diff 新增行
		Info:    "#a7c1fa", // 蓝：用户消息、输入框提示符
		Warning: "#e4ba7e", // 橙黄：工具名、Guard 警告
		Error:   "#faacba", // 粉红：错误消息、diff 删除行

		Text:    "#e4e9f7", // 冷白：正文
		Muted:   "#9aa5ce", // 蓝灰：帮助、描述、元信息
		Dim:     "#6f7bb1", // 深蓝灰：分隔线、边框、字形
		Surface: "#292e42", // 深蓝：代码块背景
	}
}

// requiredKeys 是必填的语义色，顺序用于错误信息。
var requiredKeys = []string{"accent", "success", "info", "warning", "error"}

// validateColors 校验必填项与颜色格式。
func validateColors(c *Colors) error {
	values := map[string]*string{
		"accent":  &c.Accent,
		"success": &c.Success,
		"info":    &c.Info,
		"warning": &c.Warning,
		"error":   &c.Error,
		"text":    &c.Text,
		"muted":   &c.Muted,
		"dim":     &c.Dim,
		"surface": &c.Surface,
	}
	for _, key := range requiredKeys {
		v := strings.TrimSpace(*values[key])
		if v == "" {
			return fmt.Errorf("colors.%s is required", key)
		}
		if _, ok := parseHex(v); !ok {
			return fmt.Errorf("colors.%s must be #rrggbb, got %q", key, v)
		}
	}
	for key, ptr := range values {
		v := strings.TrimSpace(*ptr)
		if v == "" {
			continue
		}
		if _, ok := parseHex(v); !ok {
			return fmt.Errorf("colors.%s must be #rrggbb, got %q", key, v)
		}
	}
	return nil
}

// parseThemeFile 解析并校验一个主题文件。返回的 Spec 在失败时带 Err。
func parseThemeFile(name, path string) Spec {
	spec := Spec{Name: name}
	raw, err := os.ReadFile(path)
	if err != nil {
		spec.Err = err.Error()
		return spec
	}
	var file struct {
		Colors Colors `toml:"colors"`
	}
	if err := toml.Unmarshal(raw, &file); err != nil {
		spec.Err = err.Error()
		return spec
	}
	if err := validateColors(&file.Colors); err != nil {
		spec.Err = err.Error()
		return spec
	}
	spec.Colors = file.Colors
	return spec
}

// LoadUser 扫描用户主题目录。目录不存在时返回空列表（不报错）。
// 解析失败的主题保留在列表中（带 Err），让用户在 UI 里看到原因。
func LoadUser(dir string) []Spec {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var specs []Spec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".toml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if name == Default {
			// 保护内置名：用户文件不能覆盖 default。
			specs = append(specs, Spec{Name: name, Err: `name "default" is reserved`})
			continue
		}
		specs = append(specs, parseThemeFile(name, filepath.Join(dir, e.Name())))
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// legacyNames 是旧配置值，启动时迁移为 Default。
var legacyNames = map[string]bool{"auto": true, "dark": true, "light": true}

// Normalize 把配置值归一为可用主题名（旧值归一为 Default）。
func Normalize(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || legacyNames[strings.ToLower(name)] {
		return Default
	}
	return name
}

// findSpec 在列表中查找主题。
func findSpec(specs []Spec, name string) (Spec, bool) {
	for _, s := range specs {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Resolve 解析主题名到具体调色板。
// 未知或不可用的主题回退到内置 default，保证界面始终可用。
func Resolve(name string, specs []Spec, bg Background) Palette {
	name = Normalize(name)
	if name == Default {
		return Adapt(Default, DefaultColors(), bg)
	}
	spec, ok := findSpec(specs, name)
	if !ok || spec.Err != "" {
		return Adapt(Default, DefaultColors(), bg)
	}
	return Adapt(spec.Name, spec.Colors, bg)
}

package tui

import (
	"fmt"
	"image/color"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"

	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

var mdCache sync.Map

func RenderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	r := markdownRenderer(width)
	text = defaultFenceLanguage(text)
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return out
}

func markdownRenderer(width int) *glamour.TermRenderer {
	key := fmt.Sprintf("%s:%d", currentTheme.Name, width)
	if v, ok := mdCache.Load(key); ok {
		return v.(*glamour.TermRenderer)
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyleConfig()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		r, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle(markdownStandardStyle()),
			glamour.WithWordWrap(width),
		)
	}
	mdCache.Store(key, r)
	return r
}

func markdownStyleConfig() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{Margin: uintPtr(0)},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Muted)},
			Indent:         uintPtr(1),
			IndentToken:    stringPtr("│ "),
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Text)},
			Margin:         uintPtr(0),
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:       colorPtr(currentTheme.Accent),
				Bold:        boolPtr(true),
				BlockSuffix: "\n",
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Accent), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Accent), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Accent), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Accent), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		Text: ansi.StylePrimitive{Color: colorPtr(currentTheme.Text)},
		Strong: ansi.StylePrimitive{
			Color: colorPtr(currentTheme.Text),
			Bold:  boolPtr(true),
		},
		Emph:        ansi.StylePrimitive{Italic: boolPtr(true)},
		Item:        ansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration: ansi.StylePrimitive{BlockPrefix: ". "},
		HorizontalRule: ansi.StylePrimitive{
			Color:  colorPtr(currentTheme.Dim),
			Format: "\n────────\n",
		},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Text)},
				Margin:         uintPtr(0),
			},
			LevelIndent: 2,
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           colorPtr(currentTheme.Text),
				BackgroundColor: colorPtr(currentTheme.Surface),
				BlockPrefix:     " ",
				BlockSuffix:     " ",
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: colorPtr(currentTheme.Text),
				},
				Margin: uintPtr(0),
			},
			// 语法高亮由主题色驱动，而非 chroma 内置主题：
			// 否则代码块配色与主题脱节，用户自定义主题时代码块仍是固定的 monokai。
			Chroma: markdownChromaConfig(),
		},
		Table: ansi.StyleTable{
			CenterSeparator: stringPtr("│"),
			ColumnSeparator: stringPtr("│"),
			RowSeparator:    stringPtr("─"),
		},
		Link: ansi.StylePrimitive{
			Color:     colorPtr(currentTheme.Info),
			Underline: boolPtr(true),
		},
	}
}

func defaultFenceLanguage(text string) string {
	lines := strings.Split(text, "\n")
	inFence := false
	fenceMarker := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(trimmed, fenceMarker) {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			if trimmed == "```" {
				lines[i] = leadingWhitespace(line) + "```bash"
			}
			inFence = true
			fenceMarker = "```"
			continue
		}
		if strings.HasPrefix(trimmed, "~~~") {
			if trimmed == "~~~" {
				lines[i] = leadingWhitespace(line) + "~~~bash"
			}
			inFence = true
			fenceMarker = "~~~"
		}
	}
	return strings.Join(lines, "\n")
}

func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// markdownStandardStyle 返回 glamour 标准样式名：自定义样式构建失败时兜底。
func markdownStandardStyle() string {
	if currentTheme.Dark {
		return "dark"
	}
	return "light"
}

// markdownChromaConfig 用主题调色板生成语法高亮配色。
//
// 映射原则：让代码块与主题同源，而不是套用 chroma 内置主题。
// 语义色各司其职——关键字用品牌色、函数用信息色、字符串用正向色、
// 数字用提醒色、注释与标点用弱化色，使代码块与 TUI 其余部分视觉统一。
func markdownChromaConfig() *ansi.Chroma {
	text := ansi.StylePrimitive{Color: colorPtr(currentTheme.Text)}
	muted := ansi.StylePrimitive{Color: colorPtr(currentTheme.Muted)}
	accent := ansi.StylePrimitive{Color: colorPtr(currentTheme.Accent)}
	info := ansi.StylePrimitive{Color: colorPtr(currentTheme.Info)}
	success := ansi.StylePrimitive{Color: colorPtr(currentTheme.Success)}
	warning := ansi.StylePrimitive{Color: colorPtr(currentTheme.Warning)}
	err := ansi.StylePrimitive{Color: colorPtr(currentTheme.Error)}

	return &ansi.Chroma{
		Text:                text,
		Error:               err,
		Comment:             muted,
		CommentPreproc:      muted,
		Keyword:             accent,
		KeywordReserved:     accent,
		KeywordNamespace:    accent,
		KeywordType:         warning,
		Operator:            muted,
		Punctuation:         muted,
		Name:                text,
		NameBuiltin:         info,
		NameTag:             accent,
		NameAttribute:       warning,
		NameClass:           warning,
		NameConstant:        warning,
		NameDecorator:       info,
		NameException:       err,
		NameFunction:        info,
		NameOther:           text,
		Literal:             success,
		LiteralNumber:       warning,
		LiteralDate:         success,
		LiteralString:       success,
		LiteralStringEscape: warning,
		GenericDeleted:      err,
		GenericEmph:         ansi.StylePrimitive{Color: colorPtr(currentTheme.Text), Italic: boolPtr(true)},
		GenericInserted:     success,
		GenericStrong:       ansi.StylePrimitive{Color: colorPtr(currentTheme.Text), Bold: boolPtr(true)},
		GenericSubheading:   accent,
		Background:          ansi.StylePrimitive{BackgroundColor: colorPtr(currentTheme.Surface)},
	}
}

// colorPtr 把调色板颜色转成 glamour 样式字段需要的字符串。
//
// 必须经 themesys.ColorString：Palette 的颜色是 color.RGBA，直接用
// fmt.Sprint 会得到 "{230 233 247 255}"，glamour 无法解析并静默回退成纯黑，
// 表现为正文颜色异常发暗。
func colorPtr(c color.Color) *string {
	s := themesys.ColorString(c)
	return &s
}

func boolPtr(v bool) *bool {
	return &v
}

func uintPtr(v uint) *uint {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

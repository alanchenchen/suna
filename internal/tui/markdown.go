package tui

import (
	"fmt"
	"image/color"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
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
			glamour.WithStandardStyle(currentTheme.MarkdownStyle),
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
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.MutedText)},
			Indent:         uintPtr(1),
			IndentToken:    stringPtr("│ "),
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Text)},
			Margin:         uintPtr(0),
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:       colorPtr(currentTheme.Brand),
				Bold:        boolPtr(true),
				BlockSuffix: "\n",
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Brand), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Brand), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Brand), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: colorPtr(currentTheme.Brand), Bold: boolPtr(true), BlockSuffix: "\n"},
		},
		Text: ansi.StylePrimitive{Color: colorPtr(currentTheme.Text)},
		Strong: ansi.StylePrimitive{
			Color: colorPtr(currentTheme.HL),
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
				Color:           colorPtr(currentTheme.HL),
				BackgroundColor: colorPtr(currentTheme.CodeBg),
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
			Theme: markdownCodeTheme(),
		},
		Table: ansi.StyleTable{
			CenterSeparator: stringPtr("│"),
			ColumnSeparator: stringPtr("│"),
			RowSeparator:    stringPtr("─"),
		},
		Link: ansi.StylePrimitive{
			Color:     colorPtr(currentTheme.User),
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

func markdownCodeTheme() string {
	if currentTheme.Name == ThemeLight {
		return "github"
	}
	return "monokai"
}

func colorPtr(c color.Color) *string {
	s := fmt.Sprint(c)
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

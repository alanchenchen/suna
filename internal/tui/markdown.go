package tui

import (
	"fmt"
	"sync"

	"charm.land/glamour/v2"
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
		glamour.WithStylesFromJSONBytes([]byte(markdownStyleJSON())),
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

func markdownStyleJSON() string {
	return fmt.Sprintf(`{
  "document": {"margin": 0},
  "heading": {"bold": true, "color": %q},
  "paragraph": {"margin": 0, "color": %q},
  "code_block": {"color": %q, "background_color": %q, "margin": 0},
  "code": {"color": %q, "background_color": %q},
  "list": {"margin": 0, "color": %q},
  "table": {"center_separator": "│", "column_separator": "│", "row_separator": "─"},
  "link": {"color": %q, "underline": true}
}`, currentTheme.HL, currentTheme.Text, currentTheme.Text, currentTheme.CodeBg, currentTheme.Text, currentTheme.CodeBg, currentTheme.Text, currentTheme.User)
}

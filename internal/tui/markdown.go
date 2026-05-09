package tui

import (
	"sync"

	"charm.land/glamour/v2"
)

var mdCache sync.Map

const sunaStyleJSON = `{
  "document": {"margin": 0},
  "heading": {"bold": true, "color": "15"},
  "paragraph": {"margin": 0},
  "code_block": {"color": "15", "background_color": "236", "margin": 0},
  "code": {"color": "15", "background_color": "236"},
  "list": {"margin": 0},
  "table": {"center_separator": "│", "column_separator": "│", "row_separator": "─"},
  "link": {"color": "12", "underline": true}
}`

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
	if v, ok := mdCache.Load(width); ok {
		return v.(*glamour.TermRenderer)
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes([]byte(sunaStyleJSON)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		r, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
		)
	}
	mdCache.Store(width, r)
	return r
}

package welcome

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestModelUpdateKey(t *testing.T) {
	m := New(Deps{Tr: func(key string) string { return key }})
	items := []Item{
		{LabelKey: "new", Action: ActionNew},
		{LabelKey: "resume", Action: ActionResume},
		{LabelKey: "help", Action: ActionHelp, Disabled: true},
	}
	m.SetItems(items, 80)
	if !m.HasItems() {
		t.Fatal("expected initialized menu")
	}

	if action, handled := m.UpdateKey("down", items); !handled || action != ActionNone {
		t.Fatalf("down = (%v, %v), want (%v, true)", action, handled, ActionNone)
	}
	if action, handled := m.UpdateKey("enter", items); !handled || action != ActionResume {
		t.Fatalf("enter on resume = (%v, %v), want (%v, true)", action, handled, ActionResume)
	}
	if action, handled := m.UpdateKey("down", items); !handled || action != ActionNone {
		t.Fatalf("down to disabled = (%v, %v), want (%v, true)", action, handled, ActionNone)
	}
	if action, handled := m.UpdateKey("enter", items); !handled || action != ActionNone {
		t.Fatalf("enter on disabled = (%v, %v), want (%v, true)", action, handled, ActionNone)
	}
	if _, handled := m.UpdateKey("x", items); handled {
		t.Fatal("unexpected handling for unrelated key")
	}
}

func TestSelectedItemUsesBrandStyle(t *testing.T) {
	m := New(Deps{
		Tr: func(key string) string { return key },
		Styles: Styles{
			Cursor: lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
			Dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
			HL:     lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true),
			Brand:  lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true),
		},
	})
	m.SetItems([]Item{{LabelKey: "config", Action: ActionConfig}}, 80)

	view := m.View()
	if !strings.Contains(view, "96m") {
		t.Fatalf("selected item should use brand cyan style, view=%q", view)
	}
	if !strings.Contains(view, "▎") {
		t.Fatalf("selected item should use selection rail, view=%q", view)
	}
	if strings.Contains(view, "▶") {
		t.Fatalf("selected item should not use arrow cursor, view=%q", view)
	}
	if strings.Contains(view, "38;5;15") {
		t.Fatalf("selected item should not use high-contrast white highlight, view=%q", view)
	}
}

func TestJoinPickerRendersTitleAndTruncatedCWD(t *testing.T) {
	m := New(Deps{Tr: func(key string) string { return key }})
	m.SetItems([]Item{{
		LabelKey: "tui.welcome.join_one",
		Key:      "A friendly session title that is too long",
		CWD:      "/a/very/long/workspace/path/that/must/not/wrap",
		Action:   ActionJoin,
	}}, 30)

	view := m.View()
	if !strings.Contains(view, "A friendly se...") {
		t.Fatalf("join title should be truncated to menu width, view=%q", view)
	}
	if !strings.Contains(view, "/a/very/long/...") {
		t.Fatalf("join cwd should be truncated to menu width, view=%q", view)
	}
	if strings.Count(view, "\n") < 1 {
		t.Fatalf("join picker entry should use title and cwd lines, view=%q", view)
	}
}

func stripANSI(s string) string {
	for {
		start := strings.Index(s, "\x1b[")
		if start < 0 {
			return s
		}
		end := start + 2
		for end < len(s) && (s[end] < '@' || s[end] > '~') {
			end++
		}
		if end == len(s) {
			return s[:start]
		}
		s = s[:start] + s[end+1:]
	}
}
func TestRenderViewFitsNarrowViewport(t *testing.T) {
	view := RenderView(ViewData{
		Width:         30,
		Pet:           "╭────────╮\n│  ◠  ◠  │\n│   ω    │\n╰────────╯",
		Info:          "Version  2026.06.very-long-version\nModel    provider/a-very-long-model-name-that-must-not-overflow\nWorkspace /a/very/long/workspace/path",
		Menu:          "one\ntwo",
		HasConfigured: true,
	}, ViewDeps{Tr: func(key string) string { return "long translated welcome text that must fit" }})
	for _, line := range strings.Split(stripANSI(view), "\n") {
		if got := lipgloss.Width(line); got > 30 {
			t.Fatalf("rendered line width = %d, want <= 30: %q", got, line)
		}
	}
}

func TestJoinPickerBackHasNoBlankSecondLine(t *testing.T) {
	m := New(Deps{Tr: func(key string) string {
		if key == "back_detail" {
			return "Return to welcome menu"
		}
		return key
	}})
	m.SetItems([]Item{
		{LabelKey: "Back", DetailKey: "back_detail", Action: ActionBack},
		{LabelKey: "Join", Key: "Title", CWD: "/workspace", Action: ActionJoin},
	}, 80)

	lines := strings.Split(stripANSI(m.View()), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		t.Fatalf("Back item has blank second line: %q", m.View())
	}
	if !strings.Contains(lines[1], "Return to welcome menu") {
		t.Fatalf("Back detail line = %q, want localized detail", lines[1])
	}
}

func TestSetItemsClampsWidthForUltraNarrowViewport(t *testing.T) {
	m := New(Deps{Tr: func(key string) string { return key }})
	m.SetItems([]Item{{LabelKey: "new", Action: ActionNew}}, 6)
	if got := m.menu.Width(); got < 1 {
		t.Fatalf("menu width = %d, want at least 1", got)
	}
}

func TestSetItemsClampsCursor(t *testing.T) {
	m := New(Deps{Tr: func(key string) string { return key }})
	items := []Item{
		{LabelKey: "new", Action: ActionNew},
		{LabelKey: "resume", Action: ActionResume},
	}
	m.SetItems(items, 80)
	_, _ = m.UpdateKey("down", items)

	items = []Item{{LabelKey: "new", Action: ActionNew}}
	m.SetItems(items, 80)
	if action, handled := m.UpdateKey("enter", items); !handled || action != ActionNew {
		t.Fatalf("clamped enter = (%v, %v), want (%v, true)", action, handled, ActionNew)
	}
}

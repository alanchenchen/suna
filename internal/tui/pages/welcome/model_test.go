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
	if strings.Contains(view, "38;5;15") {
		t.Fatalf("selected item should not use high-contrast white highlight, view=%q", view)
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

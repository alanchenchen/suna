package tui

import (
	"testing"

	"charm.land/bubbles/v2/list"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestProjectSkillEnterAndSpaceNeverProduceToggleCommand(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		filtering bool
	}{
		{name: "normal enter", key: "enter"},
		{name: "normal space", key: "space"},
		{name: "filtering enter", key: "enter", filtering: true},
		{name: "filtering space", key: " ", filtering: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tui := New(LocaleEN)
			tui.chat.SetSkills([]protocol.SkillInfo{{Name: "project-skill", Scope: "project", Valid: true, CanToggle: true}})
			tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())
			if tt.filtering {
				tui.chat.SkillsList.List().SetFilterState(list.Filtering)
			}

			_, cmd := tui.updateSkillsOverlay(tt.key, nil)
			if cmd != nil {
				t.Fatal("project skill interaction produced a command")
			}
		})
	}
}

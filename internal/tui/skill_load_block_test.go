package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

func TestSkillLoadUsesDedicatedBlockWithoutGenericToolBlock(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 30}
	tui.initChatComponents()

	tui.handleToolStartNotification(protocol.ToolStartParams{ID: "skill-1", Tool: "skill_load", Params: map[string]any{"name": "grilling"}})
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("messages after tool_start = %d, want one Skill message", got)
	}
	msg := tui.chat.Messages[0]
	view, ok := msg.Content.(*chatpage.SkillLoadView)
	if msg.Role != "skill" || !ok || view.Name != "grilling" {
		t.Fatalf("Skill message = %#v, want dedicated grilling block", msg)
	}
	if tui.chat.CurrentToolBlock != nil {
		t.Fatal("CurrentToolBlock is set for skill_load, want no generic Tool block")
	}

	started := view.StartedAt
	tui.chat.ToolStartTimes["skill-1"] = started.Add(-1500 * time.Millisecond)
	//专属 Skill 耗时以条目开始时间为准；这里直接移动开始时间以验证紧凑格式。
	view.StartedAt = started.Add(-1500 * time.Millisecond)
	tui.chat.ActiveTools["skill-1"].StartedAt = view.StartedAt
	tui.handleToolEndNotification(protocol.ToolEndParams{ID: "skill-1", Tool: "skill_load"})

	if view.Status != "loaded" || view.Duration < time.Second {
		t.Fatalf("Skill view after tool_end = %#v, want loaded with duration", view)
	}
	tui.handleSkillLoadNotification(protocol.SkillLoadParams{Name: "grilling", Status: "loaded"})
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("messages after skill notification = %d, want no duplicate", got)
	}
	tui.syncContent()
	plain := stripANSIForTest(tui.chat.Viewport.GetContent())
	if strings.Contains(plain, "工具 ·") || strings.Contains(plain, "skill_load") {
		t.Fatalf("transcript = %q, should not include generic skill_load Tool block", plain)
	}
	if !strings.Contains(plain, "已加载 SKILL") || !strings.Contains(plain, "grilling") || !strings.Contains(plain, "1.5s") {
		t.Fatalf("transcript = %q, want compact dedicated Skill block with duration", plain)
	}
}

func TestConsecutiveSkillLoadsShareSunaGroup(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 30}
	tui.initChatComponents()
	for i, name := range []string{"grilling", "handoff", "caveman"} {
		id := "skill-" + string(rune('1'+i))
		tui.handleToolStartNotification(protocol.ToolStartParams{ID: id, Tool: "skill_load", Params: map[string]any{"name": name}})
		tui.handleToolEndNotification(protocol.ToolEndParams{ID: id, Tool: "skill_load"})
	}

	tui.syncContent()
	plain := stripANSIForTest(tui.chat.Viewport.GetContent())
	if got := strings.Count(plain, "● Suna"); got != 1 {
		t.Fatalf("Suna headers = %d, want one for consecutive Skill blocks; transcript = %q", got, plain)
	}
	for _, name := range []string{"grilling", "handoff", "caveman"} {
		if !strings.Contains(plain, name) {
			t.Fatalf("transcript = %q, want Skill %q", plain, name)
		}
	}
}

package chat

import "testing"

func TestAllCommandsGrouped(t *testing.T) {
	seen := map[CommandGroup]bool{}
	for _, c := range AllCommands() {
		if c.Group == "" {
			t.Fatalf("command %s has empty group", c.Cmd)
		}
		seen[c.Group] = true
	}
	for _, g := range []CommandGroup{CommandGroupSession, CommandGroupManage, CommandGroupHelp} {
		if !seen[g] {
			t.Fatalf("group %q missing from AllCommands", g)
		}
	}
}

func TestAllCommandsIncludeAttachments(t *testing.T) {
	for _, c := range AllCommands() {
		if c.Cmd == "/attachments" {
			return
		}
	}
	t.Fatal("AllCommands missing /attachments")
}

func TestCommandGroupTitleKnown(t *testing.T) {
	for _, g := range []CommandGroup{CommandGroupSession, CommandGroupManage, CommandGroupHelp} {
		if CommandGroupTitle(g) == "" {
			t.Fatalf("CommandGroupTitle(%q) = empty, want i18n key", g)
		}
	}
	if CommandGroupTitle(CommandGroup("unknown")) != "" {
		t.Fatal("CommandGroupTitle(unknown) should be empty")
	}
}

func TestAttachmentsOverlayState(t *testing.T) {
	var m Model
	m.OpenAttachmentsOverlay()
	if !m.AttachmentsOverlayOpen {
		t.Fatal("OpenAttachmentsOverlay did not open")
	}
	if m.AttachmentsConfirm {
		t.Fatal("overlay should start without confirm")
	}
	m.BeginAttachmentsClear()
	if !m.AttachmentsConfirm {
		t.Fatal("BeginAttachmentsClear did not set confirm")
	}
	if !m.ConfirmAttachmentsClear() {
		t.Fatal("ConfirmAttachmentsClear should confirm once")
	}
	if m.AttachmentsConfirm {
		t.Fatal("confirm should reset after ConfirmAttachmentsClear")
	}
	m.CloseAttachmentsOverlay()
	if m.AttachmentsOverlayOpen {
		t.Fatal("CloseAttachmentsOverlay did not close")
	}
}

// 空白分隔参数识别：Tab、粘贴换行等空白形式与普通空格一致。
func TestRegisteredSlashCommandRecognizesWhitespaceSeparatedArguments(t *testing.T) {
	for _, input := range []string{"/mcp", "/mcp\t", "/mcp\nextra"} {
		if !IsRegisteredSlashCommand(input) {
			t.Fatalf("IsRegisteredSlashCommand(%q) = false, want true", input)
		}
	}
}

func TestRegisteredSlashCommandRejectsUnknownCommand(t *testing.T) {
	if IsRegisteredSlashCommand("/unknown") {
		t.Fatal("IsRegisteredSlashCommand(/unknown) = true, want false")
	}
}

func TestAttachmentsOverlayMutuallyExcludesOthers(t *testing.T) {
	var m Model
	m.OpenMemoryOverlay()
	m.OpenAttachmentsOverlay()
	if m.MemoryOverlayOpen {
		t.Fatal("attachments overlay should close memory overlay")
	}
	if !m.AttachmentsOverlayOpen {
		t.Fatal("attachments overlay should be open")
	}
}

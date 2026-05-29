package tui

import (
	"strings"
	"testing"
)

func TestRenderAttachmentPanelUsesBox(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.attachments = []attachmentItem{{Type: "image", Name: "ScreenShot_2026-05-29_121010_728.png", Size: 161500}}

	panel := stripANSIForTest(tui.renderAttachmentPanel())
	for _, want := range []string{"╭", "╰", "Pending attachments", "ScreenShot_2026-05-29_121010_728.png"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("attachment panel missing %q:\n%s", want, panel)
		}
	}
}

func TestRenderInputAreaSeparatesAttachmentBoxFromComposer(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 24}
	tui.initChatComponents()
	tui.ta.SetValue("describe this image")
	tui.attachments = []attachmentItem{{Type: "image", Name: "image.png", Size: 1024}}

	view := stripANSIForTest(tui.renderInputArea())
	boxEnd := strings.LastIndex(view, "╰")
	inputStart := strings.LastIndex(view, "describe this image")
	if boxEnd < 0 || inputStart < 0 || boxEnd >= inputStart {
		t.Fatalf("input area should render attachment box before composer:\n%s", view)
	}
	between := view[boxEnd:inputStart]
	if !strings.Contains(between, "──") {
		t.Fatalf("input area missing separator between attachment box and composer:\n%s", view)
	}
}

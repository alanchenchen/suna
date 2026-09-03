package tui

import (
	"strings"
	"testing"

	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func TestParseMediaSummaryExtractsNameAndSize(t *testing.T) {
	cases := []struct {
		name     string
		summary  string
		wantName string
		wantSize string
	}{
		{"attachment", "[image: photo.png, image/png, 1.2MB, source=attachment:photo.png]", "photo.png", "1.2MB"},
		{"path", "[image: photo.png, image/png, 270.0KB, source=/abs/photo.png]", "photo.png", "270.0KB"},
		{"url", "[image: ollama.png, image/png, source=https://example.com/ollama.png]", "ollama.png", ""},
		{"legacy", "[uploaded image: photo.png, image/png, 1.2MB, source=attachment:photo.png]", "photo.png", "1.2MB"},
		{"malformed", "not a summary", "", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			name, size := parseMediaSummary(tt.summary)
			if name != tt.wantName {
				t.Fatalf("parseMediaSummary(%q) name = %q, want %q", tt.summary, name, tt.wantName)
			}
			if size != tt.wantSize {
				t.Fatalf("parseMediaSummary(%q) size = %q, want %q", tt.summary, size, tt.wantSize)
			}
		})
	}
}

func TestRenderMediaSummaryShowsFriendlyStyle(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	line := stripANSIForTest(tui.renderMediaSummary("[image: photo.png, image/png, 1.2MB, source=attachment:photo.png]"))
	if !strings.Contains(line, "图片") {
		t.Fatalf("renderMediaSummary() = %q, want image label", line)
	}
	if !strings.Contains(line, "photo.png") {
		t.Fatalf("renderMediaSummary() = %q, want file name", line)
	}
	if !strings.Contains(line, "1.2MB") {
		t.Fatalf("renderMediaSummary() = %q, want size", line)
	}
	// ● 点前缀由 renderInlineUserMessage 统一添加（media 摘要走普通用户消息渲染路径），
	// renderMediaSummary 本身不应包含，避免双重 ● 点。
	if strings.Contains(line, "●") {
		t.Fatalf("renderMediaSummary() = %q, should not include user dot (added by renderInlineUserMessage)", line)
	}
	// 技术性 source 细节不应展示给用户。
	if strings.Contains(line, "source=") {
		t.Fatalf("renderMediaSummary() = %q, should not leak source detail", line)
	}
}

func TestMediaSummaryRendersSingleUserDot(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	// media 摘要消息的 role 是 user，走 renderUserMessage 的 string 分支，
	// ● 点由 renderInlineUserMessage 添加一次，renderMediaSummary 不再包含。
	content := tui.renderMediaSummary("[image: photo.png, image/png, 1.2MB, source=attachment:photo.png]")
	rendered := stripANSIForTest(tui.renderUserMessage(content, 80))
	if strings.Count(rendered, "●") != 1 {
		t.Fatalf("renderUserMessage(media) = %q, want exactly one user dot, got %d", rendered, strings.Count(rendered, "●"))
	}
	if !strings.Contains(rendered, "📷") {
		t.Fatalf("renderUserMessage(media) = %q, want image icon", rendered)
	}
	if strings.Contains(rendered, "source=") {
		t.Fatalf("renderUserMessage(media) = %q, should not leak source detail", rendered)
	}
}

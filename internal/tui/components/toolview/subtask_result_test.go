package toolview

import (
	"regexp"
	"strings"
	"testing"
)

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSIForTest(s string) string { return ansiPattern.ReplaceAllString(s, "") }

func TestSubtaskResultTextParsesSpawnPayload(t *testing.T) {
	payload := `{"status":"completed","result":"图片显示用量 26.8%","side_effects":{"status":"remaining","summary":"修改了 runner.go"}}`
	text, sideEffects, errText := SubtaskResultText(payload)
	if text != "图片显示用量 26.8%" {
		t.Fatalf("text = %q, want parsed result", text)
	}
	if sideEffects != "修改了 runner.go" {
		t.Fatalf("sideEffects = %q, want summary", sideEffects)
	}
	if errText != "" {
		t.Fatalf("errText = %q, want empty", errText)
	}
}

func TestSubtaskResultTextFallsBackToRawText(t *testing.T) {
	// 非 JSON（旧数据或纯文本结果）必须仍可读，不能因为解析失败丢内容。
	text, sideEffects, _ := SubtaskResultText("plain text result")
	if text != "plain text result" {
		t.Fatalf("text = %q, want raw fallback", text)
	}
	if sideEffects != "" {
		t.Fatalf("sideEffects = %q, want empty", sideEffects)
	}
}

func TestSubtaskResultTextIgnoresNoneSideEffects(t *testing.T) {
	payload := `{"status":"completed","result":"done","side_effects":{"status":"none","summary":"no changes"}}`
	_, sideEffects, _ := SubtaskResultText(payload)
	if sideEffects != "" {
		t.Fatalf("sideEffects = %q, want empty for none status", sideEffects)
	}
}

func TestSubtaskResultTextUsesErrorWhenResultEmpty(t *testing.T) {
	payload := `{"status":"failed","result":"","error":"model unavailable"}`
	text, _, errText := SubtaskResultText(payload)
	if text != "model unavailable" {
		t.Fatalf("text = %q, want error fallback", text)
	}
	if errText != "model unavailable" {
		t.Fatalf("errText = %q, want error", errText)
	}
}

func TestRenderToolTitledBoxKeepsHintWhenTitleLong(t *testing.T) {
	longTitle := "工具调用 · 5 个操作 · 3 个文件 · 2 个 Guard"
	out := renderToolTitledBox(48, longTitle, []string{"line"}, RenderStyles{}, "Ctrl+T 详情")
	plain := stripANSIForTest(out)
	if !strings.Contains(plain, "Ctrl+T 详情") {
		t.Fatalf("renderToolTitledBox() = %q, want hint preserved", plain)
	}
}

func TestRenderToolTitledBoxDropsHintWhenTooNarrow(t *testing.T) {
	out := renderToolTitledBox(20, "工具调用", []string{"line"}, RenderStyles{}, "Ctrl+T 详情")
	plain := stripANSIForTest(out)
	if strings.Contains(plain, "Ctrl+T") {
		t.Fatalf("renderToolTitledBox() = %q, want hint dropped when too narrow", plain)
	}
}

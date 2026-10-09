package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func chatHistoryBenchmarkFixture(turns int, kind string) *TUI {
	ui := &TUI{i18n: newTranslator(LocaleZH), width: 120, height: 40}
	ui.initChatComponents()
	for i := 0; i < turns; i++ {
		if kind == "tiny" {
			ui.chat.AppendMessage(chatMsg{Role: "panel", Content: "x"})
			continue
		}
		ui.chat.AppendMessage(chatMsg{Role: "user", Content: fmt.Sprintf("inspect turn %d", i)})
		if kind == "tools" || kind == "mixed" {
			block := ui.ensureToolBlock()
			for j := 0; j < 5; j++ {
				id := fmt.Sprintf("tool-%d-%d", i, j)
				entry := &toolEntry{ID: id, LocalID: id, RawName: "exec", Name: "Exec", Intent: "inspect workspace", Summary: "go test ./internal/tui/...", Result: strings.Repeat("result line\n", 16), Status: toolDone, Duration: time.Second}
				if kind == "mixed" && j == 0 {
					entry.RawName = "spawn"
					entry.Name = "Spawn"
					entry.ParamsRaw = map[string]any{"task": "inspect code", "model": "example-provider/example-model"}
					entry.Result = `{"result":"inspection complete","status":"completed"}`
				}
				if kind == "mixed" && j > 0 {
					entry.ParentID = fmt.Sprintf("tool-%d-0", i)
				}
				block.Add(entry)
			}
		}
		ui.chat.AppendMessage(chatMsg{Role: "assistant", Content: strings.Repeat("Completed **inspection** with unchanged behavior.\n", 8)})
	}
	ui.syncContent()
	ui.syncContent()
	return ui
}

var chatHistoryBenchmarkOutput string

func BenchmarkChatHistoryFrame(b *testing.B) {
	for _, kind := range []string{"text", "tools", "mixed", "tiny"} {
		for _, n := range []int{100, 500, 1000} {
			b.Run(fmt.Sprintf("%s/%d", kind, n), func(b *testing.B) {
				ui := chatHistoryBenchmarkFixture(n, kind)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ui.trimDisplayHistoryIfNeeded()
					ui.syncContent()
					chatHistoryBenchmarkOutput = ui.viewChat()
				}
			})
		}
	}
}
func BenchmarkChatHistoryChangedFrame(b *testing.B) {
	for _, mode := range []string{"stream", "active", "scroll"} {
		for _, turns := range []int{100, 1000} {
			b.Run(fmt.Sprintf("%s/%d", mode, turns), func(b *testing.B) {
				ui := chatHistoryBenchmarkFixture(turns, "mixed")
				if mode == "stream" {
					ui.chat.AppendStreamMessage("assistant", "stream start", time.Now())
				} else if mode == "active" {
					ui.chat.AppendMessage(chatMsg{Role: "user", Content: "current subtask"})
					block := ui.ensureToolBlock()
					block.Add(&toolEntry{ID: "active-root", RawName: "spawn", Name: "Spawn", Intent: "inspect workspace", Status: toolRunning, ParamsRaw: map[string]any{"task": "inspect code"}})
					block.Add(&toolEntry{ID: "active-child", ParentID: "active-root", RawName: "exec", Name: "Exec", Intent: "current tool", Status: toolRunning})
				}
				ui.syncContent()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if mode == "stream" {
						ui.chat.AppendStreamMessage("assistant", " next delta", time.Now())
					} else if mode == "scroll" {
						if i%20 < 10 {
							ui.chat.ScrollTranscript(-8)
						} else {
							ui.chat.ScrollTranscript(8)
						}
					}
					ui.trimDisplayHistoryIfNeeded()
					ui.syncContent()
					chatHistoryBenchmarkOutput = ui.viewChat()
				}
			})
		}
	}
}

// 主题变化需要重建样式，与稳定帧分开测量，避免把普通增量更新当作切主题性能。
func BenchmarkChatHistoryThemeRefresh(b *testing.B) {
	old := currentTheme
	b.Cleanup(func() { applyThemePalette(old) })
	for _, kind := range []string{"text", "mixed"} {
		for _, turns := range []int{100, 1000} {
			b.Run(fmt.Sprintf("%s/%d", kind, turns), func(b *testing.B) {
				ui := chatHistoryBenchmarkFixture(turns, kind)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					applyThemePalette(currentTheme)
					ui.syncContent()
					chatHistoryBenchmarkOutput = ui.viewChat()
				}
			})
		}
	}
}

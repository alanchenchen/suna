package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

type compactAcceptedMsg struct{ ID, SessionID string }

func compactTerminal(state string) bool {
	return state == "applied" || state == "skipped" || state == "failed" || state == "cancelled"
}

// 确认前只缓存本次操作的少量候选；终态不被同 ID 的迟到进度覆盖。
func (t *TUI) rememberManualCompactEvent(p protocol.CompactState) {
	if p.CompactID == "" {
		return
	}
	for i, previous := range t.manualCompactEvents {
		if previous.CompactID == p.CompactID {
			if !compactTerminal(previous.State) {
				t.manualCompactEvents[i] = p
			}
			return
		}
	}
	if len(t.manualCompactEvents) == 4 {
		drop := -1
		for i, previous := range t.manualCompactEvents {
			if !compactTerminal(previous.State) {
				drop = i
				break
			}
		}
		if drop < 0 {
			if !compactTerminal(p.State) {
				return
			}
			drop = 0
		}
		t.manualCompactEvents = append(t.manualCompactEvents[:drop], t.manualCompactEvents[drop+1:]...)
	}
	t.manualCompactEvents = append(t.manualCompactEvents, p)
}

func (t *TUI) handleCompactStateNotification(p protocol.CompactState) {
	if p.SessionID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if t.manualCompactPending && t.manualCompactID == "" {
		t.rememberManualCompactEvent(p)
		return
	}
	if t.manualCompactPending && t.manualCompactID != "" && p.CompactID != t.manualCompactID {
		return
	}
	if p.CompactID != "" && p.CompactID == t.compactTerminalID && !(t.manualCompactPending && t.manualCompactID == p.CompactID) {
		return
	}
	previousWait := t.compactState.Wait
	t.compactState = p
	t.compactReceivedAt = time.Now()
	manual := t.manualCompactPending && (t.manualCompactID == "" || t.manualCompactID == p.CompactID)
	if manual {
		t.manualCompactID = p.CompactID
	}
	t.chat.Compacting = p.Wait != ""
	if t.chat.Compacting {
		if previousWait != p.Wait || t.compactStartedAt.IsZero() {
			t.compactStartedAt = time.Now().Add(-time.Duration(p.WaitElapsedMs) * time.Millisecond)
		}
	} else if previousWait != "" {
		t.compactStartedAt = time.Time{}
	}
	if !compactTerminal(p.State) {
		return
	}
	t.compactTerminalID = p.CompactID
	if p.State == "applied" && p.Result != nil {
		r := p.Result
		t.applyContextStats(r.AfterTokens, r.ContextWindow)
		label := t.tr("compact.tools_applied")
		if p.Kind == "history" {
			label = t.tr("compact.history_applied")
		}
		text := t.tr("compact.applied", label, fmtTok(r.BeforeTokens-r.AfterTokens))
		if manual {
			text = t.tr("compact.manual_applied", label, fmtTok(r.BeforeTokens), fmtTok(r.AfterTokens), fmtTok(r.ContextWindow), fmtTok(r.BeforeTokens-r.AfterTokens), toolview.FormatCompactDuration(time.Duration(p.WaitElapsedMs)*time.Millisecond))
		}
		// 保存语义文本，复用系统消息的主题与宽度渲染，不把 ANSI 面板固化进历史。
		t.appendNonToolMessage(chatMsg{Role: "system", Content: text})
	} else if manual {
		role := "system"
		text := t.tr("compact.unchanged")
		if p.Reason == "no_source" {
			text = t.tr("compact.no_source")
		}
		if p.State == "failed" {
			role = "error"
			text = t.tr("compact.failed")
			if p.Error != "" {
				text = t.tr("compact.failed_detail", p.Error)
			}
		}
		if p.State == "cancelled" {
			text = t.tr("compact.cancelled")
		}
		t.appendNonToolMessage(chatMsg{Role: role, Content: text})
	}
	if manual {
		t.manualCompactPending = false
		t.manualCompactID = ""
		t.manualCompactEvents = nil
		t.cancelling = false
		t.resetPhase()
	}
}
func (t *TUI) backgroundCompactLabel() string {
	p := t.compactState
	if p.State != "preparing" || p.Wait != "" {
		return ""
	}
	elapsed := time.Duration(p.PrepareElapsedMs)*time.Millisecond + time.Since(t.compactReceivedAt)
	key := "compact.background_tools"
	if p.Kind == "history" {
		key = "compact.background_history"
	}
	return t.tr(key, toolview.FormatCompactDuration(elapsed))
}

// 恢复直接构造既有工具块，不重放执行事件，不触发loading、计时或通知。
func (t *TUI) restoreToolMessage(m protocol.SnapshotMessage) bool {
	if len(m.ObservedTools) > 0 {
		if m.Content != "" {
			t.appendNonToolMessage(chatMsg{Role: "assistant", Content: m.Content})
		}
		block := t.chat.EnsureToolBlock()
		for _, call := range m.ObservedTools {
			// key 仅供当前视图索引；不持久化，也不登记 ActiveTools 或计时。
			id := fmt.Sprintf("history-observation:%d:%d", len(t.chat.Messages), len(block.Order))
			block.Add(&toolview.Entry{ID: id, RawName: call.Name, Name: toolview.DisplayName(call.Name), Intent: call.Intent, Status: restoredToolStatus(call.Status), Compacted: true})
		}
		return true
	}
	if len(m.ToolCalls) > 0 {
		if m.Content != "" {
			t.appendNonToolMessage(chatMsg{Role: "assistant", Content: m.Content})
		}
		block := t.chat.EnsureToolBlock()
		for _, call := range m.ToolCalls {
			params := model.ParseToolCallArguments(call.Arguments)
			status := toolview.StatusUnknown
			// 没有结果的调用仅在会话仍活跃时展示运行中；空闲或未知会话不推断取消。
			switch t.currentSession.Status {
			case protocol.SessionStatusRunning, protocol.SessionStatusWaiting, protocol.SessionStatusCompacting:
				status = toolview.StatusRunning
			}
			entry := &toolview.Entry{ID: call.ID, RawName: call.Name, Name: toolview.DisplayName(call.Name), Intent: call.Intent, ParamsRaw: params, Params: toolview.FormatParams(params), Summary: toolview.ParamSummary(call.Name, params), Status: status}
			block.Add(entry)
		}
		return true
	}
	if m.Role == "tool" {
		entry := t.chat.FindTool(m.ToolCallID)
		if entry != nil {
			// 恢复沿用实时 tool_end 的 16 KiB 与 UTF-8 安全截断规则。
			// 普通恢复不应因 Store 保留较长模型证据而扩大 UI 展示量。
			const liveToolResultBytes = 16 * 1024
			entry.ResultBytes = len(m.Content)
			entry.ResultTruncated = m.Truncated || entry.ResultBytes > liveToolResultBytes
			entry.Result = m.Content
			if entry.ResultBytes > liveToolResultBytes {
				// 单独切片仍会保留整个快照结果的底层字符串。
				entry.Result = strings.Clone(clipHeadBytes(m.Content, liveToolResultBytes))
			}
			// 缺失或未知状态不能推断为成功、失败或取消，也不启动计时。
			entry.Status = restoredToolStatus(m.ToolStatus)
		}
		return true
	}
	return false
}

func restoredToolStatus(status string) toolview.Status {
	switch status {
	case "completed":
		return toolview.StatusDone
	case "failed":
		return toolview.StatusError
	case "cancelled":
		return toolview.StatusCancelled
	default:
		return toolview.StatusUnknown
	}
}

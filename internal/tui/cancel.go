package tui

import (
	"strings"
	"time"
)

func (t *TUI) enterCancelling() {
	t.cancelling = true
	t.currentRunCanControl = false
	t.chat.Loading = true
	t.chat.SetStatusLabel(t.tr("status.cancelling"), time.Now())
	t.chat.MarkActiveToolsCancelling()
	_ = t.syncInputFocus()
}

func (t *TUI) shouldAppendCancelNotice(runID string) bool {
	key := strings.TrimSpace(runID)
	if key == "" {
		key = "__unknown__"
	}
	if t.cancelNoticeRunID == key {
		return false
	}
	t.cancelNoticeRunID = key
	return true
}

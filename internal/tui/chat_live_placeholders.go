package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	// spinnerPlaceholder 是渲染阶段写入 transcript 的等宽占位符。
	// 前半段是终端宽度稳定为 1 的可见字符，后半段是零宽 OSC 标记；viewChat()
	// 最终输出时整体替换为当前 spinner 帧，避免私有区字符在不同终端中造成边框错位。
	spinnerPlaceholder = "⠿\x1b]777;suna-spinner\x07"

	// elapsedPlaceholderText 宽度固定为 5，隐藏 OSC 标记携带动态耗时来源。
	// 这样耗时能随 spinner tick 实时刷新，同时 transcript 内容签名保持稳定。
	elapsedPlaceholderText  = " 0.0s"
	elapsedMarkerPrefix     = "\x1b]777;suna-elapsed:"
	elapsedMarkerSuffix     = "\x07"
	phaseElapsedPayload     = "phase"
	phaseElapsedPlaceholder = elapsedPlaceholderText + elapsedMarkerPrefix + phaseElapsedPayload + elapsedMarkerSuffix
)

func (t *TUI) replaceLiveTranscriptPlaceholders(view string) string {
	if view == "" {
		return view
	}
	view = strings.ReplaceAll(view, spinnerPlaceholder, t.liveSpinnerFrame())
	return t.replaceElapsedPlaceholders(view)
}

func (t *TUI) liveSpinnerFrame() string {
	// Bubble spinner.View() 自带一个尾随空格；transcript 占位符只预留 1 列，
	// 因此这里截成单列 frame，避免最终替换后把圆角边框撑宽。
	return ansi.Truncate(t.chat.Spinner.View(), 1, "")
}

func liveElapsedPlaceholder(startedAt time.Time) string {
	if startedAt.IsZero() {
		return elapsedPlaceholderText + elapsedMarkerPrefix + "0" + elapsedMarkerSuffix
	}
	return elapsedPlaceholderText + elapsedMarkerPrefix + strconv.FormatInt(startedAt.UnixNano(), 10) + elapsedMarkerSuffix
}

func (t *TUI) replaceElapsedPlaceholders(view string) string {
	for {
		start := strings.Index(view, elapsedMarkerPrefix)
		if start < 0 {
			return view
		}
		payloadStart := start + len(elapsedMarkerPrefix)
		payloadEndRel := strings.Index(view[payloadStart:], elapsedMarkerSuffix)
		if payloadEndRel < 0 {
			return view
		}
		payloadEnd := payloadStart + payloadEndRel
		payload := view[payloadStart:payloadEnd]
		if placeholderStart := start - len(elapsedPlaceholderText); placeholderStart >= 0 && view[placeholderStart:start] == elapsedPlaceholderText {
			replacement := t.formatElapsedPayload(payload, true)
			view = view[:placeholderStart] + replacement + view[payloadEnd+len(elapsedMarkerSuffix):]
			continue
		}
		plainText := strings.TrimSpace(elapsedPlaceholderText)
		if placeholderStart := start - len(plainText); placeholderStart >= 0 && view[placeholderStart:start] == plainText {
			replacement := t.formatElapsedPayload(payload, false)
			view = view[:placeholderStart] + replacement + view[payloadEnd+len(elapsedMarkerSuffix):]
			continue
		}
		view = view[:start] + view[payloadEnd+len(elapsedMarkerSuffix):]
	}
}

func (t *TUI) formatElapsedPayload(payload string, padded bool) string {
	var formatted string
	if payload == phaseElapsedPayload {
		formatted = t.formatElapsedSince(t.chat.PhaseStart)
	} else {
		nano, err := strconv.ParseInt(payload, 10, 64)
		if err != nil || nano <= 0 {
			formatted = " 0.0s"
		} else {
			formatted = t.formatElapsedSince(time.Unix(0, nano))
		}
	}
	if !padded {
		return strings.TrimSpace(formatted)
	}
	return formatted
}

func (t *TUI) formatElapsedSince(startedAt time.Time) string {
	if startedAt.IsZero() {
		return " 0.0s"
	}
	return formatElapsedSeconds(time.Since(startedAt).Seconds())
}

func formatElapsedSeconds(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	if seconds < 100 {
		// live elapsed 必须始终保持 5 列；这里截断到 0.1s，避免 99.95s 四舍五入成 100.0s。
		tenths := int(seconds * 10)
		return fmt.Sprintf("%4.1fs", float64(tenths)/10)
	}
	if seconds < 1000 {
		return fmt.Sprintf("%4ds", int(seconds))
	}
	return "999+s"
}

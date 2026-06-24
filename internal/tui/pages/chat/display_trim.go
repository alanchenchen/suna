package chat

import (
	"fmt"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

const displayTrimLowWatermarkNumerator = 4
const displayTrimLowWatermarkDenominator = 5

// DisplayDiscardSummary 记录 TUI 展示层被释放的早期历史；它不是模型上下文，也不持久化。
type DisplayDiscardSummary struct {
	Messages int
	Turns    int
	Bytes    int
}

func (m DisplayDiscardSummary) Empty() bool {
	return m.Messages <= 0 && m.Turns <= 0 && m.Bytes <= 0
}

func (m DisplayDiscardSummary) ApproxMB() string {
	if m.Bytes <= 0 {
		return "0MB"
	}
	mb := float64(m.Bytes) / 1024 / 1024
	if mb < 1 {
		return fmt.Sprintf("%.1fMB", mb)
	}
	return fmt.Sprintf("%.0fMB", mb)
}

// TrimDisplayHistory 按完整 turn 从顶部释放 TUI 展示历史，保证裁剪后第一条真实消息仍是 user。
func (m *Model) TrimDisplayHistory(limitBytes int) bool {
	if m == nil || limitBytes <= 0 || len(m.Messages) == 0 {
		return false
	}
	used := m.EstimatedDisplayBytes()
	if used <= limitBytes {
		return false
	}
	target := limitBytes * displayTrimLowWatermarkNumerator / displayTrimLowWatermarkDenominator
	if target <= 0 || target >= limitBytes {
		target = limitBytes
	}
	cutoff := 0
	discarded := DisplayDiscardSummary{}
	for used > target {
		next := nextUserMessageIndex(m.Messages, cutoff+1)
		if next < 0 {
			break
		}
		for i := cutoff; i < next; i++ {
			discarded.Messages++
			discarded.Bytes += estimateMsgDisplayBytes(&m.Messages[i])
			if m.Messages[i].Role == "user" {
				discarded.Turns++
			}
		}
		used -= estimateMessagesDisplayBytes(m.Messages[cutoff:next])
		cutoff = next
	}
	if cutoff <= 0 {
		return false
	}
	// 先清空被裁消息，断开大字符串/render cache/tool block 引用，再复制保留区，避免旧 backing array 滞留。
	for i := 0; i < cutoff; i++ {
		m.Messages[i] = Msg{}
	}
	kept := append([]Msg(nil), m.Messages[cutoff:]...)
	m.Messages = kept
	m.DisplayDiscard.Messages += discarded.Messages
	m.DisplayDiscard.Turns += discarded.Turns
	m.DisplayDiscard.Bytes += discarded.Bytes
	m.TranscriptBlocks = nil
	m.TranscriptWindowSignature = transcriptWindowSignature{}
	m.ClearResponseNav()
	return true
}

func nextUserMessageIndex(msgs []Msg, start int) int {
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			return i
		}
	}
	return -1
}

func (m Model) EstimatedDisplayBytes() int {
	return estimateMessagesDisplayBytes(m.Messages)
}

func estimateMessagesDisplayBytes(msgs []Msg) int {
	total := 0
	for i := range msgs {
		total += estimateMsgDisplayBytes(&msgs[i])
	}
	return total
}

func estimateMsgDisplayBytes(msg *Msg) int {
	if msg == nil {
		return 0
	}
	// 固定开销粗估，避免小消息预算为 0；只用于 TUI 展示层软上限，不追求精确 RSS。
	total := 256 + len(msg.Role)
	total += estimateContentBytes(msg.Content)
	if msg.Stream != nil {
		total += msg.Stream.Raw.Len()
		for _, line := range msg.Stream.Lines {
			total += len(line)
		}
		for _, pending := range msg.Stream.Pending {
			total += len(pending)
		}
	}
	total += len(msg.Render.Output)
	for _, line := range msg.Render.StreamLines {
		total += len(line)
	}
	return total
}

func estimateContentBytes(v any) int {
	switch c := v.(type) {
	case string:
		return len(c)
	case UserMessageContent:
		total := len(c.Text)
		for _, a := range c.Attachments {
			total += len(a.Name) + len(a.Path) + len(a.Type) + 128
		}
		return total
	case *toolview.Block:
		return estimateToolBlockBytes(c)
	default:
		return 512
	}
}

func estimateToolBlockBytes(block *toolview.Block) int {
	if block == nil {
		return 0
	}
	total := len(block.Order) * 64
	for _, entry := range block.Entries {
		if entry == nil {
			continue
		}
		total += len(entry.ID) + len(entry.LocalID) + len(entry.ParentID) + len(entry.Name) + len(entry.RawName)
		total += len(entry.Intent) + len(entry.Params) + len(entry.Summary) + len(entry.Result)
		if entry.Guard != nil {
			total += len(entry.Guard.Risk) + len(entry.Guard.Decision) + len(entry.Guard.Source)
			total += len(entry.Guard.Reason) + len(entry.Guard.Suggestion) + len(entry.Guard.ReviewCode) + len(entry.Guard.ReviewMessage)
		}
		for k, v := range entry.Metadata {
			total += len(k) + estimateContentBytes(v)
		}
	}
	return total
}

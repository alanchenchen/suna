package chat

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/attachment"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

type Phase int

const (
	PhaseIdle Phase = iota
	PhaseFirstLLM
	PhaseLLM
	PhaseThinking
	PhaseTool
	PhaseWaitingAfterTool
)

// Model 持有 Chat 页面运行态。迁移期间 daemon 命令和样式仍由 root TUI 注入。

type StreamingTextState struct {
	Raw             strings.Builder
	Pending         []string
	RenderedBytes   int
	Width           int
	Lines           []string
	DroppedLines    int
	LastLineWidth   int
	PendingNewlines int
}

func (s *StreamingTextState) Append(chunk string) {
	if s == nil || chunk == "" {
		return
	}
	s.Raw.WriteString(chunk)
	s.Pending = append(s.Pending, chunk)
}

func (s *StreamingTextState) Text() string {
	if s == nil {
		return ""
	}
	return s.Raw.String()
}

type Msg struct {
	Role      string
	Content   any
	Streaming bool
	StartedAt time.Time
	EndedAt   time.Time
	Render    MsgRenderCache
	Stream    *StreamingTextState
}

type MsgRenderCache struct {
	Width       int
	Theme       string
	ContentLen  int
	ContentHash uint64
	LineCount   int
	Output      string
	Mode        string

	// 流式文本只会追加。这里缓存已换行的行，避免每个 delta 都重新 wrap 完整回复。
	StreamLines           []string
	StreamLastLineWidth   int
	StreamPendingNewlines int
}

type UserMessageContent struct {
	Text        string
	Attachments []attachment.Item
}

type GuardConfirmView struct {
	ID            string
	ToolCallID    string
	Tool          string
	Params        map[string]any
	Risk          string
	Reason        string
	Suggestion    string
	ReviewCode    string
	ReviewMessage string
}

type Model struct {
	Viewport viewport.Model
	Textarea textarea.Model
	Spinner  spinner.Model

	TranscriptBlocks          []transcriptBlock
	TranscriptYOffset         int
	TranscriptTotalLines      int
	TranscriptWindowStart     int
	TranscriptWindowEnd       int
	TranscriptWindowSignature transcriptWindowSignature

	Messages          []Msg
	DisplayDiscard    DisplayDiscardSummary
	PendingInput      string
	LastAssistantText string
	Loading           bool
	Compacting        bool
	ResumeAvailable   bool
	Phase             Phase
	PhaseStart        time.Time
	StatusLabel       string
	StreamStart       time.Time
	FollowBottom      bool
	ForceBottom       bool

	LastAssistantStartLine int
	LastAssistantLineCount int
	LastAssistantMsgIndex  int
	ResponseNavAvailable   bool
	ResponseNavJumped      bool
	ResponseNavDismissed   bool
	LastWaitingTool        string

	ActiveInteraction *Interaction
	InteractionQueue  []Interaction
	GuardCursor       int
	GuardScroll       int
	CmdSuggestion     string
	CmdSuggestions    []CommandSpec
	CmdSuggestionIdx  int
	ModelPickerOpen   bool
	ModelPickerCursor int

	ShowToolDetail      bool
	ShowReasoningDetail bool
	ToolDetailScroll    int
	SelectedToolID      string

	SubtaskCursor             int
	SubtaskCursorUserSet      bool
	SubtaskToolCursor         int
	SubtaskToolCursorUserSet  bool
	SubtaskToolDetailExpanded bool
	SubtaskToolDetailScroll   int

	ActiveTools      map[string]*toolview.Entry
	ToolStartTimes   map[string]time.Time
	CurrentToolBlock *toolview.Block

	Attachments      []attachment.Item
	AttachmentMode   bool
	AttachmentCursor int
	AttachmentDelete bool

	Skills            []protocol.SkillInfo
	SkillsOverlayOpen bool
	SkillsLoading     bool
	SkillsCursor      int
	SkillsScroll      int
	SkillsError       string

	MCPServers      []protocol.MCPServerInfo
	MCPOverlayOpen  bool
	MCPLoading      bool
	MCPCursor       int
	MCPScroll       int
	MCPError        string
	MCPActionServer string

	Memories          []protocol.MemoryItem
	MemoryOverlayOpen bool
	MemoryLoading     bool
	MemoryCursor      int
	MemoryScroll      int
	MemoryError       string
	MemoryConfirm     MemoryConfirmMode
	MemoryConfirmText string

	Sessions            []protocol.SessionInfo
	SessionsOverlayOpen bool
	SessionsLoading     bool
	SessionCursor       int
	SessionsError       string
	SessionConfirm      SessionConfirmMode
}

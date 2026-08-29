package chat

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/attachment"
	"github.com/alanchenchen/suna/internal/tui/components/overlaylist"
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

type SkillLoadView struct {
	ID        string
	Name      string
	Status    string
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
	Error     bool
}

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
	ID        uint64
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
	ReadOnly      bool
	Reason        string
	ReviewCode    string
	ReviewMessage string
}

type SteeringSubmission struct {
	ClientMsgID string
	Text        string
	Resolved    bool
	Failed      bool
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

	// SelectionStart/SelectionEnd 是内容行选区范围（含端点），-1 表示无选区。
	// 内容层应用选区样式（strip ANSI + 反色），避免行内 markdown 背景色覆盖选区背景。
	SelectionStart int
	SelectionEnd   int
	SelectionStyle lipgloss.Style

	Messages              []Msg
	DisplayDiscard        DisplayDiscardSummary
	PendingInput          string
	InputHistoryIndex     int
	InputHistoryDraft     string
	InputHistoryActive    bool
	LastAssistantText     string
	Loading               bool
	Compacting            bool
	ResumeAvailable       bool
	PendingSteering       []protocol.SteeringMessage
	SteeringSubmissions   []SteeringSubmission
	SteeringTerminal      map[string]protocol.SteeringState
	Phase                 Phase
	PhaseStart            time.Time
	StatusLabel           string
	StreamStart           time.Time
	FollowBottom          bool
	ForceBottom           bool
	ManualScrollPaused    bool
	NewContentWhilePaused bool

	LastAssistantStartLine int
	LastAssistantLineCount int
	LastAssistantMsgIndex  int
	ResponseNavAvailable   bool
	LastWaitingTool        string

	ActiveInteraction *Interaction
	InteractionQueue  []Interaction
	GuardCursor       int
	GuardScroll       int
	CmdSuggestion     string
	CmdSuggestions    []CommandSpec
	CmdSuggestionIdx  int
	ModelPickerOpen   bool
	ModelList         overlaylist.Model

	ShowToolDetail   bool
	ToolDetailScroll int
	SelectedToolID   string

	ExpandedReasoningID uint64
	NextMessageID       uint64

	SubtaskCursor             int
	SubtaskCursorUserSet      bool
	SubtaskToolCursor         int
	SubtaskToolCursorUserSet  bool
	SubtaskToolDetailExpanded bool
	SubtaskToolDetailScroll   int

	ActiveTools            map[string]*toolview.Entry
	ToolStartTimes         map[string]time.Time
	CurrentToolBlock       *toolview.Block
	CloseToolBlockWhenIdle bool

	Attachments      []attachment.Item
	AttachmentMode   bool
	AttachmentCursor int
	AttachmentDelete bool

	Skills            []protocol.SkillInfo
	SkillsList        overlaylist.Model
	SkillsOverlayOpen bool
	SkillsLoading     bool
	SkillsError       string

	MCPServers      []protocol.MCPServerInfo
	MCPList         overlaylist.Model
	MCPOverlayOpen  bool
	MCPLoading      bool
	MCPError        string
	MCPActionServer string

	Memories          []protocol.MemoryItem
	MemoryOverlayOpen bool
	MemoryLoading     bool
	MemoryCursor      int
	MemoryScroll      int
	MemoryError       string
	MemoryConfirm     MemoryConfirmMode

	Sessions            []protocol.SessionInfo
	SessionsOverlayOpen bool
	SessionsLoading     bool
	SessionCursor       int
	SessionsError       string
	SessionConfirm      SessionConfirmMode
	SessionConfirmID    string
	SessionRowKinds     []SessionRowKind

	AttachmentsOverlayOpen bool
	AttachmentsConfirm     bool
}

// HasOverlayOpen 表示是否有任何列表 overlay 打开（model picker / skills / mcp /
// memory / sessions / attachments）。内容区鼠标选区在这些 overlay 打开时不应生效，
// 避免拖动误触发选区遮挡面板交互。
func (m Model) HasOverlayOpen() bool {
	return m.ModelPickerOpen || m.SkillsOverlayOpen || m.MCPOverlayOpen ||
		m.MemoryOverlayOpen || m.SessionsOverlayOpen || m.AttachmentsOverlayOpen
}

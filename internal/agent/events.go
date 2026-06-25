package agent

import "github.com/alanchenchen/suna/internal/model"

const eventBuffer = 2048

type EventType int

const (
	EventStream EventType = iota
	EventReasoning
	EventUsage
	EventToolCall
	EventToolResult
	EventStatus
	EventAskUser
	EventGuardConfirm
	EventToolGuard
	EventSkillLoad
	EventSkillReview
)

type EventStatusKind string

const (
	StatusCompactRunning EventStatusKind = "compact_running"
	StatusCompactDone    EventStatusKind = "compact_done"
	StatusCompactError   EventStatusKind = "compact_error"
	StatusWaitingLLM     EventStatusKind = "waiting_llm"
	StatusLLMRetrying    EventStatusKind = "llm_retrying"
	StatusDone           EventStatusKind = "done"
)

type Event struct {
	Type EventType

	Content string
	Error   bool
	Status  EventStatusKind

	ToolName   string
	ToolCallID string
	ToolParams map[string]any
	ToolIntent string

	ToolResult   string
	ToolError    bool
	ToolMetadata map[string]any

	SkillName         string
	SkillLoadStatus   string
	SkillReview       string
	SkillReviewStatus string

	Question    string
	Options     []string
	AllowCustom bool
	Reply       chan string

	GuardToolCallID string
	GuardTool       string
	GuardParams     map[string]any
	GuardRisk       string
	GuardDecision   string
	GuardSource     string
	GuardReason     string
	GuardSuggestion string
	GuardReviewCode string
	GuardReviewMsg  string

	InputTokens            int
	OutputTokens           int
	CachedTokens           int
	ContextTokens          int
	EstimatedContextTokens int
	ContextWindow          int
	DurationMs             int64

	ResumeAvailable bool
	Attempt         int
	MaxAttempts     int
	DelayMs         int64
	ModelError      *model.ModelError
}

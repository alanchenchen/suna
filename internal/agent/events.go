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
	EventSteering
	EventAssistantSaved
	EventUserSaved
)

type EventStatusKind string

const (
	StatusWaitingLLM  EventStatusKind = "waiting_llm"
	StatusLLMRetrying EventStatusKind = "llm_retrying"
	StatusDone        EventStatusKind = "done"
)

type Event struct {
	UserInput *Input
	Type      EventType

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

	Steering *SteeringItem

	Question    string
	Options     []string
	AllowCustom bool
	Reply       chan string

	GuardToolCallID string
	GuardTool       string
	GuardParams     map[string]any
	GuardReadOnly   bool
	GuardDecision   string
	GuardSource     string
	GuardReason     string
	GuardReviewCode string
	GuardReviewMsg  string

	InputTokens            int
	OutputTokens           int
	CacheReadTokens        int
	CacheCreationTokens    int
	ContextTokens          int
	EstimatedContextTokens int
	ContextWindow          int
	DurationMs             int64

	ResumeAvailable bool
	Attempt         int
	DelayMs         int64
	ElapsedMs       int64
	BudgetMs        int64
	ModelError      *model.ModelError
	RunError        *RunError
}

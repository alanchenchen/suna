package agent

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

type Event struct {
	Type EventType

	Content string

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

	InputTokens   int
	OutputTokens  int
	CachedTokens  int
	ContextTokens int
	ContextWindow int
	DurationMs    int64
}

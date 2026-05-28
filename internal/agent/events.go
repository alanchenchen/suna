package agent

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

	Question string
	Options  []string
	Reply    chan string

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

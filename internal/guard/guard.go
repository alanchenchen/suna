package guard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type Decision string

const (
	Approve Decision = "approve"
	Reject  Decision = "reject"
	Confirm Decision = "confirm"
	Modify  Decision = "modify"
)

type Mode string

const (
	ModeReadonly Mode = "readonly"
	ModeAsk      Mode = "ask"
	ModeAuto     Mode = "auto"
	ModeSmart    Mode = "smart"
)

// ReviewContext 是 smart mode 下 LLM review 的轻量意图上下文。
// 它随单次 tool call 传入，避免全局 recent context 在并发工具调用时串线。
type ReviewContext struct {
	UserRequest      string
	ToolIntent       string
	AssistantContext string
	RecentContext    string
}

type ReviewRequest struct {
	ToolName   string
	ParamsJSON string
	Target     string
	Risk       string
	Context    ReviewContext
}

// LLMReviewer 用于 Guard Stage 3 LLM 审查。接收结构化操作上下文，返回 LLM 原始回复。
type LLMReviewer func(ctx context.Context, req ReviewRequest) (string, error)

type GuardResult struct {
	Decision      Decision
	Reason        string
	Risk          RiskLevel
	Suggestion    string
	Source        string
	Audit         string
	ReviewCode    string
	ReviewMessage string
}

type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
)

type Guard struct {
	db           *sql.DB
	mode         Mode
	blockedRules []blockRule
	userBlocked  []blockRule
	userAllowed  []allowedRule
	allowedCmds  []string
	workspace    string
	sessionID    string
	llmReviewer  LLMReviewer
}

type blockRule struct {
	pattern *regexp.Regexp
	reason  string
}

type allowedRule struct {
	pattern *regexp.Regexp
	tool    string
	reason  string
}

func NewGuard(db *sql.DB, sessionID string) *Guard {
	return NewGuardWithMode(db, sessionID, ModeAsk)
}

func NewGuardWithMode(db *sql.DB, sessionID string, mode Mode) *Guard {
	g := &Guard{db: db, sessionID: sessionID, mode: NormalizeMode(string(mode))}
	g.blockedRules = g.builtinBlockedRules()
	g.allowedCmds = g.builtinAllowedCommands()
	return g
}

func NewGuardWithConfig(db *sql.DB, sessionID string, blockedPatterns []string, blockedReasons []string, allowedPatterns []string, allowedTools []string) *Guard {
	return NewGuardWithConfigAndMode(db, sessionID, ModeAsk, blockedPatterns, blockedReasons, allowedPatterns, allowedTools)
}

func NewGuardWithConfigAndMode(db *sql.DB, sessionID string, mode Mode, blockedPatterns []string, blockedReasons []string, allowedPatterns []string, allowedTools []string) *Guard {
	return NewGuardWithConfigModeAndWorkspace(db, sessionID, mode, "", blockedPatterns, blockedReasons, allowedPatterns, allowedTools)
}

func NewGuardWithConfigModeAndWorkspace(db *sql.DB, sessionID string, mode Mode, workspace string, blockedPatterns []string, blockedReasons []string, allowedPatterns []string, allowedTools []string) *Guard {
	g := &Guard{db: db, sessionID: sessionID, mode: NormalizeMode(string(mode))}
	g.blockedRules = g.builtinBlockedRules()
	g.allowedCmds = g.builtinAllowedCommands()
	g.workspace = normalizeWorkspaceRoot(workspace)
	for i, p := range blockedPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		reason := ""
		if i < len(blockedReasons) {
			reason = blockedReasons[i]
		}
		g.userBlocked = append(g.userBlocked, blockRule{pattern: re, reason: reason})
	}
	for i, p := range allowedPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		tool := ""
		if i < len(allowedTools) {
			tool = allowedTools[i]
		}
		g.userAllowed = append(g.userAllowed, allowedRule{pattern: re, tool: tool, reason: ""})
	}
	return g
}

func NormalizeMode(mode string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(mode))) {
	case ModeReadonly:
		return ModeReadonly
	case ModeAuto:
		return ModeAuto
	case ModeSmart:
		return ModeSmart
	default:
		return ModeAsk
	}
}

func (g *Guard) Mode() Mode {
	if g == nil || g.mode == "" {
		return ModeAsk
	}
	return g.mode
}

func (g *Guard) Workspace() string {
	if g == nil {
		return ""
	}
	return g.workspace
}

// SetLLMReviewer 注入 LLM 审查函数，由 Agent 在创建 Guard 后调用。
func (g *Guard) SetLLMReviewer(reviewer LLMReviewer) {
	g.llmReviewer = reviewer
}

func (g *Guard) Check(ctx context.Context, tool string, params map[string]any, reviewCtx ...ReviewContext) *GuardResult {
	risk := g.assessRisk(tool, params)

	if blocked, reason := g.checkWorkspace(tool, params); blocked {
		g.audit(ctx, tool, params, risk, "workspace_reject", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Risk: risk, Source: "rule", Audit: "workspace_reject"}
	}
	if blocked, reason := g.checkBlocked(tool, params); blocked {
		g.audit(ctx, tool, params, risk, "blocked", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Risk: risk, Source: "rule", Audit: "blocked"}
	}
	if allowed, reason := g.checkAllowed(tool, params); allowed {
		if reason == "" {
			reason = "allowed rule"
		}
		g.audit(ctx, tool, params, risk, "allowed", reason)
		return &GuardResult{Decision: Approve, Reason: reason, Risk: risk, Source: "rule", Audit: "allowed"}
	}

	if g.Mode() == ModeReadonly {
		if risk == RiskLow && isReadOnlyCall(tool, params) {
			g.audit(ctx, tool, params, risk, "auto_approve", "readonly low risk")
			return &GuardResult{Decision: Approve, Reason: "readonly low risk", Risk: risk, Source: "static", Audit: "auto_approve"}
		}
		g.audit(ctx, tool, params, risk, "readonly_reject", "readonly mode blocks this operation")
		return &GuardResult{Decision: Reject, Reason: "readonly mode blocks this operation", Risk: risk, Source: "static", Audit: "readonly_reject"}
	}
	if risk == RiskLow {
		g.audit(ctx, tool, params, risk, "auto_approve", "low_risk")
		return &GuardResult{Decision: Approve, Reason: "low risk", Risk: risk, Source: "static", Audit: "auto_approve"}
	}
	if g.Mode() == ModeAuto {
		g.audit(ctx, tool, params, risk, "auto_approve", fmt.Sprintf("auto mode risk=%s", RiskString(risk)))
		return &GuardResult{Decision: Approve, Reason: "auto mode", Risk: risk, Source: "static", Audit: "auto_approve"}
	}
	if g.Mode() == ModeAsk {
		g.audit(ctx, tool, params, risk, "confirm", fmt.Sprintf("ask mode risk=%s", RiskString(risk)))
		return &GuardResult{Decision: Confirm, Reason: "confirm risky operation", Risk: risk, Source: "user", Audit: "confirm"}
	}

	// smart mode: medium/high 由 LLM 结合任务意图判断；失败或不确定才转人工确认。
	if g.llmReviewer == nil {
		return g.reviewFallback(ctx, tool, params, risk, "review_unavailable", "Smart Guard reviewer is unavailable")
	}
	ctxForReview := ReviewContext{}
	if len(reviewCtx) > 0 {
		ctxForReview = reviewCtx[0]
	}
	return g.llmReview(ctx, tool, params, risk, ctxForReview)
}

// llmReview 调用 LLM 进行安全审查。LLM 可以 approve/reject/confirm/modify。
func (g *Guard) llmReview(ctx context.Context, toolName string, params map[string]any, risk RiskLevel, reviewCtx ReviewContext) *GuardResult {
	target := guardTarget(toolName, params)
	paramsJSON, _ := marshalParams(params)
	resp, err := g.llmReviewer(ctx, ReviewRequest{ToolName: toolName, ParamsJSON: paramsJSON, Target: target, Risk: RiskString(risk), Context: reviewCtx})
	if err != nil {
		code, msg := classifyReviewError(err)
		return g.reviewFallback(ctx, toolName, params, risk, code, msg)
	}
	jsonText := extractJSON(resp)
	if strings.TrimSpace(jsonText) == "" {
		return g.reviewFallback(ctx, toolName, params, risk, "review_empty_response", "Smart Guard review returned an empty response")
	}
	var decision struct {
		Decision   string `json:"decision"`
		Reason     string `json:"reason"`
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(jsonText), &decision); err != nil {
		return g.reviewFallback(ctx, toolName, params, risk, "review_parse_failed", "Smart Guard review returned invalid JSON")
	}
	decision.Decision = strings.ToLower(strings.TrimSpace(decision.Decision))
	switch decision.Decision {
	case "reject":
		g.audit(ctx, toolName, params, risk, "llm_reject", decision.Reason)
		return &GuardResult{Decision: Reject, Reason: decision.Reason, Risk: risk, Source: "llm", Audit: "llm_reject"}
	case "confirm":
		g.audit(ctx, toolName, params, risk, "llm_confirm", decision.Reason)
		return &GuardResult{Decision: Confirm, Reason: decision.Reason, Risk: risk, Suggestion: decision.Suggestion, Source: "llm", Audit: "llm_confirm"}
	case "modify":
		g.audit(ctx, toolName, params, risk, "llm_modify", decision.Reason)
		return &GuardResult{Decision: Modify, Reason: decision.Reason, Risk: risk, Suggestion: decision.Suggestion, Source: "llm", Audit: "llm_modify"}
	case "approve":
		g.audit(ctx, toolName, params, risk, "llm_approve", decision.Reason)
		return &GuardResult{Decision: Approve, Reason: decision.Reason, Risk: risk, Source: "llm", Audit: "llm_approve"}
	default:
		return g.reviewFallback(ctx, toolName, params, risk, "review_invalid_decision", "Smart Guard review returned an invalid decision")
	}
}

func (g *Guard) reviewFallback(ctx context.Context, tool string, params map[string]any, risk RiskLevel, code, message string) *GuardResult {
	if strings.TrimSpace(code) == "" {
		code = "review_unavailable"
	}
	if strings.TrimSpace(message) == "" {
		message = "Smart Guard review failed"
	}
	g.audit(ctx, tool, params, risk, code, message)
	return &GuardResult{Decision: Confirm, Reason: message, Risk: risk, Source: "fallback", Audit: code, ReviewCode: code, ReviewMessage: message}
}

func classifyReviewError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "review_timeout", "Smart Guard review timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "review_canceled", "Smart Guard review was canceled"
	}
	return "review_provider_error", "Smart Guard review request failed"
}

// extractJSON 从 LLM 回复中提取 JSON 对象。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func (g *Guard) checkBlocked(tool string, params map[string]any) (bool, string) {
	target := guardTarget(tool, params)
	if target == "" {
		return false, ""
	}
	for _, rule := range g.blockedRules {
		if rule.pattern.MatchString(target) {
			return true, rule.reason
		}
	}
	for _, rule := range g.userBlocked {
		if rule.pattern.MatchString(target) {
			return true, rule.reason
		}
	}
	return false, ""
}

func (g *Guard) checkAllowed(tool string, params map[string]any) (bool, string) {
	target := guardTarget(tool, params)
	if target == "" {
		return false, ""
	}
	for _, rule := range g.userAllowed {
		if rule.tool != "" && rule.tool != tool {
			continue
		}
		if rule.pattern.MatchString(target) {
			return true, rule.reason
		}
	}
	return false, ""
}

func guardTarget(tool string, params map[string]any) string {
	switch tool {
	case "exec":
		target, _ := params["command"].(string)
		return target
	case "writefile", "editfile", "readfile", "listdir", "search":
		target, _ := params["path"].(string)
		return target
	case "filesystem":
		action, _ := params["action"].(string)
		path, _ := params["path"].(string)
		dst, _ := params["destination"].(string)
		if dst != "" {
			return fmt.Sprintf("%s %s -> %s", action, path, dst)
		}
		return fmt.Sprintf("%s %s", action, path)
	case "http":
		method, _ := params["method"].(string)
		if method == "" {
			method = "GET"
		}
		target, _ := params["url"].(string)
		return strings.ToUpper(method) + " " + target
	default:
		return ""
	}
}

func (g *Guard) audit(ctx context.Context, tool string, params map[string]any, risk RiskLevel, decision, reason string) {
	if g.db == nil {
		return
	}
	id := uuid.New().String()
	riskStr := "low"
	if risk == RiskMedium {
		riskStr = "medium"
	} else if risk == RiskHigh {
		riskStr = "high"
	}
	paramsStr := "{}"
	if b, err := marshalParams(params); err == nil {
		paramsStr = b
	}
	g.db.ExecContext(ctx, `
		INSERT INTO audit_log (id, session_id, tool, params, risk_level, guard_decision, guard_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, g.sessionID, tool, paramsStr, riskStr, decision, reason,
	)
}

func (g *Guard) builtinBlockedRules() []blockRule {
	rules := platformBlockedRules()
	genericRules := []struct{ pattern, reason string }{
		{`(?i)\b(curl|wget|iwr|irm|invoke-webrequest|invoke-restmethod)\b.*\|\s*(sh|bash|zsh|fish|iex|invoke-expression|powershell|pwsh)\b`, "blocked: remote script pipe execution"},
		{`(?i)\beval\s*\$\(`, "blocked: command injection pattern"},
	}
	for _, r := range genericRules {
		if re, err := regexp.Compile(r.pattern); err == nil {
			rules = append(rules, blockRule{pattern: re, reason: r.reason})
		}
	}
	return rules
}

func (g *Guard) builtinAllowedCommands() []string {
	return platformReadOnlyCommands()
}

func (g *Guard) assessRisk(tool string, params map[string]any) RiskLevel {
	switch tool {
	case "exec":
		cmd, _ := params["command"].(string)
		shell, _ := params["shell"].(string)
		return analyzeExecCommand(cmd, shell, g.allowedCmds)
	case "writefile":
		path, _ := params["path"].(string)
		return assessFileWriteRisk(path)
	case "editfile":
		path, _ := params["path"].(string)
		if isHighRiskFilePath(path) {
			return RiskHigh
		}
		return RiskMedium
	case "filesystem":
		return assessFilesystemRisk(params)
	case "http":
		return assessHTTPRisk(params)
	case "readfile", "listdir":
		return RiskLow
	case "search":
		return assessSearchRisk(params)
	default:
		// Unknown Act tools are never safe-by-default. New external tools must be explicitly classified.
		return RiskMedium
	}
}

func RiskString(risk RiskLevel) string {
	switch risk {
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "low"
	}
}

func isReadOnlyCall(tool string, params map[string]any) bool {
	switch tool {
	case "readfile", "listdir", "search", "exec":
		return true
	case "filesystem":
		action, _ := params["action"].(string)
		return action == "stat"
	case "http":
		method, _ := params["method"].(string)
		method = strings.ToUpper(strings.TrimSpace(method))
		return method == "" || method == "GET" || method == "HEAD"
	default:
		return false
	}
}

func compileRegexps(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

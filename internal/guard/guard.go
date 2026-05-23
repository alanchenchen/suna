package guard

import (
	"context"
	"database/sql"
	"encoding/json"
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

// LLMReviewer 用于 Guard Stage 3 LLM 审查。
// 接收操作上下文，返回 LLM 原始回复。
type LLMReviewer func(ctx context.Context, toolName string, paramsJSON string, target string, recentCtx string) (string, error)

type GuardResult struct {
	Decision   Decision
	Reason     string
	Risk       RiskLevel
	Suggestion string
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
	recentCtx    []string
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
	g := &Guard{
		db:        db,
		sessionID: sessionID,
		mode:      NormalizeMode(string(mode)),
	}
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
	g := &Guard{
		db:        db,
		sessionID: sessionID,
		mode:      NormalizeMode(string(mode)),
	}
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

// SetLLMReviewer 注入 LLM 审查函数，由 Agent 在创建 Guard 后调用
func (g *Guard) SetLLMReviewer(reviewer LLMReviewer) {
	g.llmReviewer = reviewer
}

// SetRecentContext 设置最近对话上下文，供 LLM 审查使用
func (g *Guard) SetRecentContext(messages []string) {
	g.recentCtx = messages
}

func (g *Guard) Check(ctx context.Context, tool string, params map[string]any) *GuardResult {
	risk := g.assessRisk(tool, params)

	if blocked, reason := g.checkWorkspace(tool, params); blocked {
		g.audit(ctx, tool, params, risk, "workspace_reject", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Risk: risk}
	}

	if blocked, reason := g.checkBlocked(tool, params); blocked {
		g.audit(ctx, tool, params, risk, "blocked", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Risk: risk}
	}
	if allowed, reason := g.checkAllowed(tool, params); allowed {
		if reason == "" {
			reason = "allowed rule"
		}
		g.audit(ctx, tool, params, risk, "allowed", reason)
		return &GuardResult{Decision: Approve, Reason: reason, Risk: risk}
	}

	if g.Mode() == ModeReadonly {
		if risk == RiskLow && isReadOnlyTool(tool) {
			g.audit(ctx, tool, params, risk, "auto_approve", "readonly low risk")
			return &GuardResult{Decision: Approve, Reason: "readonly low risk", Risk: risk}
		}
		g.audit(ctx, tool, params, risk, "readonly_reject", "readonly mode blocks this operation")
		return &GuardResult{Decision: Reject, Reason: "readonly mode blocks this operation", Risk: risk}
	}

	if risk == RiskLow {
		g.audit(ctx, tool, params, risk, "auto_approve", "low_risk")
		return &GuardResult{Decision: Approve, Reason: "low risk", Risk: risk}
	}

	if g.Mode() == ModeAuto {
		g.audit(ctx, tool, params, risk, "auto_approve", fmt.Sprintf("auto mode risk=%s", RiskString(risk)))
		return &GuardResult{Decision: Approve, Reason: "auto mode", Risk: risk}
	}

	if g.Mode() == ModeAsk {
		g.audit(ctx, tool, params, risk, "confirm", fmt.Sprintf("ask mode risk=%s", RiskString(risk)))
		return &GuardResult{Decision: Confirm, Reason: "confirm risky operation", Risk: risk}
	}

	// smart mode: LLM 审查（中高风险），失败或不确定时转用户确认。
	if g.llmReviewer != nil {
		result := g.llmReview(ctx, tool, params, risk)
		if result != nil {
			return result
		}
	}

	g.audit(ctx, tool, params, risk, "confirm", "smart review unavailable or inconclusive")
	return &GuardResult{Decision: Confirm, Reason: "smart review unavailable or inconclusive", Risk: risk}
}

// llmReview 调用 LLM 进行安全审查
func (g *Guard) llmReview(ctx context.Context, toolName string, params map[string]any, risk RiskLevel) *GuardResult {
	var target string
	switch toolName {
	case "exec":
		target, _ = params["command"].(string)
	case "writefile", "editfile":
		target, _ = params["path"].(string)
	case "writehttp":
		target, _ = params["url"].(string)
	}

	paramsJSON, _ := json.Marshal(params)

	recentCtx := ""
	if len(g.recentCtx) > 0 {
		start := 0
		if len(g.recentCtx) > 3 {
			start = len(g.recentCtx) - 3
		}
		recentCtx = strings.Join(g.recentCtx[start:], "\n")
	}

	resp, err := g.llmReviewer(ctx, toolName, string(paramsJSON), target, recentCtx)
	if err != nil {
		return nil
	}

	var decision struct {
		Decision   string `json:"decision"`
		Reason     string `json:"reason"`
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(extractJSON(resp)), &decision); err != nil {
		return nil
	}

	switch decision.Decision {
	case "reject":
		g.audit(ctx, toolName, params, risk, "llm_reject", decision.Reason)
		return &GuardResult{Decision: Reject, Reason: decision.Reason, Risk: risk}
	case "confirm":
		g.audit(ctx, toolName, params, risk, "llm_confirm", decision.Reason)
		return &GuardResult{Decision: Confirm, Reason: decision.Reason, Risk: risk}
	case "modify":
		g.audit(ctx, toolName, params, risk, "llm_modify", decision.Reason)
		return &GuardResult{Decision: Confirm, Reason: decision.Reason, Risk: risk, Suggestion: decision.Suggestion}
	case "approve":
		g.audit(ctx, toolName, params, risk, "llm_approve", decision.Reason)
		return &GuardResult{Decision: Approve, Reason: decision.Reason, Risk: risk}
	default:
		g.audit(ctx, toolName, params, risk, "llm_uncertain", decision.Reason)
		return &GuardResult{Decision: Confirm, Reason: decision.Reason, Risk: risk, Suggestion: decision.Suggestion}
	}
}

// extractJSON 从 LLM 回复中提取 JSON 对象
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
	var target string
	switch tool {
	case "exec":
		target, _ = params["command"].(string)
	case "writefile", "editfile":
		target, _ = params["path"].(string)
	case "writehttp":
		target, _ = params["url"].(string)
	case "readfile", "listdir":
		target, _ = params["path"].(string)
	case "readhttp":
		target, _ = params["url"].(string)
	default:
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
	case "writefile", "editfile", "readfile":
		target, _ := params["path"].(string)
		return target
	case "writehttp", "readhttp":
		target, _ := params["url"].(string)
		return target
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

	genericRules := []struct {
		pattern string
		reason  string
	}{
		{`(?i)\b(curl|wget|iwr|irm|invoke-webrequest|invoke-restmethod)\b.*\|\s*(sh|bash|zsh|fish|iex|invoke-expression|powershell|pwsh)\b`, "blocked: remote script pipe execution"},
		{`(?i)\beval\s*\$\(`, "blocked: command injection pattern"},
	}
	for _, r := range genericRules {
		re, err := regexp.Compile(r.pattern)
		if err == nil {
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
	case "writehttp":
		method, _ := params["method"].(string)
		if strings.EqualFold(method, "DELETE") {
			return RiskHigh
		}
		return RiskMedium
	case "readfile", "listdir", "readhttp":
		return RiskLow
	default:
		// Unknown Act tools are never safe-by-default. New capabilities must be
		// explicitly classified before they can bypass confirmation.
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

func isReadOnlyTool(tool string) bool {
	switch tool {
	case "readfile", "listdir", "readhttp":
		return true
	case "exec":
		return true
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

package guard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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
	blockedRules []blockRule
	userBlocked  []blockRule
	userAllowed  []allowedRule
	allowedCmds  []string
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
	g := &Guard{
		db:        db,
		sessionID: sessionID,
	}
	g.blockedRules = g.builtinBlockedRules()
	g.allowedCmds = g.builtinAllowedCommands()
	return g
}

func NewGuardWithConfig(db *sql.DB, sessionID string, blockedPatterns []string, blockedReasons []string, allowedPatterns []string, allowedTools []string) *Guard {
	g := &Guard{
		db:        db,
		sessionID: sessionID,
	}
	g.blockedRules = g.builtinBlockedRules()
	g.allowedCmds = g.builtinAllowedCommands()
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

	if blocked, reason := g.checkBlocked(tool, params); blocked {
		g.audit(ctx, tool, params, risk, "blocked", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Risk: risk}
	}

	if risk == RiskLow {
		g.audit(ctx, tool, params, risk, "auto_approve", "low_risk")
		return &GuardResult{Decision: Approve, Reason: "low risk", Risk: risk}
	}

	// Stage 3: LLM 审查（中高风险）
	if g.llmReviewer != nil {
		result := g.llmReview(ctx, tool, params, risk)
		if result != nil {
			return result
		}
	}

	// 无 LLM reviewer 或审查失败时，降级为 auto_approve
	g.audit(ctx, tool, params, risk, "auto_approve", fmt.Sprintf("risk=%d phase1_stub", risk))
	return &GuardResult{Decision: Approve, Reason: "phase 1 stub: auto approve", Risk: risk}
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
		return &GuardResult{Decision: Approve, Reason: "llm confirmed: " + decision.Reason, Risk: risk}
	case "modify":
		g.audit(ctx, toolName, params, risk, "llm_modify", decision.Reason)
		return &GuardResult{Decision: Approve, Reason: "llm modified: " + decision.Reason, Risk: risk, Suggestion: decision.Suggestion}
	default:
		g.audit(ctx, toolName, params, risk, "llm_approve", decision.Reason)
		return &GuardResult{Decision: Approve, Reason: decision.Reason, Risk: risk}
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
		{`curl.*\|\s*sh`, "blocked: remote script pipe execution"},
		{`wget.*\|\s*sh`, "blocked: remote script pipe execution"},
		{`eval\s*\$\(`, "blocked: command injection pattern"},
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
		if g.isReadOnlyCommand(cmd) {
			return RiskLow
		}
		cmdLower := strings.ToLower(cmd)
		if containsAny(cmdLower, []string{"rm", "rmdir", "del", "rmdir"}) {
			return RiskHigh
		}
		return RiskMedium
	case "writefile":
		path, _ := params["path"].(string)
		if fileExists(path) {
			return RiskMedium
		}
		return RiskLow
	case "editfile":
		return RiskMedium
	case "writehttp":
		method, _ := params["method"].(string)
		if strings.EqualFold(method, "DELETE") {
			return RiskHigh
		}
		return RiskMedium
	default:
		return RiskLow
	}
}

func (g *Guard) isReadOnlyCommand(cmd string) bool {
	readOnly := platformReadOnlyCommands()
	trimmed := strings.TrimSpace(cmd)
	for _, ro := range readOnly {
		if strings.HasPrefix(trimmed, ro+" ") || trimmed == ro {
			return true
		}
	}
	for _, rule := range g.userAllowed {
		if rule.pattern.MatchString(trimmed) && rule.tool == "exec" {
			return true
		}
	}
	if strings.Contains(cmd, "|") {
		parts := strings.Split(cmd, "|")
		for _, p := range parts {
			if !g.isReadOnlyCommand(strings.TrimSpace(p)) {
				return false
			}
		}
		return true
	}
	return false
}

func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func marshalParams(params map[string]any) (string, error) {
	if params == nil {
		return "{}", nil
	}
	var buf strings.Builder
	buf.WriteByte('{')
	first := true
	for k, v := range params {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteByte('"')
		buf.WriteString(k)
		buf.WriteString(`":`)
		switch val := v.(type) {
		case string:
			buf.WriteByte('"')
			buf.WriteString(val)
			buf.WriteByte('"')
		default:
			buf.WriteString(fmt.Sprintf("%v", v))
		}
	}
	buf.WriteByte('}')
	return buf.String(), nil
}

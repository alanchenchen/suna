package subtask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tools"
)

type Request struct {
	ID       string
	Task     string
	Input    []model.ContentBlock
	Binding  *model.ModelBinding
	System   string
	ToolDefs []model.ToolDef

	MaxTurns     int
	MaxToolCalls int
}

type Status string

const (
	StatusCompleted             Status = "completed"
	StatusCompletedUnstructured Status = "completed_unstructured"
	StatusFailed                Status = "failed"
)

type SideEffectStatus string

const (
	SideEffectsNone      SideEffectStatus = "none"
	SideEffectsCleaned   SideEffectStatus = "cleaned"
	SideEffectsRemaining SideEffectStatus = "remaining"
	SideEffectsUnknown   SideEffectStatus = "unknown"
)

type SideEffects struct {
	Status  SideEffectStatus `json:"status"`
	Summary string           `json:"summary,omitempty"`
	Paths   []string         `json:"paths,omitempty"`
}

type Result struct {
	Status      Status
	Text        string
	Error       string
	SideEffects SideEffects
}

type Subtask struct {
	req Request
}

func New(req Request) *Subtask {
	return &Subtask{req: req}
}

func (s *Subtask) toolDefs() []model.ToolDef {
	if len(s.req.ToolDefs) == 0 {
		return nil
	}
	defs := make([]model.ToolDef, len(s.req.ToolDefs))
	for i, def := range s.req.ToolDefs {
		defs[i] = model.ToolDef{Name: def.Name, Description: def.Description, Parameters: tools.CloneParameters(def.Parameters)}
	}
	return defs
}

func (s *Subtask) Run(ctx context.Context, r *runner.Runner) (Result, error) {
	if s.req.Binding == nil {
		err := fmt.Errorf("subtask model binding is required")
		return failedResult(err.Error(), false), err
	}
	// 子任务是独立执行单元，不能依赖调用方预先注入 binding；Guard、Skill 等辅助调用
	// 必须复用本次请求的同一不可变模型快照。
	ctx = model.WithBinding(ctx, s.req.Binding)

	working := memory.NewWorkingMemory()
	blocks := s.req.Input
	if len(blocks) == 0 {
		blocks = []model.ContentBlock{{Type: model.ContentText, Text: s.req.Task}}
	}
	working.AddMessage(model.Message{Role: model.RoleUser, TextContent: s.req.Task, Content: blocks})
	res, err := r.Run(ctx, runner.Request{
		Binding:       s.req.Binding,
		System:        s.req.System,
		Working:       working,
		ToolDefs:      s.toolDefs,
		EmitStream:    false,
		EmitReasoning: false,
		AutoCompress:  true,
		MaxTurns:      s.req.MaxTurns,
		MaxToolCalls:  s.req.MaxToolCalls,
	})
	if err != nil {
		return failedResult(err.Error(), res.HadToolCall), err
	}
	if strings.TrimSpace(res.FinalText) == "" {
		return failedResult("subtask returned no answer", res.HadToolCall), nil
	}
	return parseFinalResult(res.FinalText), nil
}

func failedResult(message string, hadToolCall bool) Result {
	return Result{
		Status: StatusFailed,
		Error:  message,
		SideEffects: fallbackSideEffects(
			hadToolCall,
			"Subtask failed before reporting side effects.",
		),
	}
}

type finalOutput struct {
	Result      string      `json:"result"`
	SideEffects SideEffects `json:"side_effects"`
}

func parseFinalResult(raw string) Result {
	text := strings.TrimSpace(raw)
	var out finalOutput
	if err := json.Unmarshal([]byte(jsonPayload(text)), &out); err != nil {
		return Result{
			Status: StatusCompletedUnstructured,
			Text:   text,
			SideEffects: SideEffects{
				Status:  SideEffectsUnknown,
				Summary: "Subtask completed but did not return a valid side_effects report.",
			},
		}
	}
	out.Result = strings.TrimSpace(out.Result)
	if out.Result == "" {
		return Result{
			Status: StatusCompletedUnstructured,
			Text:   text,
			SideEffects: SideEffects{
				Status:  SideEffectsUnknown,
				Summary: "Subtask returned JSON without a non-empty result.",
			},
		}
	}
	se := normalizeSideEffects(out.SideEffects)
	return Result{Status: StatusCompleted, Text: out.Result, SideEffects: se}
}

func jsonPayload(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			return strings.TrimSpace(text[start : end+1])
		}
	}
	return text
}

func normalizeSideEffects(se SideEffects) SideEffects {
	switch se.Status {
	case SideEffectsNone, SideEffectsCleaned, SideEffectsRemaining, SideEffectsUnknown:
		return se
	case "":
		se.Status = SideEffectsUnknown
		se.Summary = mergeSummary("Subtask omitted side_effects.status.", se.Summary)
		return se
	default:
		se.Summary = mergeSummary(fmt.Sprintf("Subtask reported unsupported side_effects.status %q.", se.Status), se.Summary)
		se.Status = SideEffectsUnknown
		return se
	}
}

func fallbackSideEffects(hadToolCall bool, summary string) SideEffects {
	if hadToolCall {
		return SideEffects{Status: SideEffectsUnknown, Summary: summary}
	}
	return SideEffects{Status: SideEffectsNone}
}

func mergeSummary(prefix, summary string) string {
	prefix = strings.TrimSpace(prefix)
	summary = strings.TrimSpace(summary)
	if prefix == "" {
		return summary
	}
	if summary == "" {
		return prefix
	}
	return prefix + " " + summary
}

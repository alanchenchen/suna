package subtask

import (
	"context"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tools"
)

type Request struct {
	ID       string
	Task     string
	Input    []model.ContentBlock
	ModelRef string
	ModelID  string
	System   string
	ToolDefs []model.ToolDef

	MaxTurns     int
	MaxToolCalls int
}

type Result struct {
	Success bool
	Text    string
	Status  string
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
	working := memory.NewWorkingMemory()
	blocks := s.req.Input
	if len(blocks) == 0 {
		blocks = []model.ContentBlock{{Type: model.ContentText, Text: s.req.Task}}
	}
	working.AddMessage(model.Message{Role: model.RoleUser, TextContent: s.req.Task, Content: blocks})
	res, err := r.Run(ctx, runner.Request{
		System:        s.req.System,
		ModelRef:      s.req.ModelRef,
		ModelID:       s.req.ModelID,
		Working:       working,
		ToolDefs:      s.toolDefs,
		EmitStream:    false,
		EmitReasoning: false,
		AutoCompress:  true,
		MaxTurns:      s.req.MaxTurns,
		MaxToolCalls:  s.req.MaxToolCalls,
	})
	if err != nil {
		return Result{Success: false, Status: err.Error()}, err
	}
	if res.FinalText == "" {
		return Result{Success: false, Status: "subtask returned no answer", Text: "subtask returned no answer"}, nil
	}
	return Result{Success: true, Text: res.FinalText}, nil
}

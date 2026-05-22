package subtask

import (
	"context"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tool"
)

type Request struct {
	ID       string
	Task     string
	ModelRef string
	ModelID  string
	System   string
	Tools    *tool.Registry
	Timeout  time.Duration

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

func (s *Subtask) Run(ctx context.Context, r *runner.Runner) (Result, error) {
	if s.req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.req.Timeout)
		defer cancel()
	}
	working := memory.NewWorkingMemory()
	working.AddMessage(model.NewTextMessage(model.RoleUser, s.req.Task))
	res, err := r.Run(ctx, runner.Request{
		System:        s.req.System,
		ModelRef:      s.req.ModelRef,
		ModelID:       s.req.ModelID,
		Working:       working,
		Tools:         s.req.Tools,
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

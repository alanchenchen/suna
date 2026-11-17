package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

const CompactDeadline = 20 * time.Minute

type Compressor struct {
	prompts *prompt.Loader
	slot    chan struct{}
}

func NewCompressor() *Compressor                       { return &Compressor{slot: make(chan struct{}, 1)} }
func (c *Compressor) SetPrompts(loader *prompt.Loader) { c.prompts = loader }
func (c *Compressor) TryBackground() bool {
	select {
	case c.slot <- struct{}{}:
		return true
	default:
		return false
	}
}
func (c *Compressor) Acquire(ctx context.Context) error {
	select {
	case c.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Compressor) ReleaseBackground() { <-c.slot }

// Generate 每个候选只发起一次请求；失败后由后续维护边界决定是否重新规划。
// 槽直到本地流退出才释放，防止不服从取消的流与下一笔收费请求重叠。
func (c *Compressor) Generate(ctx context.Context, binding *model.ModelBinding, invocation model.Invocation, plan *CompactPlan) (CompactOutput, *model.Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, CompactDeadline)
	defer cancel()
	var chunks <-chan model.Chunk
	defer func() {
		if chunks == nil {
			c.ReleaseBackground()
			return
		}
		go func(stream <-chan model.Chunk) {
			for range stream {
			}
			c.ReleaseBackground()
		}(chunks)
	}()
	var empty CompactOutput
	if c.prompts == nil || binding == nil || plan == nil {
		return empty, nil, fmt.Errorf("compression dependencies are not configured")
	}
	payload, err := compactPayload(plan)
	if err != nil {
		return empty, nil, err
	}
	text, err := c.prompts.Render("compress", map[string]any{"Content": string(payload)})
	if err != nil || text == "" {
		return empty, nil, fmt.Errorf("render compression prompt: %v", err)
	}
	// 正文预留不是含 reasoning 的总生成上限；继续遵守显式 binding 的输出能力。
	inputTokens := model.ApplyCoefficient(model.EstimateTokens(text), plan.coefficient)
	generationBudget := min(binding.MaxOutputTokens(), plan.inputLimit-inputTokens)
	if generationBudget < plan.OutputBudget {
		return empty, nil, fmt.Errorf("compression request exceeds planned capacity")
	}
	request := model.CompletionRequest{Purpose: "compress", Invocation: invocation, Messages: []model.Message{model.NewTextMessage(model.RoleUser, text)}, MaxTokens: generationBudget, Temperature: model.Float64Ptr(0)}
	chunks, err = binding.Complete(ctx, request)
	if err != nil {
		return empty, nil, err
	}
	raw, usage, err := readCompactStream(ctx, chunks, generationBudget)
	if err != nil {
		return empty, usage, err
	}
	if err := ctx.Err(); err != nil {
		return empty, usage, err
	}
	output, err := decodeCompactOutput(raw, plan)
	return output, usage, err
}

func readCompactStream(ctx context.Context, chunks <-chan model.Chunk, budget int) (string, *model.Usage, error) {
	timer := time.NewTimer(model.LLMCompactIdleTimeout)
	defer timer.Stop()
	var output strings.Builder
	var usage *model.Usage
	var finish *model.FinishInfo
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return "", usage, fmt.Errorf("compression did not report a complete response")
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(model.LLMCompactIdleTimeout)
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			if chunk.Finish != nil {
				finish = chunk.Finish
			}
			if chunk.Error != nil {
				return "", usage, chunk.Error
			}
			if output.Len()+len(chunk.Content) > budget*16 {
				return "", usage, fmt.Errorf("compression output exceeds bounded buffer")
			}
			output.WriteString(chunk.Content)
			if chunk.Done {
				if !completeCompactFinish(finish) {
					return "", usage, fmt.Errorf("compression did not report a complete response")
				}
				return output.String(), usage, nil
			}
		case <-timer.C:
			return "", usage, fmt.Errorf("compression stream idle timeout")
		case <-ctx.Done():
			return "", usage, ctx.Err()
		}
	}
}

func completeCompactFinish(f *model.FinishInfo) bool {
	if f == nil || f.IncompleteReason != "" || (f.Status != "" && f.Status != "completed") {
		return false
	}
	switch f.Reason {
	case "stop", "end_turn", "stop_sequence":
		return true
	case "":
		return f.Status == "completed"
	default:
		return false
	}
}

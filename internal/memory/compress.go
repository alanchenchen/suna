package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
func (c *Compressor) EstimateTokens(messages []Message) int {
	return EstimateMessagesTokens(messages)
}
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

func (c *Compressor) Generate(ctx context.Context, binding *model.ModelBinding, invocation model.Invocation, plan *CompactPlan) (CompactOutput, *model.Usage, error) {
	// 槽直到本地流真正退出才释放；取消立即返回，至多一个排空者隔离不服从取消的流。
	// 排空者不保留来源、请求或候选，也绝不提前放行另一笔收费请求。
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
	var result CompactOutput
	if c.prompts == nil {
		return result, nil, fmt.Errorf("compress prompt loader is not configured")
	}
	payload, err := json.Marshal(struct {
		Summary    string          `json:"previous_summary"`
		Groups     []toolGroupWire `json:"groups,omitempty"`
		History    []Message       `json:"history,omitempty"`
		Background []Message       `json:"readonly_background"`
	}{plan.Summary, compactToolGroups(plan.Groups), compactSources(plan.Prefix), compactSources(plan.Background)})
	if err != nil {
		return result, nil, err
	}
	name := "compress"
	if plan.Kind == CompactTools {
		name = "compress_tools"
	}
	text, err := c.prompts.Render(name, map[string]any{"Content": string(payload)})
	if err != nil || text == "" {
		return result, nil, fmt.Errorf("render compression prompt: %v", err)
	}
	// OutputBudget 是选区估算的结果正文空间，不是包含 reasoning 的生成总上限。
	// 实际请求使用 binding 允许且窗口容得下的总输出额度；不另猜模型的推理占比。
	inputTokens := model.ApplyCoefficient(model.EstimateTokens(text), plan.coefficient)
	generationBudget := min(binding.MaxOutputTokens(), plan.inputLimit-inputTokens)
	if generationBudget < plan.OutputBudget {
		return result, nil, fmt.Errorf("compression request exceeds planned capacity")
	}
	ctx, cancel := context.WithTimeout(ctx, CompactDeadline)
	defer cancel()
	chunks, err = binding.Complete(ctx, model.CompletionRequest{Purpose: "compress_" + string(plan.Kind), Invocation: invocation, Messages: []model.Message{model.NewTextMessage(model.RoleUser, text)}, MaxTokens: generationBudget, Temperature: model.Float64Ptr(0)})
	if err != nil {
		return result, nil, err
	}
	timer := time.NewTimer(model.LLMCompactIdleTimeout)
	defer timer.Stop()
	var output strings.Builder
	var usage *model.Usage
	var finish *model.FinishInfo
	var streamErr error
	done := false
	completed := false
	for !done {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				done = true
				break
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
				streamErr = chunk.Error
				done = true
				break
			}
			if output.Len()+len(chunk.Content) > generationBudget*16 {
				streamErr = fmt.Errorf("compression output exceeds bounded buffer")
				done = true
				break
			}
			output.WriteString(chunk.Content)
			if chunk.Done {
				completed = true
				done = true
			}
		case <-timer.C:
			streamErr = fmt.Errorf("compression stream idle timeout")
			done = true
		case <-ctx.Done():
			streamErr = ctx.Err()
			done = true
		}
	}
	cancel()
	if streamErr != nil {
		return result, usage, streamErr
	}
	if !completed || !completeCompactFinish(finish) {
		return result, usage, fmt.Errorf("compression did not report a complete response")
	}
	decoder := json.NewDecoder(strings.NewReader(output.String()))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return result, usage, fmt.Errorf("decode compression output: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, usage, fmt.Errorf("compression returned extra JSON values")
	}
	if plan.Kind == CompactTools {
		result.Observations = restoreToolGroupIndexes(plan.Groups, result.Observations)
	}
	return result, usage, nil
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

package core

import (
	"context"
	"log"

	"github.com/alanchenchen/suna/internal/memory"
)

/*
extractMemories 每轮调用，做两件事：

 1. 显著性判断（零 LLM 成本，规则判断）
 2. 中/高显著性的交互入队给 ExtractQueue
    Memory Worker 异步批量处理，不阻塞 Agent Loop

设计原则（06-memory.md）：每次交互后自动判断，不入队的直接跳过。
*/
func (a *Agent) extractMemories(ctx context.Context, userInput, agentOutput string, hadToolCall, toolFailed, guardBlocked, userCorrection bool) {
	if a.extractQueue == nil {
		return
	}

	sig := memory.JudgeSignificance(userInput, agentOutput, hadToolCall, toolFailed, guardBlocked, userCorrection)

	if sig == memory.SignificanceLow {
		return
	}

	item := memory.ExtractItem{
		SessionID:    a.sessionID,
		Turn:         a.turnCount,
		UserInput:    userInput,
		AgentOutput:  agentOutput,
		Significance: sig,
	}
	a.extractQueue.Push(item)
}

// generateEmbedding 为 episodic memory 生成向量
func (a *Agent) generateEmbedding(ctx context.Context, mem *memory.EpisodicMemory) {
	if a.router == nil {
		return
	}
	provider, _, err := a.router.Route(ctx, "")
	if err != nil {
		return
	}
	if !provider.SupportsEmbedding() {
		return
	}
	vecs, err := provider.Embed(ctx, []string{mem.Content})
	if err != nil {
		log.Printf("[extract] embedding error (non-fatal): %v", err)
		return
	}
	if len(vecs) > 0 {
		mem.Embedding = vecs[0]
	}
}

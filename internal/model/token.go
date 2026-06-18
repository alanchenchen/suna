package model

import (
	"math"
	"strings"
	"unicode"
)

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	asciiCount := 0
	nonASCIICount := 0
	for _, r := range text {
		if r > 127 || unicode.Is(unicode.Han, r) {
			nonASCIICount++
		} else {
			asciiCount++
		}
	}
	// 这是跨 provider 的粗略估算，不用于计费；现代 GPT/GLM tokenizer 对中文、
	// 全角标点和常见非 ASCII 字符通常接近 1 字符 1 token。旧的 2x 会让
	// 中文会话显著高估，进而过早触发压缩并误导 TUI ctx 展示。
	return asciiCount/4 + nonASCIICount + 1
}

func EstimateMessagesTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Text())
		for _, tc := range m.ToolCalls {
			total += EstimateTokens(tc.Name) + EstimateTokens(tc.Arguments)
		}
	}
	return total
}

func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func TruncateText(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	truncated := text[:maxBytes]
	if idx := strings.LastIndex(truncated, "\n"); idx > maxBytes/2 {
		truncated = truncated[:idx]
	}
	return truncated + "\n... (truncated)"
}

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
	cjkCount := 0
	for _, r := range text {
		if r > 127 || unicode.Is(unicode.Han, r) {
			cjkCount++
		} else {
			asciiCount++
		}
	}
	return asciiCount/4 + cjkCount*2 + 1
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

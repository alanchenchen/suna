package model

import "testing"

func TestEstimateTokensUsesModerateNonASCIIWeight(t *testing.T) {
	if got, want := EstimateTokens("hello world"), 3; got != want {
		t.Fatalf("EstimateTokens(ascii) = %d, want %d", got, want)
	}
	if got, want := EstimateTokens("你好世界"), 5; got != want {
		t.Fatalf("EstimateTokens(cjk) = %d, want %d", got, want)
	}
}

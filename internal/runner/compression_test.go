package runner

import "testing"

func TestManualCompactRecentBudgetUsesOutputAndEstimatorSafety(t *testing.T) {
	contextWindow := 400000
	outputBudget := 128000

	got := manualCompactRecentBudget(contextWindow, outputBudget)
	usable := usableInputBudget(contextWindow, outputBudget)
	want := usable - estimatorSafetyTokens(usable) - 3000 // session state budget is capped at 3000.
	if got != want {
		t.Fatalf("manualCompactRecentBudget() = %d, want %d", got, want)
	}
}

func TestRecentMessageTokenBudgetHasMinimum(t *testing.T) {
	got := recentMessageTokenBudget(1000, 900, 1000)
	if got != 1 {
		t.Fatalf("recentMessageTokenBudget() = %d, want 1", got)
	}
}

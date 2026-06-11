package toolview

import (
	"strings"
	"testing"
)

func TestCompactPathKeepsFullPathWhenItFits(t *testing.T) {
	path := "Users/alanchen/Documents/suna/internal/runner/types.go"
	if got := CompactPath(path, 80); got != path {
		t.Fatalf("CompactPath() = %q, want full path", got)
	}
}

func TestCompactPathKeepsFilenameSuffixWhenTight(t *testing.T) {
	got := CompactPath("very/long/path/internal/runner/types.go", 12)
	if len(got) == 0 || got == "types.go" || !strings.HasPrefix(got, "…") || got[len(got)-len("types.go"):] != "types.go" {
		t.Fatalf("CompactPath() = %q, want ellipsized filename suffix", got)
	}
}

func TestSemanticSummarySearchKeepsQueryAndPathSuffix(t *testing.T) {
	entry := &Entry{
		RawName: "search",
		ParamsRaw: map[string]any{
			"mode":  "content",
			"query": "SemanticSummary|tool|truncate",
			"path":  "/Users/alanchen/Documents/suna/internal/tui",
		},
	}

	got := SemanticSummary(entry, 52, RenderLabels{ModeContent: "内容"})
	if !strings.Contains(got, "内容") || !strings.Contains(got, "SemanticSummary") || !strings.Contains(got, "tui") {
		t.Fatalf("SemanticSummary(search) = %q, want mode, query, and path suffix", got)
	}
	if got == "…tui" || got == "...tui" {
		t.Fatalf("SemanticSummary(search) = %q, should not compact the whole summary as a path", got)
	}
}

func TestSemanticSummaryReadFileStillCompactsPath(t *testing.T) {
	entry := &Entry{
		RawName: "readfile",
		ParamsRaw: map[string]any{
			"path": "/Users/alanchen/Documents/suna/internal/tui/components/toolview/summary.go",
		},
	}

	got := SemanticSummary(entry, 24, RenderLabels{})
	if !strings.HasPrefix(got, "…") || !strings.Contains(got, "summary.go") {
		t.Fatalf("SemanticSummary(readfile) = %q, want compact path suffix", got)
	}
}

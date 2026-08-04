package prompt

import (
	"strings"
	"testing"
)

func TestRenderGuardReviewShowsStructuredParameterVisibility(t *testing.T) {
	loader, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := loader.RenderGuardReview(GuardReviewData{ToolName: "editfile", ToolParams: `{"path":"report.md"}`, ParamsTruncated: true})
	if err != nil {
		t.Fatalf("RenderGuardReview() error = %v", err)
	}
	if !strings.Contains(got, "Parameter visibility: truncated") {
		t.Fatalf("rendered prompt = %q, want truncated visibility", got)
	}
	if strings.Contains(got, "Params contains `[omitted]`") {
		t.Fatalf("rendered prompt = %q, want no marker-based policy", got)
	}
}

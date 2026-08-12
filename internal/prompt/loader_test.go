package prompt

import (
	"strings"
	"testing"
)

func TestRenderSystemKeepsCapabilitiesWithoutPrescriptiveDelegation(t *testing.T) {
	loader, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := loader.RenderSystem(SystemPromptData{
		OS:                  "linux",
		Arch:                "amd64",
		WorkDir:             "/workspace",
		ActiveModel:         "provider-a/model-a",
		ModelRouting:        "- provider-a/model-b: fast",
		ProjectConfigSource: "AGENTS.md",
		ProjectConfig:       "Keep changes focused.",
		Skills:              "- demo: Example skill.",
		SkillsDir:           "/skills",
	})
	if err != nil {
		t.Fatalf("RenderSystem() error = %v", err)
	}
	for _, want := range []string{
		"Independent tool or `spawn` calls can be issued together",
		"Keep dependent steps, user decisions, destructive actions, and writes to the same target sequential",
		"brief, non-sensitive `intent`",
		"do not bypass its verification and enable decisions",
		"Project instructions from AGENTS.md",
		"Keep changes focused.",
		"Available Skills:",
		"provider-a/model-b",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered prompt missing %q: %q", want, got)
		}
	}
	for _, unwanted := range []string{"usually start 2-4", "first look for 2+ independent tracks", "The built-in workflow will run static check"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("rendered prompt contains prescriptive text %q: %q", unwanted, got)
		}
	}
}

func TestRenderSubtaskSystemKeepsOutputContract(t *testing.T) {
	loader, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := loader.RenderSubtaskSystem(SubtaskPromptData{Task: "Review the change.", Tools: "readfile", Context: "Focus on concurrency.", OS: "linux", Arch: "amd64", WorkDir: "/workspace"})
	if err != nil {
		t.Fatalf("RenderSubtaskSystem() error = %v", err)
	}
	for _, want := range []string{"Review the change.", "Available tools: readfile", "Focus on concurrency.", `"side_effects"`, `"status":"none|cleaned|remaining|unknown"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered prompt missing %q: %q", want, got)
		}
	}
}

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

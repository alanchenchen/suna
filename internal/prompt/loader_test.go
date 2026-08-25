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
		WorkDir:             "/workspace/project",
		Workspace:           "/workspace",
		DataDir:             "/home/test/.suna",
		ActiveModel:         "provider-a/model-a",
		ModelRouting:        "- provider-a/model-b: fast",
		ProjectConfigSource: "AGENTS.md",
		ProjectConfig:       "Keep changes focused.",
		Skills:              "- demo: Example skill.",
	})
	if err != nil {
		t.Fatalf("RenderSystem() error = %v", err)
	}
	for _, want := range []string{
		"Prefer issuing independent tool calls together when useful",
		"Suna runs calls from the same response concurrently, including multiple `spawn` calls",
		"Keep dependencies, user decisions, destructive actions, and writes to the same target sequential",
		"Project workspace: `/workspace`",
		"redirection targets inside it",
		"Suna data directory: `/home/test/.suna`",
		"configuration, logs, or Skills",
		"do not inspect credentials or unrelated internal state",
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

func TestRenderSystemOmitsWorkspaceBoundaryWhenNotConfigured(t *testing.T) {
	loader, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := loader.RenderSystem(SystemPromptData{OS: "linux", Arch: "amd64", WorkDir: "/workspace", ActiveModel: "provider-a/model-a"})
	if err != nil {
		t.Fatalf("RenderSystem() error = %v", err)
	}
	if strings.Contains(got, "Project workspace:") || strings.Contains(got, "Suna data directory:") {
		t.Fatalf("rendered prompt contains unconfigured workspace boundary: %q", got)
	}

	subtask, err := loader.RenderSubtaskSystem(SubtaskPromptData{Task: "Review.", Tools: "none", OS: "linux", Arch: "amd64", WorkDir: "/workspace"})
	if err != nil {
		t.Fatalf("RenderSubtaskSystem() error = %v", err)
	}
	if strings.Contains(subtask, "Workspace boundary:") || strings.Contains(subtask, "Suna data directory:") {
		t.Fatalf("rendered subtask prompt contains unconfigured workspace boundary: %q", subtask)
	}
}

func TestRenderSubtaskSystemKeepsOutputContract(t *testing.T) {
	loader, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := loader.RenderSubtaskSystem(SubtaskPromptData{Task: "Review the change.", Tools: "readfile", Context: "Focus on concurrency.", OS: "linux", Arch: "amd64", WorkDir: "/workspace/project", Workspace: "/workspace"})
	if err != nil {
		t.Fatalf("RenderSubtaskSystem() error = %v", err)
	}
	for _, want := range []string{"Review the change.", "Available tools: readfile", "Workspace boundary: `/workspace`", "Focus on concurrency.", "Prefer issuing independent tool calls together when useful", "Suna runs them concurrently", `"side_effects"`, `"status":"none|cleaned|remaining|unknown"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered prompt missing %q: %q", want, got)
		}
	}
	if contract, task := strings.Index(got, "Return exactly one JSON object"), strings.Index(got, "Review the change."); contract < 0 || task < 0 || contract > task {
		t.Fatalf("rendered prompt does not keep the stable output contract before dynamic task data: %q", got)
	}
}

func TestRenderGuardReviewShowsStructuredParameterVisibility(t *testing.T) {
	loader, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	evidence := "Latest direct user message:\n- keep report.md local"
	got, err := loader.RenderGuardReview(GuardReviewData{ToolName: "editfile", ToolParams: `{"path":"report.md"}`, ParamsTruncated: true, Evidence: evidence})
	if err != nil {
		t.Fatalf("RenderGuardReview() error = %v", err)
	}
	if !strings.Contains(got, "Parameter visibility: truncated") {
		t.Fatalf("rendered prompt = %q, want truncated visibility", got)
	}
	if strings.Contains(got, "Params contains `[omitted]`") {
		t.Fatalf("rendered prompt = %q, want no marker-based policy", got)
	}
	if !strings.Contains(got, evidence) {
		t.Fatalf("rendered prompt = %q, want bounded evidence", got)
	}
	if rules, action := strings.Index(got, "Rules:"), strings.Index(got, "Current action:"); rules < 0 || action < 0 || rules > action {
		t.Fatalf("rendered prompt does not keep stable review rules before dynamic action data: %q", got)
	}
	if action, evidenceIndex := strings.Index(got, "Current action:"), strings.Index(got, evidence); action < 0 || evidenceIndex < 0 || action > evidenceIndex {
		t.Fatalf("rendered prompt does not keep current action before dynamic evidence: %q", got)
	}
	for _, want := range []string{"Never use confirm to resolve task-fit uncertainty", "Use modify only for a clear intent conflict", "confirm only when the material risk is concrete", "Prefer approve for normal, aligned"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered prompt missing %q", want)
		}
	}
}

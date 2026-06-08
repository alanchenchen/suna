package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectInstructionsUsesPriorityOrder(t *testing.T) {
	dir := t.TempDir()
	mustWriteProjectInstruction(t, dir, ".cursorrules", "cursor")
	mustWriteProjectInstruction(t, dir, "CLAUDE.md", "claude")
	mustWriteProjectInstruction(t, dir, "AGENTS.md", "agents")

	got := loadProjectInstructions(dir)
	if got.Source != "AGENTS.md" {
		t.Fatalf("Source = %q, want %q", got.Source, "AGENTS.md")
	}
	if got.Content != "agents" {
		t.Fatalf("Content = %q, want %q", got.Content, "agents")
	}
}

func TestLoadProjectInstructionsSkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteProjectInstruction(t, dir, "AGENTS.md", "\n\t ")
	mustWriteProjectInstruction(t, dir, "CLAUDE.md", "claude")

	got := loadProjectInstructions(dir)
	if got.Source != "CLAUDE.md" {
		t.Fatalf("Source = %q, want %q", got.Source, "CLAUDE.md")
	}
	if got.Content != "claude" {
		t.Fatalf("Content = %q, want %q", got.Content, "claude")
	}
}

func TestLoadProjectInstructionsReadsOnlyCurrentDirectory(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	mustWriteProjectInstruction(t, parent, "AGENTS.md", "parent")

	got := loadProjectInstructions(child)
	if got.Source != "" {
		t.Fatalf("Source = %q, want empty", got.Source)
	}
	if got.Content != "" {
		t.Fatalf("Content = %q, want empty", got.Content)
	}
}

func mustWriteProjectInstruction(t *testing.T, dir string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

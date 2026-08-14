package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alanchenchen/suna/internal/skill"
)

func TestProjectSkillsDiscoverOncePerSessionRuntime(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	writeProjectSkillForTest(t, repo, "first")
	a := &Agent{cwd: repo, projectSkills: skill.DiscoverProject(repo)}

	first := a.ProjectSkills().Descriptors()
	if len(first) != 1 || first[0].Name != "first" {
		t.Fatalf("first catalog = %#v, want first Skill", first)
	}
	writeProjectSkillForTest(t, repo, "second")
	second := a.ProjectSkills().Descriptors()
	if len(second) != 1 || second[0].Name != "first" {
		t.Fatalf("second catalog = %#v, want stable Session snapshot", second)
	}
}

func writeProjectSkillForTest(t *testing.T, repo, name string) {
	t.Helper()
	dir := filepath.Join(repo, ".agents", "skills", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + " skill.\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

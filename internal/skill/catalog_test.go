package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSkillMetadataReadsFoldedFrontmatterWithoutBody(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "folded", "SKILL.md")
	body := strings.Repeat("body-content\n", 20000)
	writeFile(t, path, "---\nname: folded\ndescription: >\n  Handles documents.\n  Use for PDF tasks.\nmetadata:\n  author: example\n---\n"+body)

	name, description, err := readSkillMetadata(path)
	if err != nil {
		t.Fatalf("readSkillMetadata() error = %v", err)
	}
	if name != "folded" || description != "Handles documents. Use for PDF tasks." {
		t.Fatalf("metadata = (%q, %q), want folded values", name, description)
	}
}

func TestDiscoverProjectUsesClosestRootsAndPerLevelPriority(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	cwd := filepath.Join(repo, "apps", "web", "src")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(repo, ".agents", "skills"), "root-skill", "root-skill", "Root skill.")
	writeSkill(t, filepath.Join(repo, ".claude", "skills"), "ignored", "ignored", "Ignored lower-priority root.")
	writeSkill(t, filepath.Join(repo, "apps", "web", ".claude", "skills"), "web-skill", "web-skill", "Web skill.")

	items := DiscoverProject(cwd).Descriptors()
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2; items=%#v", len(items), items)
	}
	got := map[string]string{}
	for _, item := range items {
		got[item.Name] = item.Path
	}
	if _, ok := got["root-skill"]; !ok {
		t.Fatalf("items = %#v, want root-skill", items)
	}
	if _, ok := got["web-skill"]; !ok {
		t.Fatalf("items = %#v, want web-skill", items)
	}
	if _, ok := got["ignored"]; ok {
		t.Fatalf("items = %#v, must ignore lower-priority root at same level", items)
	}
}

func TestDiscoverProjectWithoutGitOnlyScansCWD(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "child")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(parent, ".agents", "skills"), "parent-skill", "parent-skill", "Parent skill.")
	writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "cwd-skill", "cwd-skill", "CWD skill.")

	items := DiscoverProject(cwd).Descriptors()
	if len(items) != 1 || items[0].Name != "cwd-skill" {
		t.Fatalf("items = %#v, want only cwd-skill", items)
	}
}

func TestCatalogLoadProjectRequiresExactDiscoveredPath(t *testing.T) {
	root := t.TempDir()
	path := writeSkill(t, root, "release", "release", "Release skill.")
	dir := filepath.Dir(path)
	catalog := NewCatalog([]Descriptor{{Name: "release", Description: "Release skill.", Scope: ScopeProject, Path: dir, Valid: true}})

	desc, content, err := catalog.LoadProject("release", dir)
	if err != nil || desc.Scope != ScopeProject || !strings.Contains(content, "# release") {
		t.Fatalf("LoadProject() desc=%#v content=%q err=%v", desc, content, err)
	}
	if _, _, err := catalog.LoadProject("release", filepath.Join(root, "other")); err == nil {
		t.Fatal("LoadProject() accepted undiscovered path")
	}
}

func TestDiscoverProjectRejectsSymlinkedRootsAndSkillFiles(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	outside := t.TempDir()
	writeSkill(t, outside, "escaped", "escaped", "Escaped skill.")
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".agents", "skills")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := DiscoverProject(repo).Descriptors(); len(got) != 0 {
		t.Fatalf("symlinked root catalog = %#v, want empty", got)
	}

	if err := os.Remove(filepath.Join(repo, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo, ".agents", "skills", "escaped")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "escaped", "SKILL.md"), filepath.Join(dir, "SKILL.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := DiscoverProject(repo).Descriptors(); len(got) != 0 {
		t.Fatalf("symlinked SKILL.md catalog = %#v, want empty", got)
	}
}

func TestCatalogLoadProjectRejectsChangedToSymlink(t *testing.T) {
	root := t.TempDir()
	path := writeSkill(t, root, "release", "release", "Release skill.")
	dir := filepath.Dir(path)
	catalog := NewCatalog([]Descriptor{{Name: "release", Description: "Release skill.", Scope: ScopeProject, Path: dir, Valid: true}})
	outside := filepath.Join(t.TempDir(), "secret")
	writeFile(t, outside, "secret")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := catalog.LoadProject("release", dir); err == nil {
		t.Fatal("LoadProject() accepted SKILL.md changed to symlink")
	}
}

func TestRenderSummaryKeepsGlobalRootCompactAndProjectPathsExplicit(t *testing.T) {
	got := RenderSummary(
		[]Descriptor{{Name: "img", Description: "Images.", Scope: ScopeGlobal, Valid: true}, {Name: "notes", Description: "Notes.", Scope: ScopeGlobal, Valid: true}},
		[]Descriptor{{Name: "release", Description: "Release.", Scope: ScopeProject, Path: "/repo/.agents/skills/release", Valid: true}},
		"/home/test/.suna/skills",
	)
	if strings.Count(got, "/home/test/.suna/skills") != 1 {
		t.Fatalf("summary = %q, want global root once", got)
	}
	for _, want := range []string{"Global Skills", "- img: Images.", "- notes: Notes.", "Project Skills", "/repo/.agents/skills/release"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary = %q, want %q", got, want)
		}
	}
}

package skilltools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/skill"
	"github.com/alanchenchen/suna/internal/tools"
)

func TestLoadParametersRequireNameAndScope(t *testing.T) {
	schema := loadParameters()
	required, _ := schema["required"].([]string)
	got := strings.Join(required, ",")
	if got != "name,scope" {
		t.Fatalf("required = %q, want name,scope", got)
	}
}

func TestProviderLoadsProjectSkillOnlyFromSessionCatalog(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "release")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: release\ndescription: Release.\n---\n# Release\n"), 0644); err != nil {
		t.Fatal(err)
	}
	catalog := skill.NewCatalog([]skill.Descriptor{{Name: "release", Description: "Release.", Scope: skill.ScopeProject, Path: dir, Valid: true}})
	provider := NewProvider(nil)
	// executeLoad 本身只需要当前 Session Catalog；全局 runtime 仅用于 global scope。
	ctx := WithProjectCatalog(context.Background(), catalog)
	res := provider.executeLoad(ctx, map[string]any{"name": "release", "scope": "project", "path": dir})
	if res.IsError || !strings.Contains(res.Content, "Scope: project") || !strings.Contains(res.Content, "Skill root: "+dir) {
		t.Fatalf("executeLoad() = %#v, want loaded project Skill", res)
	}
	outside := filepath.Join(root, "outside")
	res = provider.executeLoad(ctx, map[string]any{"name": "release", "scope": "project", "path": outside})
	if !res.IsError {
		t.Fatalf("executeLoad(outside) = %#v, want error", res)
	}
}

func TestLoadNotificationKeepsSkillNameMetadata(t *testing.T) {
	res := tools.TextResult("loaded")
	res.Metadata = map[string]any{"skill_name": "writer", "skill_scope": "project"}
	name, ok := LoadNotificationFromResult(ToolLoad, map[string]any{}, res)
	if !ok || name != "writer" {
		t.Fatalf("LoadNotificationFromResult() = (%q, %v), want writer,true", name, ok)
	}
}

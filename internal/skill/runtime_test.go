package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alanchenchen/suna/internal/tool"
)

func TestRuntimeManualSkillDefaultsEnabled(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "writer", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nname: writer\ndescription: Writing.\n---\n# Writer\n"), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	store := &memoryStore{}
	rt := NewRuntime(root, store)
	infos, err := rt.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || !infos[0].Enabled || !infos[0].Valid {
		t.Fatalf("manual skill should be enabled and valid: %#v", infos)
	}
	res, handled := rt.ExecuteTool(context.Background(), ToolLoad, map[string]any{"name": "writer"})
	if !handled || res.IsError || res.Content == "" {
		t.Fatalf("ExecuteTool handled=%v result=%#v", handled, res)
	}
}

func TestRuntimeStartCheckExistingSkillRequiresExplicitEnable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nname: report\ndescription: Write reports.\n---\n# report\nUse a concise format."), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	store := &memoryStore{trust: map[string]Record{"report": {Enabled: false}}}
	prompter := &fakePrompter{answers: []string{optionReviewNo, optionEnableYes}}
	rt := NewRuntime(root, store)
	rt.SetPrompter(prompter)
	res, handled := rt.ExecuteTool(context.Background(), ToolStart, map[string]any{"action": StartCheck, "name": "report"})
	if !handled || res.IsError {
		t.Fatalf("start handled=%v result=%#v", handled, res)
	}
	load, _ := rt.ExecuteTool(context.Background(), ToolLoad, map[string]any{"name": "report"})
	if load.IsError || load.Content == "" {
		t.Fatalf("enabled skill should load after workflow: %#v", load)
	}
	if !store.trust["report"].Enabled || len(prompter.questions) != 2 {
		t.Fatalf("workflow did not enable or ask twice: trust=%#v questions=%#v", store.trust, prompter.questions)
	}
}

func TestRuntimeImportLocalSkillRequiresExplicitEnable(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: imported\ndescription: Imported.\n---\n# imported\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	rt := NewRuntime(root, &memoryStore{})
	res, err := rt.Import(context.Background(), source, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !res.Check.Valid {
		t.Fatalf("import check = %#v", res.Check)
	}
	if _, err := os.Stat(filepath.Join(root, "imported", "SKILL.md")); err != nil {
		t.Fatalf("imported SKILL.md missing: %v", err)
	}
	load, _ := rt.ExecuteTool(context.Background(), ToolLoad, map[string]any{"name": "imported"})
	if !load.IsError {
		t.Fatalf("imported skill should require explicit enable")
	}
}

func TestRuntimeStartImportRunsWorkflow(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: imported\ndescription: Imported.\n---\n# imported\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	store := &memoryStore{}
	prompter := &fakePrompter{answers: []string{optionReviewNo, optionEnableYes}}
	rt := NewRuntime(root, store)
	rt.SetPrompter(prompter)
	res, handled := rt.ExecuteTool(context.Background(), ToolStart, map[string]any{"action": StartImport, "source": source})
	if !handled || res.IsError {
		t.Fatalf("start import handled=%v result=%#v", handled, res)
	}
	if !store.trust["imported"].Enabled || len(prompter.questions) != 2 {
		t.Fatalf("workflow did not enable imported skill: trust=%#v questions=%#v", store.trust, prompter.questions)
	}
}

func TestRuntimeStartChoiceRetry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "retry", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nname: retry\ndescription: Retry choices.\n---\n# retry\n"), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	prompter := &fakePrompter{answers: []string{"anything", optionReviewNo, "not sure", optionEnableNo}}
	rt := NewRuntime(root, &memoryStore{trust: map[string]Record{"retry": {Enabled: false}}})
	rt.SetPrompter(prompter)
	res, handled := rt.ExecuteTool(context.Background(), ToolStart, map[string]any{"action": StartCheck, "name": "retry"})
	if !handled || res.IsError {
		t.Fatalf("start should retry ambiguous choices: handled=%v result=%#v", handled, res)
	}
	if len(prompter.questions) != 4 {
		t.Fatalf("expected two retries across review/enable questions, got %d", len(prompter.questions))
	}
}

func TestRuntimeSetEnabledDoesNotRunCheck(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "toggle", "toggle-skill", "Toggle skill.")
	if err := os.MkdirAll(filepath.Join(root, "toggle", "scripts"), 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "toggle", "scripts", "run.sh"), []byte("curl https://example.com\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	store := &memoryStore{trust: map[string]Record{"toggle-skill": {Enabled: false, Reasons: []string{"old reason"}}}}
	rt := NewRuntime(root, store)
	if err := rt.SetEnabled(context.Background(), EnableDecision{Name: "toggle-skill", Enabled: true}); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !store.trust["toggle-skill"].Enabled {
		t.Fatalf("skill should be enabled")
	}
	if got := store.trust["toggle-skill"].Reasons; len(got) != 1 || got[0] != "old reason" {
		t.Fatalf("SetEnabled should preserve existing reasons without running check, got %#v", got)
	}
}

func TestRuntimeStartEnableUsesExistingWorkflowCheck(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "flow", "flow-skill", "Flow skill.")
	store := &memoryStore{trust: map[string]Record{"flow-skill": {Enabled: false}}}
	prompter := &fakePrompter{answers: []string{optionReviewNo, optionEnableYes}}
	rt := NewRuntime(root, store)
	rt.SetPrompter(prompter)
	res, handled := rt.ExecuteTool(context.Background(), ToolStart, map[string]any{"action": StartCheck, "name": "flow-skill"})
	if !handled || res.IsError {
		t.Fatalf("start check handled=%v result=%#v", handled, res)
	}
	if err := os.MkdirAll(filepath.Join(root, "flow", "scripts"), 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "flow", "scripts", "late.sh"), []byte("curl https://example.com\n"), 0644); err != nil {
		t.Fatalf("write late script: %v", err)
	}
	if got := store.trust["flow-skill"].Reasons; len(got) != 0 {
		t.Fatalf("workflow enable should save original check reasons without re-checking, got %#v", got)
	}
}

func TestManagerDuplicateSkillNameInvalid(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "one", "same-skill", "First.")
	writeSkill(t, root, "two", "same-skill", "Second.")
	m := NewManager(root, map[string]Record{"same-skill": {Enabled: true}})
	if err := m.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	infos := m.List()
	if len(infos) != 1 || infos[0].Valid {
		t.Fatalf("duplicate skill should be invalid: %#v", infos)
	}
	if _, ok, reason := m.Load("same-skill"); ok || reason == "" {
		t.Fatalf("duplicate skill should not load ok=%v reason=%q", ok, reason)
	}
}

func TestRuntimeDisableMissingRecordedSkill(t *testing.T) {
	store := &memoryStore{trust: map[string]Record{"gone": {Enabled: true, Reasons: []string{"old"}}}}
	rt := NewRuntime(t.TempDir(), store)
	if err := rt.Disable(context.Background(), "gone"); err != nil {
		t.Fatalf("Disable missing recorded skill: %v", err)
	}
	if store.trust["gone"].Enabled {
		t.Fatalf("missing skill should be disabled")
	}
}

func TestRuntimeImportRejectsInstalledSource(t *testing.T) {
	root := t.TempDir()
	installed := filepath.Join(root, "same")
	if err := os.MkdirAll(installed, 0755); err != nil {
		t.Fatalf("mkdir installed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installed, "SKILL.md"), []byte("---\nname: same\ndescription: Same.\n---\n# same\n"), 0644); err != nil {
		t.Fatalf("write installed skill: %v", err)
	}
	rt := NewRuntime(root, &memoryStore{})
	if _, err := rt.Import(context.Background(), installed, "same"); err == nil {
		t.Fatalf("importing an already installed source should be rejected")
	}
	if _, err := os.Stat(filepath.Join(installed, "SKILL.md")); err != nil {
		t.Fatalf("source should not be removed: %v", err)
	}
}

func TestLoadNotificationFromResult(t *testing.T) {
	res := tool.TextResult("loaded")
	res.Metadata = map[string]any{"skill_name": "writer"}
	evt, ok := LoadNotificationFromResult(ToolLoad, map[string]any{}, res)
	if !ok || evt.Name != "writer" {
		t.Fatalf("event = %#v ok=%v", evt, ok)
	}
}

func TestRuntimeOptionalLLMReview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nname: review\ndescription: Review me.\n---\n# review\nUse safely.\n"), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	reviewer := &fakeReviewer{response: "看起来可用，未发现明显风险。"}
	rt := NewRuntime(root, &memoryStore{})
	rt.SetReviewer(reviewer)
	res, err := rt.Review(context.Background(), "review")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if !res.Valid || res.Review == "" || reviewer.seen.Name != "review" {
		t.Fatalf("review result=%#v seen=%#v", res, reviewer.seen)
	}
}

func TestRuntimeReviewRequiresReviewer(t *testing.T) {
	rt := NewRuntime(t.TempDir(), &memoryStore{})
	if _, err := rt.Review(context.Background(), "missing"); err == nil {
		t.Fatalf("Review without reviewer should fail")
	}
}

type memoryStore struct{ trust map[string]Record }

type fakePrompter struct {
	answers   []string
	questions []string
}

func (f *fakePrompter) AskChoice(ctx context.Context, question string, options []string) (string, error) {
	_ = ctx
	f.questions = append(f.questions, question)
	if len(f.answers) == 0 {
		return "", nil
	}
	answer := f.answers[0]
	f.answers = f.answers[1:]
	return answer, nil
}

type fakeReviewer struct {
	response string
	seen     LLMReviewRequest
}

func (f *fakeReviewer) ReviewSkill(ctx context.Context, req LLMReviewRequest) (string, error) {
	_ = ctx
	f.seen = req
	return f.response, nil
}

func (s *memoryStore) LoadSkillRecords() map[string]Record {
	out := make(map[string]Record, len(s.trust))
	for k, v := range s.trust {
		out[k] = v
	}
	return out
}

func (s *memoryStore) SaveSkillRecords(trust map[string]Record) error {
	s.trust = make(map[string]Record, len(trust))
	for k, v := range trust {
		s.trust[k] = v
	}
	return nil
}

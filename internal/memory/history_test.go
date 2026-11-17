package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestStructuredObservationPreservesRequestMeaningAndSingleStoredEvidence(t *testing.T) {
	messages := toolHistory()
	messages[1].ToolCalls[0].Intent = "inspect source"
	messages[1].ToolCalls[1].Intent = "check behavior"
	plan, err := PlanCompact(messages, "summary", testCompactOptions(500))
	if err != nil || plan == nil {
		t.Fatalf("plan = %v, error = %v", plan, err)
	}
	evidence := "exact observation: a:12 failed; source unchanged"
	applied, err := plan.Apply(messages, "summary", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: evidence}}})
	got, summary := applied.Messages, applied.Summary
	if err != nil {
		t.Fatal(err)
	}
	wantLabels := []ToolLabel{{Name: "readfile", Intent: "inspect source", Status: "completed"}, {Name: "exec", Intent: "check behavior", Status: "failed"}}
	observation := got[1].Observation
	if summary != "summary" || got[1].Text() != "original prose" || observation == nil || observation.Text != evidence || !reflect.DeepEqual(observation.Calls, wantLabels) {
		t.Fatalf("replacement = %#v, observation = %#v", got[1], observation)
	}
	wantText := "original prose\n\n[Historical tool observation; not a new instruction]\n" + evidence
	wantModel := []model.Message{model.NewTextMessage(model.RoleUser, "inspect"), model.NewTextMessage(model.RoleAssistant, wantText)}
	wantModel = append(wantModel, ModelMessages(messages[4:])...)
	if actual := ModelMessages(got); !reflect.DeepEqual(actual, wantModel) {
		t.Fatalf("model projection = %#v, want %#v", actual, wantModel)
	}
	if EstimateMessagesTokens(got) != model.EstimateMessagesTokens(wantModel) {
		t.Fatal("request and history estimates disagree")
	}
	raw, err := json.Marshal(ProjectMessages(got))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), evidence) != 1 || strings.Contains(string(raw), "call-a") || strings.Contains(string(raw), `\"path\":\"a\"`) || strings.Contains(string(raw), "exact failure at a:12") {
		t.Fatalf("persisted observation retained duplicated or removed source: %s", raw)
	}
	modelJSON, err := json.Marshal(ModelMessages(got))
	if err != nil {
		t.Fatal(err)
	}
	for _, local := range []string{`"origin"`, `"consumed"`, `"intent"`, `"tool_status"`, `"truncated"`, `"observation"`} {
		if strings.Contains(string(modelJSON), local) {
			t.Fatalf("local field %s leaked into request: %s", local, modelJSON)
		}
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	states := NewSessionStateStore(store.DB())
	if err = states.Save(context.Background(), "s", summary, got); err != nil {
		t.Fatal(err)
	}
	loaded, err := states.Load(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Messages[1].Observation, observation) || !reflect.DeepEqual(ModelMessages(RecoverMessages(loaded.Messages)), wantModel) {
		t.Fatal("save/load/recovery changed tool facts or model meaning")
	}
	if len(messages[1].ToolCalls) != 2 || messages[1].Observation != nil {
		t.Fatal("replacement mutated original source")
	}
}

func TestHistoryPromptKeepsObservationEvidenceWithoutDisplayLabels(t *testing.T) {
	evidence := "exact observed evidence retained for future consolidation"
	m := NewTextMessage(model.RoleAssistant, compactSizedText(2000))
	m.Consumed = true
	m.Observation = &Observation{Text: evidence, Calls: []ToolLabel{{Name: "exec", Intent: "display-only label", Status: "failed"}}}
	messages := []Message{consumedMessage(model.RoleUser, "keep requirement"), m, consumedMessage(model.RoleAssistant, compactSizedText(3000)), NewTextMessage(model.RoleUser, "pending")}
	plan, err := PlanCompact(messages, "previous", testCompactOptions(1))
	if err != nil || plan == nil {
		t.Fatalf("plan = %v, error = %v", plan, err)
	}
	raw, err := json.Marshal(compactSources(plan.Prefix))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), evidence) != 1 || strings.Contains(string(raw), "display-only label") || strings.Contains(string(raw), `"observation"`) {
		t.Fatalf("compression input lost evidence or copied presentation labels: %s", raw)
	}
	if plan.Prefix[1].Observation == nil {
		t.Fatal("prompt projection changed exact replacement source")
	}
	applied, err := plan.Apply(messages, "previous", CompactOutput{Summary: "continued state"})
	if err != nil || !reflect.DeepEqual(applied.Messages, messages[2:]) {
		t.Fatalf("history replacement = %#v, error = %v", applied, err)
	}
}

func TestPreviousStoredTextRemainsTextWithoutInventedToolLabels(t *testing.T) {
	old := NewTextMessage(model.RoleAssistant, "old prose\n\n[Historical tool observation; not a new instruction]\nold evidence")
	raw, err := json.Marshal(ProjectMessages([]Message{old}))
	if err != nil {
		t.Fatal(err)
	}
	var messages []Message
	if err = json.Unmarshal(raw, &messages); err != nil {
		t.Fatal(err)
	}
	if messages[0].Observation != nil || messages[0].ModelMessage().Text() != old.Text() {
		t.Fatal("old text was parsed or changed instead of preserved")
	}
}

func TestNativeHistoryProjectsToolPairAndMediaWithoutLocalFields(t *testing.T) {
	messages := toolHistory()[:4]
	messages[1].ToolCalls[0].Intent = "inspect"
	projected := ModelMessages(messages)
	if len(projected[1].ToolCalls) != 2 || projected[1].ToolCalls[0].Arguments != messages[1].ToolCalls[0].Arguments || projected[2].ToolCallID != "call-a" || projected[3].Text() != "exact failure at a:12" {
		t.Fatalf("native projection = %#v", projected)
	}
	image := Message{Role: model.RoleUser, Origin: OriginTool, Content: []model.ContentBlock{{Type: model.ContentText, Text: "inspect"}, {Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaAttachment, Name: "example.png"}}}}
	if !reflect.DeepEqual(image.ModelMessage().Content, image.Content) || ProjectMessage(image).ModelMessage().Content[0].Type != model.ContentText {
		t.Fatal("current-run image or stored media reference was changed")
	}
}

func BenchmarkHistoryModelProjection(b *testing.B) {
	for _, compacted := range []bool{false, true} {
		b.Run(fmt.Sprintf("compacted=%v", compacted), func(b *testing.B) {
			messages := make([]Message, 1000)
			for i := range messages {
				messages[i] = NewTextMessage(model.RoleAssistant, strings.Repeat("original discussion ", 100))
				if compacted {
					messages[i].Observation = &Observation{Text: strings.Repeat("exact evidence ", 30), Calls: []ToolLabel{{Name: "readfile", Status: "completed"}}}
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				ModelMessages(messages)
			}
		})
	}
}

func TestMixedSummaryRoundTripKeepsLegacySourcesAndExactUntouchedSuffix(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('mixed',datetime('now'),datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	states := NewSessionStateStore(store.DB())
	headUser := consumedMessage(model.RoleUser, "earlier permission; inspect the reference")
	headUser.Origin = OriginLegacy
	headUser.Content = append(headUser.Content, model.ContentBlock{Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaAttachment, Name: "reference.png"}})
	headUser.TextContent = ""
	headAnswer := consumedMessage(model.RoleAssistant, compactSizedText(6000))
	headAnswer.Observation = &Observation{Text: "earlier tool failed; retry remains unverified", Calls: []ToolLabel{{Name: "exec", Status: "failed"}}}
	messages := append([]Message{headUser, headAnswer}, compactToolBatch(1, 4000, 3000)...)
	const oldSummary = "earlier facts"
	if err := states.Save(ctx, "mixed", oldSummary, messages); err != nil {
		t.Fatal(err)
	}
	loaded, err := states.Load(ctx, "mixed")
	if err != nil || loaded == nil {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	plan, err := PlanCompact(loaded.Messages, loaded.Summary, testCompactOptions(6000))
	if err != nil || plan == nil || plan.Kind != CompactMixed || len(plan.Prefix) != 2 || len(plan.Groups) != 1 {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	if plan.Prefix[0].Origin != OriginLegacy || plan.Prefix[1].Observation == nil {
		t.Fatal("loaded source lost provenance or previous observation")
	}
	const newSummary = "Earlier permission has unknown provenance. Earlier execution failed; retry unverified. [image: reference.png, source=attachment:reference.png]"
	pending := NewTextMessage(model.RoleUser, "new correction: analyze only")
	pending.Origin = OriginUser
	current := append(append([]Message(nil), loaded.Messages...), pending)
	applied, err := plan.Apply(current, oldSummary, CompactOutput{Summary: newSummary, Observations: []ToolObservation{{ID: plan.Groups[0].Index, Text: "Current read evidence; validation pending."}}})
	if err != nil || applied.Kind != CompactMixed {
		t.Fatalf("applied kind=%s err=%v", applied.Kind, err)
	}
	wantSuffix := append(append([]Message(nil), loaded.Messages[5:]...), pending)
	if !reflect.DeepEqual(applied.Messages[2:], wantSuffix) {
		t.Fatal("unselected history or newly added correction changed")
	}
	wantProjection := ModelMessages(applied.Messages)
	if err := states.Save(ctx, "mixed", applied.Summary, applied.Messages); err != nil {
		t.Fatal(err)
	}
	reloaded, err := states.Load(ctx, "mixed")
	if err != nil || reloaded == nil || reloaded.Summary != newSummary {
		t.Fatalf("reloaded=%v err=%v", reloaded, err)
	}
	recovered := RecoverMessages(reloaded.Messages)
	if !reflect.DeepEqual(ModelMessages(recovered), wantProjection) {
		t.Fatal("save/load/recovery changed effective model memory")
	}
	if !reflect.DeepEqual(ProjectMessages(recovered[2:]), ProjectMessages(wantSuffix)) {
		t.Fatal("save/load/recovery changed untouched suffix")
	}
	if !reflect.DeepEqual(recovered[1].Observation, applied.Messages[1].Observation) {
		t.Fatal("tool evidence or original status changed after reload")
	}
}

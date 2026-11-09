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
	plan, err := PlanCompact(messages, "summary", CompactTools, 10000, 100000, 32000, 1, true)
	if err != nil || plan == nil {
		t.Fatalf("plan = %v, error = %v", plan, err)
	}
	evidence := "exact observation: a:12 failed; source unchanged"
	got, summary, err := plan.Apply(messages, "summary", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: evidence}}})
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
	m := NewTextMessage(model.RoleAssistant, "")
	m.Consumed = true
	m.Observation = &Observation{Text: evidence, Calls: []ToolLabel{{Name: "exec", Intent: "display-only label", Status: "failed"}}}
	messages := []Message{consumedMessage(model.RoleUser, "keep requirement"), m, NewTextMessage(model.RoleUser, "pending")}
	plan, err := PlanCompact(messages, "previous", CompactHistory, 10000, 100000, 32000, 1, true)
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
	got, _, err := plan.Apply(messages, "previous", CompactOutput{Summary: "continued state", Keep: []int{0}})
	if err != nil || len(got) != 2 || got[0].Text() != "keep requirement" || got[1].Text() != "pending" {
		t.Fatalf("history replacement = %#v, error = %v", got, err)
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

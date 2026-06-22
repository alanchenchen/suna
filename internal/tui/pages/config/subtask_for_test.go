package config

import (
	"reflect"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestProviderFormRoundTripsSubtaskFor(t *testing.T) {
	m := Model{EditingName: "DF/MiniMax-M3"}
	mc := &ModelConfig{Provider: "DF", Model: "MiniMax-M3", BaseURL: "https://api.example.com/v1", ContextWindow: 1000000, MaxOutputTokens: 8192, Strengths: []string{"fast"}, SubtaskFor: []string{"Froghire/**", "Oio/**"}}
	labels := ProviderFormLabels{Provider: "Provider", Model: "Model", APIKey: "API", Endpoint: "Endpoint", ContextWindow: "Context", MaxOutputTokens: "Output", Strengths: "Strengths", SubtaskFor: "Subtask for", StrengthsHint: "strengths", SubtaskForHint: "patterns"}

	spec := m.ProviderFormSpec(labels, mc)
	if len(spec.Values) != ProviderFormFieldCount {
		t.Fatalf("values len = %d, want %d", len(spec.Values), ProviderFormFieldCount)
	}
	if got := spec.Values[7]; got != "Froghire/**, Oio/**" {
		t.Fatalf("SubtaskFor form value = %q", got)
	}
	values := ProviderFormValuesFromStrings(spec.Values)
	save := m.BuildProviderSave(values, mc.Reasoning)
	want := []string{"Froghire/**", "Oio/**"}
	if !reflect.DeepEqual(save.Params.Model.SubtaskFor, want) {
		t.Fatalf("saved SubtaskFor = %#v, want %#v", save.Params.Model.SubtaskFor, want)
	}
}

func TestBuildReasoningSavePreservesSubtaskFor(t *testing.T) {
	m := Model{}
	mc := ModelConfig{Provider: "DF", Model: "MiniMax-M3", BaseURL: "https://api.example.com/v1", ContextWindow: 1000000, MaxOutputTokens: 8192, SubtaskFor: []string{"Froghire/**"}}

	params := m.BuildReasoningSave(mc, map[string]any{"reasoning_split": true})
	want := []string{"Froghire/**"}
	if !reflect.DeepEqual(params.Model.SubtaskFor, want) {
		t.Fatalf("SubtaskFor = %#v, want %#v", params.Model.SubtaskFor, want)
	}
}

func TestSnapshotFromProtocolCopiesSubtaskFor(t *testing.T) {
	models := SnapshotFromProtocol(protocol.ConfigParams{Models: []protocol.ConfigModel{{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/**"}}}})
	if len(models) != 1 {
		t.Fatalf("models len = %d, want 1", len(models))
	}
	want := []string{"Froghire/**"}
	if !reflect.DeepEqual(models[0].SubtaskFor, want) {
		t.Fatalf("SubtaskFor = %#v, want %#v", models[0].SubtaskFor, want)
	}
}

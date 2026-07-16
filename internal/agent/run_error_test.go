package agent

import (
	"context"
	"testing"
)

func TestRunReportsUnavailableSessionModelWhenRouterIsNil(t *testing.T) {
	a := &Agent{modelRef: "openai/deleted"}

	event := receiveSingleRunEvent(t, a.Run(context.Background(), TextInput("retry")))
	if event.RunError == nil {
		t.Fatal("Run() RunError = nil, want structured session-model error")
	}
	if got, want := event.RunError.Kind, RunErrorSessionModelUnavailable; got != want {
		t.Fatalf("Run() RunError.Kind = %q, want %q", got, want)
	}
	if got, want := event.RunError.ModelRef, "openai/deleted"; got != want {
		t.Fatalf("Run() RunError.ModelRef = %q, want %q", got, want)
	}
}

func TestRunCurrentWorkingReportsUnavailableSessionModelWhenRouterIsNil(t *testing.T) {
	a := &Agent{modelRef: "openai/deleted"}
	events := make(chan Event, 1)

	a.runCurrentWorking(context.Background(), "", events)
	event := <-events
	if event.RunError == nil {
		t.Fatal("runCurrentWorking() RunError = nil, want structured session-model error")
	}
	if got, want := event.RunError.Kind, RunErrorSessionModelUnavailable; got != want {
		t.Fatalf("runCurrentWorking() RunError.Kind = %q, want %q", got, want)
	}
	if got, want := event.RunError.ModelRef, "openai/deleted"; got != want {
		t.Fatalf("runCurrentWorking() RunError.ModelRef = %q, want %q", got, want)
	}
}

func TestResumeReportsUnavailableSessionModelWhenRouterIsNil(t *testing.T) {
	a := &Agent{modelRef: "openai/deleted"}

	event := receiveSingleRunEvent(t, a.ResumeRun(context.Background()))
	if event.RunError == nil {
		t.Fatal("ResumeRun() RunError = nil, want structured session-model error")
	}
	if got, want := event.RunError.Kind, RunErrorSessionModelUnavailable; got != want {
		t.Fatalf("ResumeRun() RunError.Kind = %q, want %q", got, want)
	}
	if got, want := event.RunError.ModelRef, "openai/deleted"; got != want {
		t.Fatalf("ResumeRun() RunError.ModelRef = %q, want %q", got, want)
	}
}

func TestRunReportsNoModelConfiguredWithoutSessionModelRef(t *testing.T) {
	a := &Agent{}

	event := receiveSingleRunEvent(t, a.Run(context.Background(), TextInput("new session")))
	if event.RunError == nil {
		t.Fatal("Run() RunError = nil, want structured no-model error")
	}
	if got, want := event.RunError.Kind, RunErrorNoModelConfigured; got != want {
		t.Fatalf("Run() RunError.Kind = %q, want %q", got, want)
	}
	if got := event.RunError.ModelRef; got != "" {
		t.Fatalf("Run() RunError.ModelRef = %q, want empty", got)
	}
}

func receiveSingleRunEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	event, ok := <-events
	if !ok {
		t.Fatal("event channel closed without status event")
	}
	if _, ok := <-events; ok {
		t.Fatal("event channel emitted more than one event")
	}
	return event
}

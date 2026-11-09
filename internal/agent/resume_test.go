package agent

import (
	"testing"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func TestCanResumeRunUsesIncompleteWorkingTail(t *testing.T) {
	tests := []struct {
		name string
		msgs []memory.Message
		want bool
	}{
		{name: "empty", want: false},
		{name: "last user", msgs: []memory.Message{memory.NewTextMessage(model.RoleUser, "hello")}, want: true},
		{name: "last tool", msgs: []memory.Message{memory.NewTextMessage(model.RoleUser, "hello"), {Role: model.RoleTool, TextContent: "done"}}, want: true},
		{name: "last assistant", msgs: []memory.Message{memory.NewTextMessage(model.RoleUser, "hello"), memory.NewTextMessage(model.RoleAssistant, "ok")}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			a := &Agent{working: memory.NewWorkingMemory()}
			for _, msg := range tt.msgs {
				a.working.AddMessage(msg)
			}
			if got := a.CanResumeRun(); got != tt.want {
				t.Fatalf("CanResumeRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

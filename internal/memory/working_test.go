package memory

import (
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestSetMessagesCopiesInputSlice(t *testing.T) {
	w := NewWorkingMemory()
	msgs := []model.Message{model.NewTextMessage(model.RoleUser, "first")}
	w.SetMessages(msgs)
	msgs[0] = model.NewTextMessage(model.RoleUser, "mutated")

	got := w.Messages()
	if len(got) != 1 || got[0].Text() != "first" {
		t.Fatalf("messages = %+v, want copied first message", got)
	}
}

package chat

import "testing"

func TestCurrentInputPolicy(t *testing.T) {
	tests := []struct {
		name  string
		state InputPolicyState
		want  InputPolicy
	}{
		{
			name:  "compact locks without cancel",
			state: InputPolicyState{Compacting: true, SpinnerView: "⠋", CompactRunning: "compacting", RespondingLabel: "responding"},
			want:  InputPolicy{Locked: true, Placeholder: "⠋ compacting"},
		},
		{
			name:  "loading uses status and cancel",
			state: InputPolicyState{Loading: true, StatusLabel: "waiting", RespondingLabel: "responding"},
			want:  InputPolicy{Locked: true, Placeholder: "waiting", AllowCancel: true},
		},
		{
			name:  "loading falls back to responding",
			state: InputPolicyState{Loading: true, RespondingLabel: "responding"},
			want:  InputPolicy{Locked: true, Placeholder: "responding", AllowCancel: true},
		},
		{
			name:  "ask choice locks without cancel",
			state: InputPolicyState{Loading: true, InteractionKind: InteractionAskUser, RespondingLabel: "responding"},
			want:  InputPolicy{Locked: true, Placeholder: "responding"},
		},
		{
			name:  "ask custom keeps composer editable",
			state: InputPolicyState{Loading: true, InteractionKind: InteractionAskUser, AskAllowCustom: true, RespondingLabel: "responding"},
			want:  InputPolicy{},
		},
		{
			name:  "guard keeps composer available to modal",
			state: InputPolicyState{Loading: true, InteractionKind: InteractionGuardConfirm, RespondingLabel: "responding"},
			want:  InputPolicy{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CurrentInputPolicy(tt.state)
			if got != tt.want {
				t.Fatalf("CurrentInputPolicy() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDisplayPlaceholder(t *testing.T) {
	if got := (InputPolicy{}).DisplayPlaceholder("responding", "cancel"); got != "" {
		t.Fatalf("unlocked DisplayPlaceholder() = %q, want empty", got)
	}
	if got := (InputPolicy{Locked: true, AllowCancel: true}).DisplayPlaceholder("responding", "cancel"); got != "responding · Esc cancel" {
		t.Fatalf("fallback DisplayPlaceholder() = %q", got)
	}
}

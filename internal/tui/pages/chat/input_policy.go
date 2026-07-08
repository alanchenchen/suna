package chat

// InputPolicy 描述输入框是否可编辑，以及锁定时展示的 placeholder。
// 该结构不依赖 root TUI，便于在没有 daemon/page 状态的情况下测试输入体验。
type InputPolicy struct {
	Locked      bool
	Placeholder string
	AllowCancel bool
}

// InputPolicyState 是推导输入锁定行为所需的最小运行态快照。
type InputPolicyState struct {
	Compacting      bool
	Loading         bool
	ObservingRun    bool
	InteractionKind InteractionKind
	AskAllowCustom  bool
	StatusLabel     string
	SpinnerView     string
	CompactRunning  string
	RespondingLabel string
	ObservingLabel  string
}

func CurrentInputPolicy(state InputPolicyState) InputPolicy {
	if state.Compacting {
		return InputPolicy{Locked: true, Placeholder: joinNonEmpty(state.SpinnerView, state.CompactRunning)}
	}
	if state.Loading && state.InteractionKind == InteractionNone {
		if state.ObservingRun {
			label := state.ObservingLabel
			if label == "" {
				label = state.RespondingLabel
			}
			return InputPolicy{Locked: true, Placeholder: joinNonEmpty(state.SpinnerView, label)}
		}
		label := state.StatusLabel
		if label == "" {
			label = state.RespondingLabel
		}
		return InputPolicy{Locked: true, Placeholder: joinNonEmpty(state.SpinnerView, label), AllowCancel: true}
	}
	if state.InteractionKind == InteractionAskUser && !state.AskAllowCustom {
		return InputPolicy{Locked: true, Placeholder: state.RespondingLabel}
	}
	return InputPolicy{}
}

func (p InputPolicy) DisplayPlaceholder(respondingLabel, cancelLabel string) string {
	if !p.Locked {
		return ""
	}
	label := p.Placeholder
	if label == "" {
		label = respondingLabel
	}
	if p.AllowCancel {
		label += " · Esc " + cancelLabel
	}
	return label
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

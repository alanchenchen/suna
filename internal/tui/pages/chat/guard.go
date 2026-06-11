package chat

import "time"

func (m *Model) EnqueueGuardConfirm(g *GuardConfirmView) {
	if g == nil {
		return
	}
	m.EnqueueInteraction(Interaction{Kind: InteractionGuardConfirm, ID: g.ID, Guard: g})
	m.Loading = false
	m.Phase = PhaseIdle
	m.PhaseStart = time.Time{}
}

func (m *Model) AdvanceGuardQueue() {
	if m.ActiveInteractionKind() == InteractionGuardConfirm {
		m.CompleteInteraction()
	}
	if m.ActiveInteractionKind() == InteractionGuardConfirm {
		m.Loading = false
		m.Phase = PhaseIdle
		m.PhaseStart = time.Time{}
	}
}

func (m *Model) ResumeToolPhase(now time.Time) {
	m.Loading = true
	m.Phase = PhaseTool
	m.PhaseStart = now
}

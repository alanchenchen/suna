package chat

import "github.com/alanchenchen/suna/internal/tui/components/attachment"

type InteractionKind int

const (
	InteractionNone InteractionKind = iota
	InteractionDiscardDraft
	InteractionGuardConfirm
	InteractionAskUser
	InteractionImagePasteConfirm
)

type AskUserView struct {
	ID          string
	Question    string
	Options     []string
	AllowCustom bool
	Cursor      int
}

type Interaction struct {
	Kind       InteractionKind
	ID         string
	Guard      *GuardConfirmView
	Ask        *AskUserView
	ImagePaste *attachment.PendingImagePaste
}

func (m *Model) ActiveInteractionKind() InteractionKind {
	if m.ActiveInteraction == nil {
		return InteractionNone
	}
	return m.ActiveInteraction.Kind
}

func (m *Model) HasBlockingInteraction() bool { return m.ActiveInteraction != nil }

func (m *Model) ActiveGuard() *GuardConfirmView {
	if m.ActiveInteraction == nil || m.ActiveInteraction.Kind != InteractionGuardConfirm {
		return nil
	}
	return m.ActiveInteraction.Guard
}

func (m *Model) ActiveAsk() *AskUserView {
	if m.ActiveInteraction == nil || m.ActiveInteraction.Kind != InteractionAskUser {
		return nil
	}
	return m.ActiveInteraction.Ask
}

func (m *Model) ActiveImagePaste() *attachment.PendingImagePaste {
	if m.ActiveInteraction == nil || m.ActiveInteraction.Kind != InteractionImagePasteConfirm {
		return nil
	}
	return m.ActiveInteraction.ImagePaste
}

func (m *Model) HasDiscardDraftConfirm() bool {
	return m.ActiveInteractionKind() == InteractionDiscardDraft
}

func (m *Model) EnqueueInteraction(i Interaction) {
	if i.Kind == InteractionNone {
		return
	}
	if m.ActiveInteraction == nil {
		m.activateInteraction(i)
		return
	}
	m.InteractionQueue = append(m.InteractionQueue, i)
}

func (m *Model) CompleteInteraction() {
	if len(m.InteractionQueue) == 0 {
		m.ActiveInteraction = nil
		return
	}
	next := m.InteractionQueue[0]
	copy(m.InteractionQueue, m.InteractionQueue[1:])
	m.InteractionQueue[len(m.InteractionQueue)-1] = Interaction{}
	m.InteractionQueue = m.InteractionQueue[:len(m.InteractionQueue)-1]
	m.activateInteraction(next)
}

func (m *Model) activateInteraction(i Interaction) {
	m.ActiveInteraction = &i
	switch i.Kind {
	case InteractionGuardConfirm:
		m.GuardCursor = 1
		m.GuardScroll = 0
	case InteractionAskUser:
		if i.Ask != nil {
			i.Ask.Cursor = 0
		}
	}
}

func (m *Model) RemoveQueuedInteractions(kind InteractionKind) {
	if len(m.InteractionQueue) == 0 {
		return
	}
	kept := m.InteractionQueue[:0]
	for _, i := range m.InteractionQueue {
		if i.Kind != kind {
			kept = append(kept, i)
		}
	}
	for i := len(kept); i < len(m.InteractionQueue); i++ {
		m.InteractionQueue[i] = Interaction{}
	}
	m.InteractionQueue = kept
}

func (m *Model) CancelActiveInteraction() { m.CompleteInteraction() }

func (m *Model) RequestDiscardDraft() {
	m.EnqueueInteraction(Interaction{Kind: InteractionDiscardDraft, ID: "discard_draft"})
}

func (m *Model) CancelDiscardDraft() {
	if m.ActiveInteractionKind() == InteractionDiscardDraft {
		m.CancelActiveInteraction()
		return
	}
	m.RemoveQueuedInteractions(InteractionDiscardDraft)
}

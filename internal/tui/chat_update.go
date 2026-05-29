package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

func (t *TUI) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width = m.Width
		t.height = m.Height
		t.ready = true
		t.layoutChat()
		t.syncContent()
		return t, nil

	case tea.KeyPressMsg:
		ks := m.String()
		if t.confirmDiscardDraft {
			return t.updateDiscardDraftConfirm(ks, msg)
		}
		if t.pendingGuard != nil {
			return t.updateGuardConfirm(ks)
		}
		if t.modelPickerOpen {
			return t.updateModelPicker(ks)
		}
		if t.pendingImagePaste != nil {
			cmd := t.updatePendingImagePaste(ks)
			t.syncContent()
			return t, cmd
		}
		if t.attachmentMode || t.attachmentDelete {
			if t.updateAttachmentMode(ks) {
				t.syncContent()
				return t, nil
			}
		}
		switch {
		case ks == "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case ks == "?":
			t.showHelp = !t.showHelp
			return t, nil
		case ks == "enter":
			t.confirmDiscardDraft = false
			if len(t.cmdSuggestions) > 0 {
				cmd := t.acceptCommandSuggestion()
				if cmd != nil {
					return t, cmd
				}
				return t, t.ta.Focus()
			}
			if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 && t.ta.Value() == "" {
				idx := t.pendingAskCursor
				if idx >= 0 && idx < len(t.pendingAskOptions) {
					answer := t.pendingAskOptions[idx]
					askID := t.pendingAskID
					t.pendingAskID = ""
					t.pendingAskOptions = nil
					t.appendNonToolMessage(chatMsg{role: "user", content: answer})
					t.startLLMWait()
					t.syncContent()
					return t, tea.Batch(t.askReplyCmd(askID, answer), t.sp.Tick)
				}
			}
			if !t.loading {
				return t, t.handleSend()
			}
			return t, nil
		case ks == "shift+enter":
			t.confirmDiscardDraft = false
			t.ta.InsertString("\n")
			t.layoutChat()
			return t, nil
		case ks == "esc":
			if t.showToolDetail {
				t.showToolDetail = false
				t.syncContent()
				return t, nil
			}
			if t.showHelp {
				t.showHelp = false
				return t, nil
			}
			if t.loading {
				t.resetPhase()
				t.appendNonToolMessage(chatMsg{role: "system", content: t.i18n.T("status.cancelled")})
				t.syncContent()
				return t, tea.Batch(t.cancelCmd(), t.ta.Focus())
			}
			if !t.hasDraft() {
				t.mode = "welcome"
				t.refreshDaemonStatus()
				t.initWelcomeList()
				return t, nil
			}
			t.confirmDiscardDraft = true
			t.layoutChat()
			return t, t.ta.Focus()
		case ks == "ctrl+t":
			t.showToolDetail = !t.showToolDetail
			t.toolDetailScroll = 0
			if t.showToolDetail && t.selectedToolID == "" {
				ids := t.visibleToolIDs()
				if len(ids) > 0 {
					t.selectedToolID = ids[0]
				}
			}
			t.syncContent()
			return t, nil
		case ks == "ctrl+r":
			t.showReasoningDetail = !t.showReasoningDetail
			t.syncContent()
			return t, nil
		case ks == "pgup":
			if t.showToolDetail {
				t.scrollToolDetailOverlay(-max(1, t.toolDetailPageStep()))
				t.syncContent()
			} else {
				t.vp.HalfPageUp()
			}
			return t, nil
		case ks == "pgdown":
			if t.showToolDetail {
				t.scrollToolDetailOverlay(max(1, t.toolDetailPageStep()))
				t.syncContent()
			} else {
				t.vp.HalfPageDown()
			}
			return t, nil
		case ks == "up":
			if t.showToolDetail {
				t.moveSelectedTool(-1)
				t.syncContent()
			} else if len(t.cmdSuggestions) > 0 {
				if t.cmdSuggestionIdx > 0 {
					t.cmdSuggestionIdx--
				}
			} else if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 {
				if t.pendingAskCursor > 0 {
					t.pendingAskCursor--
				}
				t.syncContent()
			} else if t.updateAttachmentMode(ks) {
				t.syncContent()
			}
			return t, nil
		case ks == "down":
			if t.showToolDetail {
				t.moveSelectedTool(1)
				t.syncContent()
			} else if len(t.cmdSuggestions) > 0 {
				if t.cmdSuggestionIdx < len(t.cmdSuggestions)-1 {
					t.cmdSuggestionIdx++
				}
			} else if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 {
				if t.pendingAskCursor < len(t.pendingAskOptions)-1 {
					t.pendingAskCursor++
				}
				t.syncContent()
			} else if t.updateAttachmentMode(ks) {
				t.syncContent()
			}
			return t, nil
		}

	case spinner.TickMsg:
		if t.loading {
			var cmd tea.Cmd
			t.sp, cmd = t.sp.Update(msg)
			t.syncContent()
			return t, cmd
		}
		return t, nil

	case tea.PasteMsg:
		cmd := t.handlePaste(m.Content)
		t.syncContent()
		return t, cmd

	case localNotification:
		t.handleLocalNotification(m)
		t.syncContent()
		if t.loading {
			return t, func() tea.Msg { return t.sp.Tick() }
		}
		return t, nil

	case tea.MouseMsg:
		if t.pendingGuard != nil {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				if mm.Mouse().Button == tea.MouseWheelUp {
					t.scrollGuardOverlay(-3)
				} else if mm.Mouse().Button == tea.MouseWheelDown {
					t.scrollGuardOverlay(3)
				}
				t.syncContent()
			}
			return t, nil
		}
		if t.showToolDetail {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				if mm.Mouse().Button == tea.MouseWheelUp {
					t.scrollToolDetailOverlay(-3)
				} else if mm.Mouse().Button == tea.MouseWheelDown {
					t.scrollToolDetailOverlay(3)
				}
				t.syncContent()
			}
			return t, nil
		}
		var cmd tea.Cmd
		t.vp, cmd = t.vp.Update(msg)
		return t, cmd
	}

	if t.confirmDiscardDraft {
		t.confirmDiscardDraft = false
	}

	var cmd tea.Cmd
	t.ta, cmd = t.ta.Update(msg)

	t.updateCmdSuggestionState()
	t.layoutChat()

	return t, cmd
}

func (t *TUI) updateDiscardDraftConfirm(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ks {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "enter":
		t.discardDraft()
		return t, t.ta.Focus()
	case "esc":
		t.confirmDiscardDraft = false
		t.layoutChat()
		return t, t.ta.Focus()
	}

	t.confirmDiscardDraft = false
	var cmd tea.Cmd
	t.ta, cmd = t.ta.Update(msg)
	t.updateCmdSuggestionState()
	t.layoutChat()
	return t, cmd
}

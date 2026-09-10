package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		switch key.String() {
		case "esc":
			m.form = nil
			m.formError = ""
			if m.reloadAfterForm {
				m.reloadAfterForm = false
				return m, m.refreshCmd(refreshWorkspace)
			}
			return m, nil
		case "ctrl+s":
			return m.beginFormSubmit()
		case "tab":
			if inputCmd, accepted := m.acceptFormSuggestion(msg); accepted {
				return m, inputCmd
			}
			return m, m.moveFormFocus(1)
		case "enter":
			if m.form.focus == descriptionField {
				return m.beginFormSubmit()
			}
			return m, m.moveFormFocus(1)
		case "down":
			return m, m.moveFormFocus(1)
		case "shift+tab", "up":
			return m, m.moveFormFocus(-1)
		case "ctrl+p":
			if !m.form.editing {
				return m.changePlacement(1)
			}
		case "ctrl+o":
			if !m.form.editing && m.form.placement != manualPlacement {
				m.form.overtime = !m.form.overtime
				m.formError = ""
				return m, m.schedulePreview()
			}
		case "ctrl+l":
			if !m.form.editing && m.form.placement != manualPlacement {
				m.form.noLunch = !m.form.noLunch
				m.formError = ""
				return m, m.schedulePreview()
			}
		}
		if m.form.focus == placementField {
			switch key.String() {
			case "left", "h":
				return m.changePlacement(-1)
			case "right", "l", " ":
				return m.changePlacement(1)
			}
			return m, nil
		}
	}
	inputIndex, ok := inputIndexForField(m.form.focus)
	if !ok {
		return m, nil
	}
	before := m.form.inputs[inputIndex].Value()
	var inputCmd tea.Cmd
	input := m.form.inputs[inputIndex]
	input, inputCmd = input.Update(msg)
	m.form.inputs[inputIndex] = input
	cmds := []tea.Cmd{inputCmd}
	if before != input.Value() {
		m.formError = ""
		if m.form.focus == issueField {
			m.invalidateDescriptionSuggestions()
		}
		if !m.form.editing && m.previewInputField(m.form.focus) {
			cmds = append(cmds, m.schedulePreview())
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) beginFormSubmit() (tea.Model, tea.Cmd) {
	m.normalizeDurationInput()
	if m.form.stale {
		m.overlay = reloadOverlay
		return m, nil
	}
	if !m.form.editing && m.form.placement != manualPlacement {
		if m.form.previewLoading {
			m.formError = "wait for the placement preview"
			return m, nil
		}
		if len(m.form.previewRecords) == 0 {
			m.formError = firstText(m.form.previewError, "enter a valid issue and duration to preview placement")
			return m, nil
		}
	}
	operation := "worklogs.add"
	if m.form.editing {
		operation = "worklogs.update"
	}
	summary := tuiActivitySummary("issue="+strings.ToUpper(strings.TrimSpace(m.form.inputs[issueInput].Value())), durationSummary(m.form.inputs[durationInput].Value()), "date="+m.selectedDate.Format("2006-01-02"), "placement="+strings.ToLower(m.form.placement.String()))
	return m, m.beginActivity(operation, summary, m.submitFormCmd())
}

func (m *model) acceptFormSuggestion(msg tea.Msg) (tea.Cmd, bool) {
	index, explicit := 0, ""
	switch m.form.focus {
	case issueField:
		index = issueInput
		explicit = m.form.suggestedIssue
	case startField:
		if m.form.editing || m.form.placement != manualPlacement {
			return nil, false
		}
		index = startInput
		explicit = m.form.suggestedStart
	case descriptionField:
		index = descriptionInput
		explicit = m.form.suggestedDescription
	default:
		return nil, false
	}

	input := m.form.inputs[index]
	before := input.Value()
	if before == "" && explicit != "" {
		input.SetValue(explicit)
	} else if input.CurrentSuggestion() != "" && input.CurrentSuggestion() != before {
		var inputCmd tea.Cmd
		input, inputCmd = input.Update(msg)
		m.form.inputs[index] = input
		if m.form.focus == issueField {
			m.invalidateDescriptionSuggestions()
			return tea.Batch(inputCmd, m.schedulePreview()), true
		}
		return inputCmd, true
	} else {
		return nil, false
	}

	m.form.inputs[index] = input
	m.formError = ""
	if m.form.focus == issueField {
		m.invalidateDescriptionSuggestions()
		return m.schedulePreview(), true
	}
	if m.form.focus == startField {
		return m.schedulePreview(), true
	}
	return nil, true
}

func (m model) handleMutationResult(msg mutationResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if msg.kind == "preset-source" && errors.Is(msg.err, presets.ErrConflict) && m.form != nil {
			m.form.sourcePresetID = ""
			m.form.sourcePresetRevision = 0
			m.formError = "preset changed or was deleted; review this detached draft and save again"
			m.setNotice(noticeError, m.formError)
			return m, m.startPresetLoad()
		}
		if errors.Is(msg.err, worklogs.ErrPlacementChanged) {
			m.formError = "availability changed; review the updated placement"
			m.setNotice(noticeError, m.formError)
			return m, m.schedulePreview()
		}
		var validation worklogs.ValidationError
		if errors.As(msg.err, &validation) && validation.Conflict != nil {
			if msg.kind == "restore" || msg.kind == "restore-day" {
				m.setNotice(noticeError, "restore conflict; no worklogs were restored")
				return m, m.refreshCmd(refreshWorkspace)
			}
			if m.form != nil && !m.form.editing && m.form.placement != manualPlacement {
				m.formError = "availability changed; recalculating placement"
				return m, m.schedulePreview()
			}
			m.conflict = validation.Conflict
			m.overlay = forceOverlay
			return m, nil
		}
		if errors.Is(msg.err, worklogs.ErrConflict) {
			if msg.kind == "restore" || msg.kind == "restore-day" {
				m.setNotice(noticeWarning, "trash changed externally; nothing was restored")
				return m, m.refreshCmd(refreshWorkspace)
			}
			if m.form != nil {
				m.form.stale = true
				m.overlay = reloadOverlay
			} else {
				m.setNotice(noticeWarning, "worklog changed externally; list reloaded")
				return m, m.refreshCmd(refreshWorkspace)
			}
			return m, nil
		}
		m.formError = msg.err.Error()
		m.setNotice(noticeError, msg.err.Error())
		return m, nil
	}
	m.form = nil
	m.reloadAfterForm = false
	m.formError = ""
	m.overlay = noOverlay
	m.setNotice(noticeSuccess, msg.kind+" succeeded")
	if msg.kind == "delete-day" {
		m.setNotice(noticeSuccess, fmt.Sprintf("deleted %s from %s", pluralizeWorklogs(msg.deleted), m.selectedDate.Format("2006-01-02")))
	}
	if msg.kind == "restore" {
		m.setNotice(noticeSuccess, "restored worklog from trash")
	}
	if msg.kind == "restore-day" {
		m.setNotice(noticeSuccess, fmt.Sprintf("restored %s on %s", pluralizeWorklogs(msg.deleted), m.selectedDate.Format("2006-01-02")))
	}
	if msg.kind == "add" && len(msg.records) > 0 {
		m.lastAddedIssue = msg.records[0].IssueKey
		m.selectAfterID = msg.records[0].ID
		if len(msg.records) == 1 {
			local := msg.records[0].StartedAtUTC.In(m.workspace.cfg.Location)
			m.setNotice(noticeSuccess, "added worklog at "+local.Format("15:04"))
		} else {
			m.setNotice(noticeSuccess, fmt.Sprintf("added %d worklogs", len(msg.records)))
		}
	}
	return m, m.consumeTrackerCmd(refreshForMutation(msg.kind) | msg.refresh)
}

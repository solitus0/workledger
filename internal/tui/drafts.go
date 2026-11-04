package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) draftValues() []string {
	var values []string
	switch {
	case m.form != nil:
		for _, input := range m.form.inputs {
			values = append(values, input.Value())
		}
		values = append(values, m.form.placement.String(), fmt.Sprint(m.form.overtime), fmt.Sprint(m.form.noLunch))
	case m.presetForm != nil:
		for _, input := range m.presetForm.inputs {
			values = append(values, input.Value())
		}
	case m.issueForm != nil:
		for _, input := range m.issueForm.inputs {
			values = append(values, input.Value())
		}
	case m.planForm != nil:
		values = []string{m.planForm.direction, fmt.Sprint(m.planForm.window), fmt.Sprint(m.planForm.targetIndex), fmt.Sprint(m.planForm.profileIndex), m.planForm.dateInputs[0].Value(), m.planForm.dateInputs[1].Value()}
	}
	return values
}
func (m *model) rememberDraft() {
	m.draftBaseline = m.draftValues()
	m.draftError = ""
}

func (m model) cancelDraft() (tea.Model, tea.Cmd) {
	if !slices.Equal(m.draftBaseline, m.draftValues()) {
		m.overlay = discardDraftOverlay
		return m, nil
	}
	return m.closeDraft()
}
func (m model) closeDraft() (tea.Model, tea.Cmd) {
	stale := m.reloadAfterForm || (m.form != nil && m.form.stale) || (m.presetForm != nil && m.presetForm.stale) || (m.issueForm != nil && m.issueForm.stale)
	m.form, m.presetForm, m.issueForm, m.planForm = nil, nil, nil, nil
	m.formError, m.presetFormError, m.issueFormError = "", "", ""
	m.draftBaseline = nil
	m.reloadAfterForm = false
	m.overlay = noOverlay
	m.clearNotice()
	if stale {
		return m, m.refreshCmd(refreshWorkspace)
	}
	return m, nil
}
func (m model) savingLabel() string {
	switch {
	case m.draftReloading:
		return "Reloading latest record…"
	case m.form != nil && m.form.saving:
		return "Saving worklog…"
	case m.presetForm != nil && m.presetForm.saving:
		return "Saving preset…"
	case m.issueForm != nil && m.issueForm.saving:
		return "Saving issue…"
	case m.worklogAssignment != nil && m.worklogAssignment.saving:
		return "Saving issue assignments…"
	case m.epicAssignment != nil && m.epicAssignment.saving:
		return "Saving epic assignments…"
	case m.move != nil && m.move.saving:
		return "Saving position…"
	}
	return ""
}

type draftReloadedMsg struct {
	kind    string
	worklog worklogs.LocalWorklog
	preset  presets.Preset
	issue   worklogs.IssueMetadata
	err     error
}

func (m *model) reloadDraftCmd() tea.Cmd {
	m.draftReloading = true
	ws, ctx := m.workspace, m.ctx
	kind, id := "", ""
	switch {
	case m.form != nil:
		kind, id = "worklog", m.form.id
	case m.presetForm != nil:
		kind, id = "preset", m.presetForm.id
	case m.issueForm != nil:
		kind, id = "issue", m.issueForm.inputs[0].Value()
	}
	return func() tea.Msg {
		result := draftReloadedMsg{kind: kind}
		switch kind {
		case "worklog":
			rows, err := ws.worklogs.LookupIDs(ctx, []string{id})
			result.err = err
			if err == nil {
				if len(rows) == 1 {
					result.worklog = rows[0]
				} else {
					result.err = errors.New("Worklog no longer exists. Cancel to keep or discard the draft.")
				}
			}
		case "preset":
			result.preset, result.err = ws.presets.LookupID(ctx, id)
		case "issue":
			rows, err := ws.issues.SearchIssues(ctx, "", id, "")
			result.err = err
			if err == nil {
				result.err = errors.New("Issue no longer exists. Cancel to keep or discard the draft.")
				for _, row := range rows {
					if row.IssueKey == id {
						result.issue = row
						result.err = nil
						break
					}
				}
			}
		default:
			result.err = errors.New("No editable record to reload")
		}
		return result
	}
}
func (m model) applyDraftReload(msg draftReloadedMsg) (tea.Model, tea.Cmd) {
	if !m.draftReloading {
		return m, nil
	}
	m.draftReloading = false
	if msg.err != nil {
		m.openDetails("Unable to reload draft", msg.err.Error()+"\n\nYour draft is retained.")
		return m, nil
	}
	switch msg.kind {
	case "worklog":
		m.openEditForm(msg.worklog)
	case "preset":
		m.openPresetForm(&msg.preset)
	case "issue":
		m.openIssueForm(&msg.issue)
	}
	m.overlay = noOverlay
	m.clearNotice()
	return m, nil
}

func validationFeedback(err error) (field, message string) {
	message = err.Error()
	var worklog worklogs.ValidationError
	var preset presets.ValidationError
	if errors.As(err, &worklog) && len(worklog.Issues) > 0 {
		field, message = worklog.Issues[0].Field, worklog.Issues[0].Message
	}
	if errors.As(err, &preset) && len(preset.Issues) > 0 {
		field, message = preset.Issues[0].Field, preset.Issues[0].Message
	}
	if field == "duration" && strings.Contains(message, "Go duration") {
		message = "Enter a duration such as 1h30m or 90m."
	}
	return field, message
}

func validationDetails(err error) string {
	var lines []string
	var worklog worklogs.ValidationError
	var preset presets.ValidationError
	if errors.As(err, &worklog) {
		for _, issue := range worklog.Issues {
			lines = append(lines, issue.Field+": "+issue.Message)
		}
	}
	if errors.As(err, &preset) {
		for _, issue := range preset.Issues {
			lines = append(lines, issue.Field+": "+issue.Message)
		}
	}
	return firstText(strings.Join(lines, "\n"), err.Error())
}

func (m *model) worklogValidation(err error) {
	m.draftError = validationDetails(err)
	field, message := validationFeedback(err)
	m.formError = message
	if m.form == nil {
		return
	}
	target := m.form.focus
	switch field {
	case "issue", "issue_key":
		target = issueField
	case "started", "started_utc", "start":
		target = startField
	case "duration":
		target = durationField
	case "description":
		target = descriptionField
	}
	m.form.errorField = target
	m.focusForm(target)
	m.clearNotice()
}

func (m model) placementDetails() string {
	lines := []string{m.selectedDate.Format("Monday, 2006-01-02"), "Issue " + worklogs.DisplayIssueKey(m.form.inputs[issueInput].Value()), "Duration " + m.form.inputs[durationInput].Value(), "Start " + emptyDash(m.form.inputs[startInput].Value()), "Placement " + m.form.placement.String(), ""}
	for i, row := range m.form.previewRecords {
		start := row.StartedAtUTC.In(m.workspace.cfg.Location)
		end := start.Add(time.Duration(row.DurationSeconds) * time.Second)
		lines = append(lines, fmt.Sprintf("%d. %s–%s · %s", i+1, start.Format("15:04"), end.Format("15:04"), formatDuration(row.DurationSeconds)))
	}
	if len(m.form.previewRecords) == 0 {
		lines = append(lines, m.renderPlacementPreview())
	}
	lines = append(lines, "", m.renderProjectedDayBalance())
	if m.form.previewConflict != "" {
		lines = append(lines, "", m.form.previewConflict)
	}
	if context := m.renderIssueContext(); context != "" {
		lines = append(lines, "", context)
	}
	lines = append(lines, "", "Description", m.form.inputs[descriptionInput].Value())
	return strings.Join(lines, "\n")
}

func (m model) draftDetails() string {
	if m.form != nil {
		return m.placementDetails() + "\n\n" + m.draftErrorDetails(m.formError)
	}
	var labels, values []string
	message := ""
	switch {
	case m.presetForm != nil:
		labels = []string{"Name", "Issue", "Start time", "Duration", "Description"}
		for _, input := range m.presetForm.inputs {
			values = append(values, input.Value())
		}
		message = m.presetFormError
	case m.issueForm != nil:
		labels = []string{"Issue", "Title", "Max estimate", "Epic"}
		for _, input := range m.issueForm.inputs {
			values = append(values, input.Value())
		}
		message = m.issueFormError
	case m.planForm != nil:
		return m.renderPlanForm() + "\n\n" + m.planForm.errorText
	}
	var lines []string
	for i, label := range labels {
		lines = append(lines, label, emptyDash(values[i]), "")
	}
	if message != "" {
		lines = append(lines, "Validation", m.draftErrorDetails(message))
	}
	return strings.Join(lines, "\n")
}

func (m model) draftErrorDetails(message string) string {
	if message == "" {
		return ""
	}
	return firstText(m.draftError, message)
}

package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/solitus0/workledger/internal/presets"
)

type presetIssueSuggestionsResultMsg struct {
	items []string
	err   error
}

type presetDescriptionSuggestionsResultMsg struct {
	issue string
	items []string
	err   error
}

func (m *model) startPresetLoad() tea.Cmd {
	if m.workspace == nil || m.workspace.presets == nil {
		return nil
	}
	m.presetGen++
	m.presetLoading = true
	return m.presetCmd(m.presetGen)
}

func (m model) presetCmd(generation int) tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		if ws == nil || ws.presets == nil {
			return presetResultMsg{generation: generation, items: []presets.Preset{}}
		}
		items, err := ws.presets.List(ctx, "", presetSuggestionLimit)
		return presetResultMsg{generation: generation, items: items, err: err}
	}
}

func (m model) selectedPreset() *presets.Preset {
	if m.presetSelected < 0 || m.presetSelected >= len(m.presets) {
		return nil
	}
	return &m.presets[m.presetSelected]
}

func (m model) selectedPresetID() string {
	if item := m.selectedPreset(); item != nil {
		return item.ID
	}
	return ""
}

func (m *model) restorePresetSelection(id string) {
	m.presetSelected = clampIndex(m.presetSelected, len(m.presets))
	for index := range m.presets {
		if m.presets[index].ID == id {
			m.presetSelected = index
			return
		}
	}
}

func (m model) updatePresets(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.presetSelected = max(0, m.presetSelected-1)
	case "down", "j":
		m.presetSelected = min(max(0, len(m.presets)-1), m.presetSelected+1)
	case "a":
		m.openPresetForm(nil)
		return m, m.presetIssueSuggestionsCmd()
	case "e":
		if item := m.selectedPreset(); item != nil {
			copy := *item
			m.openPresetForm(&copy)
			return m, m.presetIssueSuggestionsCmd()
		}
	case "D":
		if item := m.selectedPreset(); item != nil {
			copy := *item
			m.presetDelete = &copy
			m.overlay = deletePresetOverlay
		}
	case "r":
		return m, m.beginActivity("presets.refresh", "", m.startPresetLoad())
	}
	return m, nil
}

func (m *model) openPresetForm(item *presets.Preset) {
	values := []string{"", "", "", "", ""}
	state := presetFormState{}
	if item != nil {
		values = []string{item.Name, item.IssueKey, item.StartTime, formatDurationInput(item.DurationSeconds), item.Description}
		state.editing, state.name, state.revision = true, item.Name, item.Revision
	}
	placeholders := []string{"daily-standup", "PROJ-123", "09:00", "15m", "Daily standup"}
	for index := range state.inputs {
		input := textinput.New()
		input.Prompt = ""
		input.Placeholder = placeholders[index]
		input.SetValue(values[index])
		input.SetWidth(48)
		if m.deps.noColor {
			input.SetVirtualCursor(false)
			input.SetStyles(textinput.Styles{})
		} else {
			input.SetStyles(m.colors.textInputStyles())
		}
		state.inputs[index] = input
	}
	state.inputs[0].CharLimit = 64
	state.inputs[1].CharLimit = 64
	state.inputs[1].ShowSuggestions = true
	state.inputs[2].CharLimit = 5
	state.inputs[3].CharLimit = 24
	state.inputs[4].CharLimit = 240
	state.inputs[4].ShowSuggestions = true
	state.inputs[0].Focus()
	m.presetForm = &state
	m.presetFormError = ""
	m.resizePresetInputs()
}

func (m *model) resizePresetInputs() {
	if m.presetForm == nil {
		return
	}
	width := max(20, m.width-30-12)
	for index := range m.presetForm.inputs {
		m.presetForm.inputs[index].SetWidth(width)
	}
}

func (m *model) restylePresetInputs() {
	if m.deps.noColor {
		return
	}
	styles := m.colors.textInputStyles()
	if m.presetForm != nil {
		for index := range m.presetForm.inputs {
			m.presetForm.inputs[index].SetStyles(styles)
		}
	}
	if m.presetPicker != nil {
		m.presetPicker.input.SetStyles(styles)
	}
}

func (m model) updatePresetForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		switch key.String() {
		case "esc":
			m.presetForm = nil
			m.presetFormError = ""
			return m, nil
		case "ctrl+s":
			return m.beginPresetFormSubmit()
		case "tab":
			input := m.presetForm.inputs[m.presetForm.focus]
			if input.CurrentSuggestion() != "" && input.CurrentSuggestion() != input.Value() {
				input, _ = input.Update(msg)
				m.presetForm.inputs[m.presetForm.focus] = input
				return m, nil
			}
			return m.movePresetFormFocus(1)
		case "enter":
			if m.presetForm.focus == 4 {
				return m.beginPresetFormSubmit()
			}
			return m.movePresetFormFocus(1)
		case "down":
			return m.movePresetFormFocus(1)
		case "shift+tab", "up":
			return m.movePresetFormFocus(-1)
		}
	}
	index := m.presetForm.focus
	before := m.presetForm.inputs[index].Value()
	input, cmd := m.presetForm.inputs[index].Update(msg)
	m.presetForm.inputs[index] = input
	if before != input.Value() {
		m.presetFormError = ""
		if index == 1 {
			m.presetForm.inputs[4].SetSuggestions(nil)
		}
	}
	return m, cmd
}

func (m model) movePresetFormFocus(delta int) (tea.Model, tea.Cmd) {
	current := m.presetForm.focus
	if current == 3 {
		input := m.presetForm.inputs[3]
		input.SetValue(normalizeTUIDuration(input.Value()))
		m.presetForm.inputs[3] = input
	}
	for index := range m.presetForm.inputs {
		m.presetForm.inputs[index].Blur()
	}
	m.presetForm.focus = (current + delta + len(m.presetForm.inputs)) % len(m.presetForm.inputs)
	m.presetForm.inputs[m.presetForm.focus].Focus()
	if current == 1 {
		return m, m.presetDescriptionSuggestionsCmd()
	}
	return m, nil
}

func (m model) beginPresetFormSubmit() (tea.Model, tea.Cmd) {
	if m.presetForm.stale {
		m.presetFormError = "preset changed externally; cancel and reopen before saving"
		return m, nil
	}
	operation := "presets.create"
	if m.presetForm.editing {
		operation = "presets.update"
	}
	summary := tuiActivitySummary("name="+strings.TrimSpace(m.presetForm.inputs[0].Value()), "issue="+strings.ToUpper(strings.TrimSpace(m.presetForm.inputs[1].Value())), durationSummary(m.presetForm.inputs[3].Value()))
	return m, m.beginActivity(operation, summary, m.submitPresetFormCmd())
}

func (m model) submitPresetFormCmd() tea.Cmd {
	form := *m.presetForm
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui preset save"); err != nil {
			return presetMutationResultMsg{kind: "save preset", err: err}
		}
		values := make([]string, len(form.inputs))
		for index := range form.inputs {
			values[index] = form.inputs[index].Value()
		}
		if !form.editing {
			item, err := ws.presets.Create(ctx, ws.cfg, presets.CreateInput{Name: values[0], IssueKey: values[1], StartTime: values[2], Duration: normalizeTUIDuration(values[3]), Description: values[4]})
			return presetMutationResultMsg{kind: "added preset", item: item, err: err}
		}
		name, issue, start, duration, description := values[0], values[1], values[2], normalizeTUIDuration(values[3]), values[4]
		item, err := ws.presets.Update(ctx, ws.cfg, form.name, presets.PatchInput{Name: &name, IssueKey: &issue, StartTime: &start, Duration: &duration, Description: &description, ExpectedRevision: form.revision})
		return presetMutationResultMsg{kind: "updated preset", item: item, err: err}
	}
}

func (m model) deletePresetCmd(item presets.Preset) tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui preset delete"); err != nil {
			return presetMutationResultMsg{kind: "deleted preset", err: err}
		}
		_, err := ws.presets.Delete(ctx, item.Name, item.Revision)
		return presetMutationResultMsg{kind: "deleted preset", item: item, err: err}
	}
}

func (m model) handlePresetMutationResult(msg presetMutationResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if errors.Is(msg.err, presets.ErrConflict) {
			if m.presetForm != nil {
				m.presetForm.stale = true
				m.presetFormError = "preset changed externally; cancel and reopen before saving"
			} else {
				m.setNotice(noticeWarning, "preset changed externally; list reloaded")
			}
			return m, m.startPresetLoad()
		}
		m.presetFormError = msg.err.Error()
		m.setNotice(noticeError, msg.err.Error())
		return m, nil
	}
	m.presetForm = nil
	m.presetFormError = ""
	m.presetDelete = nil
	m.overlay = noOverlay
	m.setNotice(noticeSuccess, msg.kind+" succeeded")
	return m, m.consumeTrackerCmd(refreshPresets)
}

func (m model) presetIssueSuggestionsCmd() tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		items, err := ws.worklogs.ListKnownIssueKeys(ctx, "", issueSuggestionLimit)
		return presetIssueSuggestionsResultMsg{items: items, err: err}
	}
}

func (m model) presetDescriptionSuggestionsCmd() tea.Cmd {
	issue := strings.ToUpper(strings.TrimSpace(m.presetForm.inputs[1].Value()))
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		items, err := ws.worklogs.ListRecentDescriptions(ctx, issue, descriptionSuggestionLimit)
		return presetDescriptionSuggestionsResultMsg{issue: issue, items: items, err: err}
	}
}

func (m *model) openPresetPicker() tea.Cmd {
	if len(m.presets) == 0 {
		m.setNotice(noticeInfo, "no presets available; create one in Presets")
		return nil
	}
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Type preset name"
	input.ShowSuggestions = true
	input.SetWidth(42)
	m.presetPicker = &presetPickerState{input: input, items: append([]presets.Preset(nil), m.presets...)}
	m.filterPresetPicker()
	return nil
}

func (m *model) filterPresetPicker() {
	if m.presetPicker == nil {
		return
	}
	prefix := strings.ToLower(strings.TrimSpace(m.presetPicker.input.Value()))
	items := make([]presets.Preset, 0)
	for _, item := range m.presets {
		if prefix == "" || strings.HasPrefix(strings.ToLower(item.Name), prefix) {
			items = append(items, item)
		}
	}
	m.presetPicker.items = items
	suggestions := make([]string, 0, len(items))
	for _, item := range items {
		suggestions = append(suggestions, item.Name)
	}
	m.presetPicker.input.SetSuggestions(suggestions)
	maximum := len(items) - 1
	m.presetPicker.selected = min(maximum, max(0, m.presetPicker.selected))
}

func (m model) updatePresetPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		switch key.String() {
		case "ctrl+f":
			m.presetPicker.searching = true
			return m, m.presetPicker.input.Focus()
		case "esc":
			if m.presetPicker.searching {
				m.presetPicker.searching = false
				m.presetPicker.input.Blur()
				m.presetPicker.input.SetValue("")
				m.presetPicker.selected = 0
				m.filterPresetPicker()
				return m, nil
			}
			m.presetPicker = nil
			return m, nil
		case "up":
			m.presetPicker.selected = max(0, m.presetPicker.selected-1)
			return m, nil
		case "down":
			maximum := len(m.presetPicker.items) - 1
			m.presetPicker.selected = min(max(0, maximum), m.presetPicker.selected+1)
			return m, nil
		case "tab":
			if !m.presetPicker.searching {
				return m, nil
			}
			input, cmd := m.presetPicker.input.Update(msg)
			m.presetPicker.input = input
			m.filterPresetPicker()
			return m, cmd
		case "enter":
			index := m.presetPicker.selected
			if index >= 0 && index < len(m.presetPicker.items) {
				item := m.presetPicker.items[index]
				m.presetPicker = nil
				return m, m.openAddFromPreset(item)
			}
			return m, nil
		}
	}
	if !m.presetPicker.searching {
		return m, nil
	}
	before := m.presetPicker.input.Value()
	input, cmd := m.presetPicker.input.Update(msg)
	m.presetPicker.input = input
	if before != input.Value() {
		m.presetPicker.selected = 0
		m.filterPresetPicker()
	}
	return m, cmd
}

func (m *model) openAddFromPreset(item presets.Preset) tea.Cmd {
	form := m.newForm(false, "", 0, item.IssueKey, item.StartTime, formatDurationInput(item.DurationSeconds), item.Description)
	form.placement = manualPlacement
	form.sourcePresetID = item.ID
	form.sourcePresetRevision = item.Revision
	m.form = &form
	m.formError = ""
	m.resizeForm()
	m.issueSuggestionGen++
	m.descriptionSuggestionGen++
	return tea.Batch(m.issueSuggestionsCmd(m.issueSuggestionGen), m.descriptionSuggestionsCmd(), m.schedulePreview())
}

func presetSummary(item presets.Preset) string {
	return fmt.Sprintf("%s · %s · %s", item.IssueKey, item.StartTime, formatDuration(item.DurationSeconds))
}

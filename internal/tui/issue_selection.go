package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

type epicAssignmentState struct {
	input         textinput.Model
	keys          []string
	hidden        int
	selected      int
	saving, stale bool
	err           string
}

// A range is a live preview over the current ordered view. Its baseline lets a
// shrinking range restore earlier marks rather than leaving a trail behind it.
type issueRangeState struct {
	anchor string
	base   map[string]bool
	unset  bool
}

func (m *model) toggleIssueRange(unset bool) {
	if m.issueRange != nil {
		m.issueRange = nil
		return
	}
	if key := m.selectedIssueKey(); key != "" {
		m.issueRange = &issueRangeState{anchor: key, base: maps.Clone(m.issueMarks), unset: unset}
		m.updateIssueRange()
	}
}

func (m *model) updateIssueRange() {
	if m.issueRange == nil {
		return
	}
	indices := m.visibleIssues()
	anchor, focused := -1, -1
	for position, index := range indices {
		if m.issues[index].IssueKey == m.issueRange.anchor {
			anchor = position
		}
		if index == m.issueSelected {
			focused = position
		}
	}
	if anchor < 0 || focused < 0 {
		m.issueRange = nil
		return
	}
	m.issueMarks = maps.Clone(m.issueRange.base)
	if m.issueMarks == nil {
		m.issueMarks = make(map[string]bool)
	}
	for _, index := range indices[min(anchor, focused) : max(anchor, focused)+1] {
		key := m.issues[index].IssueKey
		if m.issueRange.unset {
			delete(m.issueMarks, key)
		} else {
			m.issueMarks[key] = true
		}
	}
}

func (m *model) markVisibleIssues() {
	if m.issueMarks == nil {
		m.issueMarks = make(map[string]bool)
	}
	for _, index := range m.visibleIssues() {
		m.issueMarks[m.issues[index].IssueKey] = true
	}
}

func (m *model) pruneIssueMarks() {
	present := make(map[string]bool, len(m.issues))
	for _, item := range m.issues {
		present[item.IssueKey] = true
	}
	removed := 0
	for key := range m.issueMarks {
		if !present[key] {
			delete(m.issueMarks, key)
			removed++
		}
	}
	if removed > 0 {
		m.setNotice(noticeWarning, fmt.Sprintf("%d selected issues no longer available; removed from selection", removed))
	}
}

type epicDestination struct{ name, label string }
type epicAssignmentResultMsg struct {
	keys []string
	epic string
	err  error
}

func (msg epicAssignmentResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}

func issueCountLabel(count int) string {
	if count == 1 {
		return "1 issue"
	}
	return fmt.Sprintf("%d issues", count)
}

func (m *model) toggleIssueMark() {
	key := m.selectedIssueKey()
	if key == "" {
		return
	}
	if m.issueMarks == nil {
		m.issueMarks = make(map[string]bool)
	}
	if m.issueMarks[key] {
		delete(m.issueMarks, key)
	} else {
		m.issueMarks[key] = true
	}
	m.moveIssueSelection(1)
}

func (m model) markedIssueKeys() []string {
	keys := make([]string, 0, len(m.issueMarks))
	for key := range m.issueMarks {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareIssueKeys)
	return keys
}

func (m model) hiddenIssueMarks() int {
	visible := 0
	for _, index := range m.visibleIssues() {
		if m.issueMarks[m.issues[index].IssueKey] {
			visible++
		}
	}
	return len(m.issueMarks) - visible
}

func (m model) issueSelectionLabel() string {
	if len(m.issueMarks) == 0 && m.issueRange == nil {
		return ""
	}
	label := fmt.Sprintf("%d selected", len(m.issueMarks))
	if m.issueRange != nil {
		mode := "Range select"
		if m.issueRange.unset {
			mode = "Range unselect"
		}
		label = mode + " · " + label
	}
	if hidden := m.hiddenIssueMarks(); hidden > 0 {
		label += fmt.Sprintf(" · %d outside current view", hidden)
	}
	return label
}

func (m model) openEpicAssignment() (tea.Model, tea.Cmd) {
	if len(m.issueMarks) == 0 || m.issuesLoading || m.issuesErr != "" {
		return m, nil
	}
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Find an epic or type a new name"
	m.epicAssignment = &epicAssignmentState{input: input, keys: m.markedIssueKeys(), hidden: m.hiddenIssueMarks()}
	m.overlay = epicAssignmentOverlay
	m.clearNotice()
	m.resizeIssueInputs()
	m.restyleIssueInputs()
	return m, m.epicAssignment.input.Focus()
}

func (m model) epicDestinations() []epicDestination {
	query := strings.TrimSpace(m.epicAssignment.input.Value())
	var options []epicDestination
	exact := false
	for _, group := range worklogs.GroupIssueEpics(m.issues) {
		if group.Epic == "" {
			continue
		}
		if group.Epic == query {
			exact = true
		}
		if strings.Contains(strings.ToLower(group.Epic), strings.ToLower(query)) {
			options = append(options, epicDestination{group.Epic, group.Epic})
		}
	}
	if query != "" && !exact {
		options = append(options, epicDestination{query, "Create: " + query})
	}
	if query == "" {
		options = append(options, epicDestination{"", "No epic (remove assignment)"})
	}
	return options
}

func (m model) updateEpicAssignment(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.epicAssignment
	if state == nil || state.saving {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.overlay = noOverlay
			m.epicAssignment = nil
			return m, nil
		case "up":
			state.selected = max(0, state.selected-1)
			return m, nil
		case "down":
			state.selected = min(len(m.epicDestinations())-1, state.selected+1)
			return m, nil
		case "enter":
			return m.beginEpicAssignment()
		}
	}
	previous := state.input.Value()
	input, cmd := state.input.Update(msg)
	state.input = input
	if previous != input.Value() {
		state.selected = 0
		state.err = ""
	}
	return m, cmd
}

func (m model) beginEpicAssignment() (tea.Model, tea.Cmd) {
	state := m.epicAssignment
	if state.stale {
		state.err = "Issues changed externally; cancel and reopen before applying."
		return m, nil
	}
	options := m.epicDestinations()
	if len(options) == 0 {
		return m, nil
	}
	epic := options[min(state.selected, len(options)-1)].name
	patches := make([]worklogs.IssueMetadataPatch, len(state.keys))
	for index, key := range state.keys {
		patches[index] = worklogs.IssueMetadataPatch{IssueKey: key, EpicSet: true}
		if epic != "" {
			patches[index].Epic = &epic
		}
	}
	if err := worklogs.ValidateIssueMetadataPatches(patches); err != nil {
		state.err = err.Error()
		return m, nil
	}
	state.saving = true
	keys, ws, ctx := slices.Clone(state.keys), m.workspace, m.ctx
	cmd := func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui bulk epic assignment"); err != nil {
			return epicAssignmentResultMsg{keys: keys, epic: epic, err: err}
		}
		return epicAssignmentResultMsg{keys: keys, epic: epic, err: ws.issues.ApplyIssueMetadata(ctx, patches, false)}
	}
	return m, m.beginActivity("issues.apply", fmt.Sprintf("issues=%d epic=%s", len(keys), epicName(epic)), cmd)
}

func (m model) handleEpicAssignmentResult(msg epicAssignmentResultMsg) (tea.Model, tea.Cmd) {
	if m.epicAssignment == nil {
		return m, nil
	}
	if msg.err != nil {
		m.epicAssignment.saving = false
		m.epicAssignment.err = msg.err.Error()
		m.setNotice(noticeError, msg.err.Error())
		return m, nil
	}
	assigned := make(map[string]bool, len(msg.keys))
	for _, key := range msg.keys {
		assigned[key] = true
	}
	key := m.selectedIssueKey()
	for index, item := range m.issues {
		if assigned[item.IssueKey] {
			m.issues[index].Epic = msg.epic
		}
	}
	m.epicAssignment = nil
	m.overlay = noOverlay
	m.issueMarks = nil
	m.restoreIssueSelection(key)
	m.restoreEpicSelection(m.epicsSelected, m.epicSelected)
	notice := "Assigned " + issueCountLabel(len(msg.keys)) + " to " + msg.epic
	if msg.epic == "" {
		notice = "Removed epic from " + issueCountLabel(len(msg.keys))
	}
	m.setNotice(noticeSuccess, notice)
	return m, m.consumeTrackerCmd(refreshIssues | refreshWorklogs)
}

func (m model) renderEpicAssignment(width, height int) string {
	state := m.epicAssignment
	if state == nil {
		return ""
	}
	lines := []string{m.paintBold("Assign epic to "+issueCountLabel(len(state.keys)), m.colors.Focus), ""}
	summary := strings.Join(state.keys, ", ")
	if state.hidden > 0 {
		summary = fmt.Sprintf("%d outside current view · ", state.hidden) + summary
	}
	lines = append(lines, m.secondary(truncate(summary, width)), "", "Epic: "+state.input.View(), "")
	options := m.epicDestinations()
	// Reserve space for the explicit consequence and any error; destination list scrolls.
	tail := []string{""}
	if len(options) > 0 {
		target := options[min(state.selected, len(options)-1)].name
		tail = append(tail, "Apply to all selected: "+truncate(epicName(target), max(1, width-23)))
	}
	if state.saving {
		tail = append(tail, m.paint("Saving…", m.colors.Progress))
	}
	if state.err != "" {
		errorLines := strings.Split(ansi.Wrap(state.err, width, ""), "\n")
		limit := max(1, min(3, height-len(lines)-len(tail)-1))
		if len(errorLines) > limit {
			errorLines = errorLines[:limit]
			errorLines[limit-1] = ansi.Truncate(errorLines[limit-1], width-1, "") + "…"
		}
		for _, line := range errorLines {
			tail = append(tail, m.paint(line, m.colors.Destructive))
		}
	}
	start, end := visibleRange(state.selected, len(options), max(1, height-len(lines)-len(tail)))
	for index := start; index < end; index++ {
		prefix := "  "
		if index == state.selected {
			prefix = "> "
		}
		lines = append(lines, m.selectedRow(prefix+truncate(options[index].label, width-2), width, index == state.selected))
	}
	return strings.Join(append(lines, tail...), "\n")
}

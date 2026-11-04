package tui

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

type worklogRangeState struct {
	anchor string
	base   map[string]int64
	unset  bool
}

type worklogAssignmentState struct {
	title                  string
	input                  textinput.Model
	ids                    []string
	expected               []worklogs.AssignmentExpectation
	generation             int
	loading, saving, stale bool
	err                    string
}

type worklogAssignmentSuggestionsMsg struct {
	generation int
	query      string
	items      []string
	title      string
}

type worklogAssignmentLoadedMsg struct {
	generation int
	records    []worklogs.LocalWorklog
	err        error
}
type worklogAssignmentResultMsg struct {
	ids    []string
	result worklogs.AssignResult
	err    error
}

func (msg worklogAssignmentResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}

type worklogMarksCheckedMsg struct {
	generation int
	ids        []string
	records    []worklogs.LocalWorklog
	err        error
}

func (m model) isWorklogList() bool {
	return m.worklogView == dayWorklogView || m.worklogView == unassignedWorklogView
}

func (m model) renderUnassignedWorkspace(width, height int) string {
	listWidth, detailWidth := inspectorWidths(width)
	total := 0
	for _, item := range m.worklogs {
		total += item.DurationSeconds
	}
	lines := []string{m.renderWorklogViewSelector(), fmt.Sprintf("All dates · %d worklogs · %s", len(m.worklogs), formatEstimateDuration(int64(total)))}
	if label := m.worklogSelectionLabel(); label != "" {
		lines = append(lines, m.paint(label, m.colors.Focus))
	}
	lines = append(lines, "")
	if m.worklogFilter != nil {
		lines = append(lines, "Find "+m.worklogFilter.input.View())
	}
	lines = append(lines, m.muted("      Date         Start Duration Description"))
	indices := m.visibleWorklogIndices()
	start, end := visibleRange(slices.Index(indices, m.worklogSelected), len(indices), max(1, height-len(lines)))
	for _, index := range indices[start:end] {
		item := m.worklogs[index]
		prefix := "  "
		if index == m.worklogSelected {
			prefix = "> "
		}
		mark := "[ ] "
		if _, ok := m.worklogMarks[item.ID]; ok {
			mark = "[x] "
		}
		row := prefix + mark + item.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02   15:04 ") + fmt.Sprintf("%-8s %s", formatDuration(item.DurationSeconds), item.Description)
		lines = append(lines, m.selectedRow(row, listWidth-2, index == m.worklogSelected))
	}
	if len(indices) == 0 {
		message := "No unassigned worklogs."
		if m.worklogLoading {
			message = "Loading…"
		}
		lines = append(lines, message)
	}
	return m.inspector(strings.Join(lines, "\n"), m.renderWorklogInspector(detailWidth-2, height), width, height)
}

func (m *model) toggleWorklogMark() {
	if m.worklogLoading {
		return
	}
	if selected := m.selectedWorklog(); selected != nil {
		if m.worklogMarks == nil {
			m.worklogMarks = make(map[string]int64)
		}
		if _, ok := m.worklogMarks[selected.ID]; ok {
			delete(m.worklogMarks, selected.ID)
		} else {
			m.worklogMarks[selected.ID] = selected.Revision
		}
		m.moveWorklogSelection(1)
	}
}

func (m *model) toggleWorklogRange(unset bool) {
	if m.worklogRange != nil {
		m.worklogRange = nil
		return
	}
	if m.worklogLoading {
		return
	}
	if item := m.selectedWorklog(); item != nil {
		m.worklogRange = &worklogRangeState{anchor: item.ID, base: maps.Clone(m.worklogMarks), unset: unset}
		m.updateWorklogRange()
	}
}

func (m *model) updateWorklogRange() {
	if m.worklogRange == nil {
		return
	}
	indices := m.visibleWorklogIndices()
	anchor, focused := -1, -1
	for pos, index := range indices {
		if m.worklogs[index].ID == m.worklogRange.anchor {
			anchor = pos
		}
		if index == m.worklogSelected {
			focused = pos
		}
	}
	if anchor < 0 || focused < 0 {
		m.worklogRange = nil
		return
	}
	m.worklogMarks = maps.Clone(m.worklogRange.base)
	if m.worklogMarks == nil {
		m.worklogMarks = make(map[string]int64)
	}
	for _, index := range indices[min(anchor, focused) : max(anchor, focused)+1] {
		item := m.worklogs[index]
		if m.worklogRange.unset {
			delete(m.worklogMarks, item.ID)
		} else {
			m.worklogMarks[item.ID] = item.Revision
		}
	}
}

func (m *model) markVisibleWorklogs() {
	if m.worklogLoading {
		return
	}
	m.worklogRange = nil
	if m.worklogMarks == nil {
		m.worklogMarks = make(map[string]int64)
	}
	for _, index := range m.visibleWorklogIndices() {
		item := m.worklogs[index]
		m.worklogMarks[item.ID] = item.Revision
	}
}

func (m model) markedWorklogIDs() []string { return slices.Sorted(maps.Keys(m.worklogMarks)) }

func (m model) loadWorklogAssignmentSuggestions() tea.Cmd {
	a := m.worklogAssignment
	if a == nil {
		return nil
	}
	query := strings.ToUpper(strings.TrimSpace(a.input.Value()))
	ws, ctx, generation := m.workspace, m.ctx, a.generation
	return func() tea.Msg {
		keys, _ := ws.worklogs.ListKnownIssueKeys(ctx, query, issueSuggestionLimit)
		title := ""
		if worklogs.IsValidIssueKey(query) {
			metadata, _ := ws.worklogs.LookupIssueMetadata(ctx, []string{query})
			title = metadata[query].Title
		}
		return worklogAssignmentSuggestionsMsg{generation: generation, query: query, items: keys, title: title}
	}
}

func (m model) worklogSelectionLabel() string {
	if len(m.worklogMarks) == 0 && m.worklogRange == nil {
		return ""
	}
	visible := 0
	for _, index := range m.visibleWorklogIndices() {
		if _, ok := m.worklogMarks[m.worklogs[index].ID]; ok {
			visible++
		}
	}
	label := fmt.Sprintf("%d selected", len(m.worklogMarks))
	if m.worklogRange != nil {
		mode := "Range select"
		if m.worklogRange.unset {
			mode = "Range unselect"
		}
		label = mode + " · " + label
	}
	if hidden := len(m.worklogMarks) - visible; hidden > 0 {
		label += fmt.Sprintf(" · %d outside current view", hidden)
	}
	return label
}

func (m *model) navigateWorklogList(key string) {
	indices := m.visibleWorklogIndices()
	if len(indices) == 0 {
		return
	}
	position := max(0, slices.Index(indices, m.worklogSelected))
	page := max(1, m.height-17)
	switch key {
	case "home":
		position = 0
	case "end":
		position = len(indices) - 1
	case "pgup":
		position = max(0, position-page)
	case "pgdown":
		position = min(len(indices)-1, position+page)
	}
	m.worklogSelected = indices[position]
	m.updateWorklogRange()
}

func (m model) checkWorklogMarks() tea.Cmd {
	if m.workspace == nil || len(m.worklogMarks) == 0 {
		return nil
	}
	ids, generation, ws, ctx := m.markedWorklogIDs(), m.worklogGen, m.workspace, m.ctx
	return func() tea.Msg {
		rows, err := ws.worklogs.LookupIDs(ctx, ids)
		return worklogMarksCheckedMsg{generation: generation, ids: ids, records: rows, err: err}
	}
}

func (m model) applyWorklogMarksChecked(msg worklogMarksCheckedMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.worklogGen || msg.err != nil {
		return m, nil
	}
	present := make(map[string]bool, len(msg.records))
	for _, item := range msg.records {
		present[item.ID] = true
	}
	removed := 0
	for _, id := range msg.ids {
		if !present[id] {
			if _, ok := m.worklogMarks[id]; ok {
				delete(m.worklogMarks, id)
				removed++
			}
		}
	}
	if removed > 0 {
		m.setNotice(noticeWarning, fmt.Sprintf("%d selected worklogs were deleted; removed from selection", removed))
	}
	return m, nil
}

func (m model) openWorklogAssignment() (tea.Model, tea.Cmd) {
	if len(m.worklogMarks) == 0 || m.worklogLoading {
		return m, nil
	}
	m.worklogRange = nil
	m.assignmentGen++
	input := m.searchInput("Issue key, or Ctrl+K to find")
	input.SetWidth(max(12, min(64, m.width-40)))
	input.ShowSuggestions = true
	state := &worklogAssignmentState{input: input, ids: m.markedWorklogIDs(), generation: m.assignmentGen, loading: true}
	m.worklogAssignment = state
	m.overlay = worklogAssignmentOverlay
	ws, ctx, generation, ids := m.workspace, m.ctx, state.generation, state.ids
	return m, tea.Batch(input.Focus(), func() tea.Msg {
		records, err := ws.worklogs.LookupIDs(ctx, ids)
		return worklogAssignmentLoadedMsg{generation: generation, records: records, err: err}
	})
}

func (m model) applyWorklogAssignmentLoaded(msg worklogAssignmentLoadedMsg) (tea.Model, tea.Cmd) {
	s := m.worklogAssignment
	if s == nil || s.generation != msg.generation {
		return m, nil
	}
	s.loading = false
	if msg.err != nil {
		s.err = msg.err.Error()
		s.stale = true
		return m, nil
	}
	if len(msg.records) != len(s.ids) {
		s.err = "Selection changed; cancel and reopen assignment."
		s.stale = true
		return m, nil
	}
	for _, row := range msg.records {
		if row.IssueKey != "" {
			s.err = "Selection contains an assigned worklog; edit its key individually."
			s.stale = true
			return m, nil
		}
		s.expected = append(s.expected, worklogs.AssignmentExpectation{ID: row.ID, Revision: row.Revision})
	}
	return m, m.loadWorklogAssignmentSuggestions()
}

func (m model) updateWorklogAssignment(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := m.worklogAssignment
	if s == nil || s.saving {
		return m, nil
	}
	if m.issuePicker != nil {
		return m.updateIssuePicker(msg)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.worklogAssignment = nil
			m.overlay = noOverlay
			return m, nil
		case "ctrl+k":
			return m, m.openIssuePicker()
		case "enter", "ctrl+s":
			if s.loading || s.stale {
				return m, nil
			}
			issue := strings.ToUpper(strings.TrimSpace(s.input.Value()))
			if !worklogs.IsValidIssueKey(issue) {
				s.err = "Issue must match <PROJECTKEY>-<NUMBER>."
				return m, nil
			}
			s.saving = true
			s.err = ""
			input := worklogs.AssignInput{IDs: slices.Clone(s.ids), IssueKey: issue, Expected: slices.Clone(s.expected)}
			ws, ctx := m.workspace, m.ctx
			return m, m.beginActivity("worklogs.assign", fmt.Sprintf("issue=%s count=%d", issue, len(input.IDs)), func() tea.Msg {
				if err := m.deps.checkWritable(ws.sqlitePath, "tui worklogs assign"); err != nil {
					return worklogAssignmentResultMsg{ids: input.IDs, err: err}
				}
				result, err := ws.mutations.Assign(ctx, input, false)
				return worklogAssignmentResultMsg{ids: input.IDs, result: result, err: err}
			})
		}
	}
	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if before != s.input.Value() {
		s.title = ""
		s.err = ""
		return m, tea.Batch(cmd, m.loadWorklogAssignmentSuggestions())
	}
	return m, cmd
}

func (m model) applyWorklogAssignmentResult(msg worklogAssignmentResultMsg) (tea.Model, tea.Cmd) {
	s := m.worklogAssignment
	if s == nil {
		return m, nil
	}
	s.saving = false
	if msg.err != nil {
		s.err = msg.err.Error()
		if errors.Is(msg.err, worklogs.ErrConflict) || errors.Is(msg.err, worklogs.ErrNotFound) {
			s.stale = true
		}
		var validation worklogs.ValidationError
		if errors.As(msg.err, &validation) && validation.Conflict != nil {
			s.err = fmt.Sprintf("Conflict with %s. Resolve using worklogs show, update, or delete, then reopen assignment.", strings.Join(validation.Conflict.ConflictingIDs, ", "))
		}
		return m, nil
	}
	for _, id := range msg.ids {
		delete(m.worklogMarks, id)
	}
	m.worklogAssignment = nil
	m.overlay = noOverlay
	m.setNotice(noticeSuccess, fmt.Sprintf("Assigned %d worklogs", len(msg.result.Records)))
	return m, m.consumeTrackerCmd(refreshWorklogs | refreshIssues)
}

func (m model) renderWorklogAssignment(width, height int) string {
	s := m.worklogAssignment
	if s == nil {
		return ""
	}
	lines := []string{m.paintBold("Assign issue", m.colors.Focus), fmt.Sprintf("%d worklogs selected", len(s.ids)), ""}

	if m.issuePicker != nil {
		p := m.issuePicker
		modes := []string{"Key", "Description", "Title"}
		active := 0
		if p.mode == worklogs.IssueFindByDescription {
			active = 1
		} else if p.mode == worklogs.IssueFindByTitle {
			active = 2
		}
		modes[active] = m.paint("["+modes[active]+"]", m.colors.Focus)
		lines = append(lines, "Find issue · "+strings.Join(modes, "  "), p.input.View(), "")
		start, end := visibleRange(p.selected, len(p.items), max(1, height-len(lines)-5))
		for i := start; i < end; i++ {
			prefix := "  "
			if i == p.selected {
				prefix = "> "
			}
			item := p.items[i]
			lines = append(lines, m.selectedRow(prefix+item.IssueKey+"  "+item.Title, width, i == p.selected))
		}
		if p.loading {
			lines = append(lines, "Searching…")
		} else if p.err != "" {
			lines = append(lines, m.paint(p.err, m.colors.Destructive))
		} else if len(p.items) == 0 {
			lines = append(lines, "No matching issues.")
		}

	} else {
		lines = append(lines, "Issue key", s.input.View())
		if s.title != "" {
			lines = append(lines, m.secondary(s.title))
		}

		if s.loading {
			lines = append(lines, "Checking selection…")
		}
		if s.saving {
			lines = append(lines, "Saving…")
		}
	}
	if s.err != "" {
		errorLines := strings.Split(ansi.Wrap(m.paint(s.err, m.colors.Destructive), width, ""), "\n")
		limit := max(1, min(3, height-len(lines)-2))
		if len(errorLines) > limit {
			errorLines = errorLines[:limit]
			errorLines[limit-1] = ansi.Truncate(errorLines[limit-1], width-1, "") + "…"
		}
		lines = append(lines, errorLines...)
	}
	lines = lines[:min(len(lines), max(1, height-2))]
	return strings.Join(lines, "\n")
}

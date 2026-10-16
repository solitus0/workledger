package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

const (
	issuePickerPageSize   = 50
	issueDetailPageSize   = 25
	issuePickerSearchWait = 120 * time.Millisecond
)

func (m *model) openIssuePicker() tea.Cmd {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Issue key prefix"
	input.CharLimit = 64
	if m.deps.noColor {
		input.SetVirtualCursor(false)
		input.SetStyles(textinput.Styles{})
	} else {
		input.SetStyles(m.colors.textInputStyles())
	}
	m.issuePicker = &issuePickerState{input: input, mode: worklogs.IssueFindByPrefix}
	m.resizeIssuePickerInput()
	return m.loadIssuePickerPage(0, false)
}

func (m *model) resizeIssuePickerInput() {
	if m.issuePicker != nil {
		left, _ := issuePickerPanelWidths(m.issuePickerWorkspaceWidth())
		m.issuePicker.input.SetWidth(max(12, left-4))
	}
}

func (m *model) restyleIssuePickerInput() {
	if m.issuePicker != nil && !m.deps.noColor {
		m.issuePicker.input.SetStyles(m.colors.textInputStyles())
	}
}

func (m *model) loadIssuePickerPage(page int, debounce bool) tea.Cmd {
	if m.issuePicker == nil {
		return nil
	}
	picker := m.issuePicker
	query := ""
	if picker.searching {
		query = strings.TrimSpace(picker.input.Value())
		if picker.mode == worklogs.IssueFindByPrefix {
			query = strings.ToUpper(query)
		}
	}
	if query != picker.query {
		picker.selectLast = false
	}
	picker.selectedKey = ""
	if page == picker.page && query == picker.query && picker.selected >= 0 && picker.selected < len(picker.items) {
		picker.selectedKey = picker.items[picker.selected].IssueKey
	}
	picker.page = page
	picker.query = query
	picker.items = nil
	picker.selected = 0
	picker.loading = true
	picker.err = ""
	m.issuePickerGen++
	picker.generation = m.issuePickerGen
	m.clearIssuePickerDetails()
	generation := picker.generation
	if debounce {
		return tea.Tick(issuePickerSearchWait, func(time.Time) tea.Msg {
			return issuePickerReadyMsg{generation: generation}
		})
	}
	return m.issuePickerCmd()
}

func (m model) issuePickerCmd() tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	generation, mode, query, page := m.issuePicker.generation, m.issuePicker.mode, m.issuePicker.query, m.issuePicker.page
	return func() tea.Msg {
		items, hasMore, err := ws.worklogs.ListUsedIssues(ctx, mode, query, issuePickerPageSize, page*issuePickerPageSize)
		return issuePickerResultMsg{generation: generation, mode: mode, query: query, page: page, items: items, hasMore: hasMore, err: err}
	}
}

func (m model) applyIssuePickerResult(msg issuePickerResultMsg) (tea.Model, tea.Cmd) {
	if m.issuePicker == nil || msg.generation != m.issuePicker.generation ||
		msg.mode != m.issuePicker.mode || msg.query != m.issuePicker.query || msg.page != m.issuePicker.page {
		return m, nil
	}
	picker := m.issuePicker
	picker.loading = false
	if msg.err != nil {
		picker.err = msg.err.Error()
		picker.hasMore = false
		picker.selectLast = false
		return m, nil
	}
	if len(msg.items) == 0 && picker.page > 0 {
		return m, m.loadIssuePickerPage(picker.page-1, false)
	}
	picker.items = msg.items
	picker.hasMore = msg.hasMore
	if picker.selectLast {
		picker.selected = max(0, len(picker.items)-1)
		picker.selectLast = false
	}
	for index, item := range picker.items {
		if item.IssueKey == picker.selectedKey {
			picker.selected = index
			break
		}
	}
	picker.selectedKey = ""
	return m, m.loadIssuePickerDetails()
}

func (m *model) clearIssuePickerDetails() {
	picker := m.issuePicker
	if picker == nil {
		return
	}
	m.issueDetailGen++
	picker.detailGen = m.issueDetailGen
	picker.detailIssue = ""
	picker.details = nil
	picker.detailMore = false
	picker.detailLoad = false
	picker.detailErr = ""
	picker.detailScroll = 0
	picker.detailAdvance = 0
}

func (m *model) loadIssuePickerDetails() tea.Cmd {
	picker := m.issuePicker
	if picker == nil {
		return nil
	}
	m.clearIssuePickerDetails()
	if picker.loading || picker.selected < 0 || picker.selected >= len(picker.items) {
		return nil
	}
	picker.detailIssue = picker.items[picker.selected].IssueKey
	picker.detailLoad = true
	return m.issueDescriptionsCmd(0)
}

func (m model) issueDescriptionsCmd(offset int) tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	picker := m.issuePicker
	generation, issueKey := picker.detailGen, picker.detailIssue
	return func() tea.Msg {
		items, hasMore, err := ws.worklogs.ListIssueWorklogDescriptions(ctx, issueKey, issueDetailPageSize, offset)
		return issueDescriptionsResultMsg{generation: generation, issueKey: issueKey, offset: offset, items: items, hasMore: hasMore, err: err}
	}
}

func (m model) applyIssueDescriptionsResult(msg issueDescriptionsResultMsg) (tea.Model, tea.Cmd) {
	picker := m.issuePicker
	if picker == nil || msg.generation != picker.detailGen || msg.issueKey != picker.detailIssue || msg.offset != len(picker.details) {
		return m, nil
	}
	picker.detailLoad = false
	if msg.err != nil {
		picker.detailErr = msg.err.Error()
		picker.detailMore = false
		picker.detailAdvance = 0
		return m, nil
	}
	picker.details = append(picker.details, msg.items...)
	picker.detailMore = msg.hasMore
	if picker.detailAdvance > 0 {
		advance := picker.detailAdvance
		picker.detailAdvance = 0
		return m.scrollIssuePickerDetails(advance)
	}
	return m, nil
}

func (m model) updateIssuePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m.updateIssuePickerInput(msg)
	}
	picker := m.issuePicker
	switch key.String() {
	case "/":
		if !picker.searching || picker.detailFocus {
			picker.searching = true
			picker.detailFocus = false
			return m, picker.input.Focus()
		}
	case "ctrl+t":
		picker.searching = true
		picker.detailFocus = false
		if picker.mode == worklogs.IssueFindByPrefix {
			picker.mode = worklogs.IssueFindByDescription
			picker.input.Placeholder = "Description text"
		} else if picker.mode == worklogs.IssueFindByDescription {
			picker.mode = worklogs.IssueFindByTitle
			picker.input.Placeholder = "Issue title text"
		} else {
			picker.mode = worklogs.IssueFindByPrefix
			picker.input.Placeholder = "Issue key prefix"
		}
		picker.input.SetValue("")
		picker.input.Focus()
		return m, m.loadIssuePickerPage(0, false)
	case "esc":
		if picker.searching {
			picker.searching = false
			picker.mode = worklogs.IssueFindByPrefix
			picker.input.Placeholder = "Issue key prefix"
			picker.detailFocus = false
			picker.input.Blur()
			picker.input.SetValue("")
			return m, m.loadIssuePickerPage(0, false)
		}
		m.issuePicker = nil
		return m, nil
	case "tab":
		picker.detailFocus = !picker.detailFocus
		if picker.searching {
			if picker.detailFocus {
				picker.input.Blur()
			} else {
				return m, picker.input.Focus()
			}
		}
		return m, nil
	case "left":
		if !picker.searching {
			picker.detailFocus = false
			return m, nil
		}
	case "right":
		if !picker.searching {
			picker.detailFocus = true
			return m, nil
		}
	case "up":
		if picker.detailFocus {
			return m.scrollIssuePickerDetails(-1)
		}
		return m.moveIssuePicker(-1)
	case "down":
		if picker.detailFocus {
			return m.scrollIssuePickerDetails(1)
		}
		return m.moveIssuePicker(1)
	case "pgup":
		if picker.detailFocus {
			return m.scrollIssuePickerDetails(-m.issueDetailVisibleRows())
		}
		if picker.page > 0 {
			picker.selectLast = false
			return m, m.loadIssuePickerPage(picker.page-1, false)
		}
		return m, nil
	case "pgdown":
		if picker.detailFocus {
			return m.scrollIssuePickerDetails(m.issueDetailVisibleRows())
		}
		if picker.hasMore {
			picker.selectLast = false
			return m, m.loadIssuePickerPage(picker.page+1, false)
		}
		return m, nil
	case "enter":
		if picker.loading || picker.selected < 0 || picker.selected >= len(picker.items) {
			return m, nil
		}
		issue := picker.items[picker.selected].IssueKey
		m.issuePicker = nil
		m.form.inputs[issueInput].SetValue(issue)
		m.formError = ""
		m.invalidateDescriptionSuggestions()
		return m, tea.Batch(m.scheduleIssueSuggestions(false), m.schedulePreview())
	}
	if !picker.searching {
		switch key.String() {
		case "k":
			if picker.detailFocus {
				return m.scrollIssuePickerDetails(-1)
			}
			return m.moveIssuePicker(-1)
		case "j":
			if picker.detailFocus {
				return m.scrollIssuePickerDetails(1)
			}
			return m.moveIssuePicker(1)
		}
		return m, nil
	}
	return m.updateIssuePickerInput(msg)
}

func (m model) updateIssuePickerInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	picker := m.issuePicker
	if !picker.searching || picker.detailFocus {
		return m, nil
	}
	before := picker.input.Value()
	input, cmd := picker.input.Update(msg)
	picker.input = input
	if before != input.Value() {
		return m, tea.Batch(cmd, m.loadIssuePickerPage(0, true))
	}
	return m, cmd
}

func (m model) moveIssuePicker(delta int) (tea.Model, tea.Cmd) {
	picker := m.issuePicker
	if picker.loading || len(picker.items) == 0 {
		return m, nil
	}
	next := picker.selected + delta
	if next < 0 && picker.page > 0 {
		picker.selectLast = true
		return m, m.loadIssuePickerPage(picker.page-1, false)
	}
	if next >= len(picker.items) && picker.hasMore {
		picker.selectLast = false
		return m, m.loadIssuePickerPage(picker.page+1, false)
	}
	picker.selected = min(len(picker.items)-1, max(0, next))
	return m, m.loadIssuePickerDetails()
}

func (m model) scrollIssuePickerDetails(delta int) (tea.Model, tea.Cmd) {
	picker := m.issuePicker
	if picker.detailIssue == "" || picker.detailErr != "" {
		return m, nil
	}
	_, rightWidth := issuePickerPanelWidths(m.issuePickerWorkspaceWidth())
	lineCount := len(m.issueDetailLines(rightWidth - 4))
	visible := m.issueDetailVisibleRows()
	maxScroll := max(0, lineCount-visible)
	picker.detailScroll = min(maxScroll, max(0, picker.detailScroll+delta))
	if delta > 0 && picker.detailScroll == maxScroll && picker.detailMore {
		picker.detailAdvance = max(picker.detailAdvance, delta)
		if !picker.detailLoad {
			picker.detailLoad = true
			return m, m.issueDescriptionsCmd(len(picker.details))
		}
	}
	return m, nil
}

func (m model) issuePickerWorkspaceWidth() int {
	return calculateLayout(m.width, m.height, m.activityVisible()).workspace.width
}

func (m model) issueDetailVisibleRows() int {
	return max(1, calculateLayout(m.width, m.height, m.activityVisible()).workspace.height-7)
}

func issuePickerPanelWidths(width int) (int, int) {
	left := max(31, width*45/100)
	return left, width - left
}

func (m model) issueDetailLines(width int) []string {
	picker := m.issuePicker
	if picker == nil {
		return nil
	}
	lines := make([]string, 0, len(picker.details)*3)
	for _, item := range picker.items {
		if item.IssueKey != picker.detailIssue {
			continue
		}
		if item.Title != "" {
			lines = append(lines, wrapIssueDescription(item.Title, width)...)
		}
		if item.MaxEstimateSeconds != nil {
			lines = append(lines, "Max estimate: "+formatEstimateDuration(*item.MaxEstimateSeconds))
		}
		if item.Title != "" || item.MaxEstimateSeconds != nil {
			lines = append(lines, "")
		}
		break
	}
	for _, item := range picker.details {
		date := item.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02")
		marker := ""
		if picker.searching && picker.mode == worklogs.IssueFindByDescription && picker.query != "" && strings.Contains(strings.ToLower(item.Description), strings.ToLower(picker.query)) {
			marker = "* "
		}
		lines = append(lines, marker+date+" · "+formatDuration(item.DurationSeconds))
		lines = append(lines, wrapIssueDescription(item.Description, width)...)
		lines = append(lines, "")
	}
	return lines
}

func wrapIssueDescription(value string, width int) []string {
	width = max(1, width)
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{"(empty description)"}
	}
	var lines []string
	for _, paragraph := range strings.Split(value, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			for lipgloss.Width(word) > width {
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				runes := []rune(word)
				cut := 0
				for next := 1; next <= len(runes) && lipgloss.Width(string(runes[:next])) <= width; next++ {
					cut = next
				}
				if cut == 0 {
					lines = append(lines, "…")
					word = string(runes[1:])
					continue
				}
				lines = append(lines, string(runes[:cut]))
				word = string(runes[cut:])
			}
			if word == "" {
				continue
			}
			if line == "" {
				line = word
			} else if lipgloss.Width(line)+1+lipgloss.Width(word) <= width {
				line += " " + word
			} else {
				lines = append(lines, line)
				line = word
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func (m model) renderIssuePicker(width, height int) string {
	picker := m.issuePicker
	leftWidth, rightWidth := issuePickerPanelWidths(width)
	leftInner, rightInner := leftWidth-4, rightWidth-4
	left := []string{m.paint("Known issues", m.colors.Focus)}
	if picker.searching {
		mode := "Find by prefix"
		if picker.mode == worklogs.IssueFindByDescription {
			mode = "Find by description"
		} else if picker.mode == worklogs.IssueFindByTitle {
			mode = "Find by title"
		}
		left = append(left, mode, picker.input.View())
	} else {
		left = append(left, m.muted("Browse local issue catalog"))
	}
	left = append(left, "")
	rowCount := max(1, height-len(left)-5)
	start, end := visibleRange(picker.selected, len(picker.items), rowCount)
	for index := start; index < end; index++ {
		item := picker.items[index]
		marker := "  "
		if index == picker.selected {
			marker = m.paint("> ", m.colors.Focus)
		}
		date := "Unused"
		if !item.LastEnteredAt.IsZero() {
			date = item.LastEnteredAt.In(m.workspace.cfg.Location).Format("2006-01-02")
		}
		label := item.IssueKey
		if item.Title != "" {
			label += " · " + item.Title
		}
		keyWidth := max(1, leftInner-2-11)
		left = append(left, m.selectedRow(fmt.Sprintf("%s%-*s %s", marker, keyWidth, truncate(label, keyWidth), date), leftInner, index == picker.selected))
	}
	if picker.loading {
		left = append(left, m.paint("  Loading issues…", m.colors.Progress))
	} else if picker.err != "" {
		left = append(left, m.paint("  Issue history unavailable", m.colors.Warning))
	} else if len(picker.items) == 0 {
		left = append(left, m.muted("  No matching issues"))
	}
	if picker.page > 0 || picker.hasMore {
		left = append(left, "", m.muted(fmt.Sprintf("Page %d", picker.page+1)))
	}

	right := []string{m.paint(truncate("Worklogs · "+picker.detailIssue, rightInner), m.colors.Focus)}
	if picker.searching && picker.mode == worklogs.IssueFindByDescription && picker.query != "" {
		right = append(right, m.muted("* matches description find"))
	} else {
		right = append(right, m.muted("All active descriptions"))
	}
	right = append(right, "")
	if picker.detailErr != "" {
		right = append(right, m.paint("Descriptions unavailable", m.colors.Warning))
	} else if picker.detailIssue == "" {
		right = append(right, m.muted("Select an issue"))
	} else {
		lines := m.issueDetailLines(rightInner)
		visible := max(1, height-len(right)-4)
		start := min(picker.detailScroll, max(0, len(lines)-visible))
		end := min(len(lines), start+visible)
		right = append(right, lines[start:end]...)
		if picker.detailLoad {
			right = append(right, m.muted("Loading descriptions…"))
		} else if len(picker.details) == 0 {
			right = append(right, m.muted("No descriptions"))
		} else if picker.detailMore || end < len(lines) {
			right = append(right, m.muted("↓ more"))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.box(strings.Join(left, "\n"), leftWidth, height, !picker.detailFocus),
		m.box(strings.Join(right, "\n"), rightWidth, height, picker.detailFocus),
	)
}

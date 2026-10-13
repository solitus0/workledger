package tui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

type issueSortMode int

const (
	sortIssueKey issueSortMode = iota
	sortRemaining
	sortOverrun
	sortTitle
	sortLogged
	sortCreatedAt
)

func (mode issueSortMode) label(view issueView) string {
	switch mode {
	case sortRemaining:
		return "Remaining ↓"
	case sortOverrun:
		return "Overrun ↓"
	case sortTitle:
		return "Title A–Z"
	case sortLogged:
		return "Logged ↓"
	case sortCreatedAt:
		return "Created at ↓"
	default:
		if view == epicListView {
			return "Epic name A–Z"
		}
		return "Issue key ↑"
	}
}

func (m model) currentIssueSort() issueSortMode {
	if m.issueView == epicListView {
		return m.epicSort
	}
	return m.issueSort
}

func (m model) issueSortOptions() []issueSortMode {
	if m.issueView == epicListView {
		return []issueSortMode{sortIssueKey, sortRemaining, sortOverrun, sortLogged}
	}
	return []issueSortMode{sortCreatedAt, sortIssueKey, sortRemaining, sortOverrun, sortTitle, sortLogged}
}

func (m *model) openIssueSort() {
	m.issueRange = nil
	m.sortSelected = max(0, slices.Index(m.issueSortOptions(), m.currentIssueSort()))
	m.overlay = issueSortOverlay
}

func (m model) updateIssueSort(key string) (tea.Model, tea.Cmd) {
	options := m.issueSortOptions()
	switch key {
	case "up", "k":
		m.sortSelected = max(0, m.sortSelected-1)
	case "down", "j":
		m.sortSelected = min(len(options)-1, m.sortSelected+1)
	case "home":
		m.sortSelected = 0
	case "end":
		m.sortSelected = len(options) - 1
	case "esc":
		m.overlay = noOverlay
	case "enter":
		if m.issueView == epicListView {
			name, selected := m.epicsSelected, m.epicSelected
			m.epicSort = options[m.sortSelected]
			m.restoreEpicSelection(name, selected)
		} else {
			key := m.selectedIssueKey()
			m.issueSort = options[m.sortSelected]
			m.restoreIssueSelection(key)
		}
		m.overlay = noOverlay
	}
	return m, nil
}

func (m model) renderIssueSortMenu(width int) string {
	title := "Sort issues"
	if m.issueView == epicListView {
		title = "Sort epics"
	}
	lines := []string{m.paintBold(title, m.colors.Focus), ""}
	for index, mode := range m.issueSortOptions() {
		prefix := "  "
		if index == m.sortSelected {
			prefix = "> "
		}
		label := mode.label(m.issueView)
		if mode == m.currentIssueSort() {
			label += " (current)"
		}
		lines = append(lines, m.selectedRow(prefix+label, width, index == m.sortSelected))
	}
	return strings.Join(lines, "\n")
}

func compareKnownDescending(a, b int64, aKnown, bKnown bool) int {
	if aKnown != bKnown {
		if aKnown {
			return -1
		}
		return 1
	}
	if !aKnown {
		return 0
	}
	return cmp.Compare(b, a)
}

func compareIssueKeys(a, b string) int {
	ap, an, _ := strings.Cut(a, "-")
	bp, bn, _ := strings.Cut(b, "-")
	if result := strings.Compare(ap, bp); result != 0 {
		return result
	}
	if result := cmp.Compare(len(an), len(bn)); result != 0 {
		return result
	}
	return strings.Compare(an, bn)
}

func compareIssues(a, b worklogs.IssueMetadata, mode issueSortMode) int {
	result := 0
	switch mode {
	case sortCreatedAt:
		if a.CreatedAt.IsZero() != b.CreatedAt.IsZero() {
			if a.CreatedAt.IsZero() {
				return 1
			}
			return -1
		}
		result = b.CreatedAt.Compare(a.CreatedAt)
	case sortLogged:
		result = cmp.Compare(b.LoggedSeconds, a.LoggedSeconds)
	case sortRemaining:
		var ar, br int64
		if a.RemainingSeconds != nil {
			ar = *a.RemainingSeconds
		}
		if b.RemainingSeconds != nil {
			br = *b.RemainingSeconds
		}
		result = compareKnownDescending(ar, br, a.RemainingSeconds != nil, b.RemainingSeconds != nil)
	case sortOverrun:
		result = compareKnownDescending(a.OverrunSeconds, b.OverrunSeconds, a.MaxEstimateSeconds != nil, b.MaxEstimateSeconds != nil)
	case sortTitle:
		if (a.Title == "") != (b.Title == "") {
			if a.Title == "" {
				return 1
			}
			return -1
		}
		result = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	}
	if result != 0 {
		return result
	}
	return compareIssueKeys(a.IssueKey, b.IssueKey)
}

func compareEpics(a, b worklogs.EpicSummary, mode issueSortMode) int {
	result := 0
	switch mode {
	case sortLogged:
		result = cmp.Compare(b.LoggedSeconds, a.LoggedSeconds)
	case sortRemaining:
		result = compareKnownDescending(a.RemainingSeconds, b.RemainingSeconds, a.EstimatedIssueCount > 0, b.EstimatedIssueCount > 0)
	case sortOverrun:
		result = compareKnownDescending(a.OverrunSeconds, b.OverrunSeconds, a.EstimatedIssueCount > 0, b.EstimatedIssueCount > 0)
	}
	if result != 0 {
		return result
	}
	if (a.Epic == "") != (b.Epic == "") {
		if a.Epic == "" {
			return 1
		}
		return -1
	}
	if result := strings.Compare(strings.ToLower(a.Epic), strings.ToLower(b.Epic)); result != 0 {
		return result
	}
	return strings.Compare(a.Epic, b.Epic)
}

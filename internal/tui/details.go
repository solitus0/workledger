package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *model) openWorklogDetails() {
	m.openDetails("Worklog details", m.renderWorklogDetail())
	m.detailWorklog = true
}

func (m *model) openDetails(title, content string) {
	if sections := strings.SplitN(content, "\n\n", 2); len(sections) == 2 && ansi.Strip(sections[0]) == title {
		content = sections[1]
	}
	m.issueHistory = nil
	m.overlay = detailsOverlay
	m.detailTitle, m.detailContent, m.detailWorklog = title, content, false
	m.detailViewport = viewport.New()
	m.refreshDetailViewport()
}

func (m *model) refreshDetailViewport() {
	if m.issueHistory != nil {
		m.resizeIssueHistory()
		return
	}
	if m.overlay != detailsOverlay {
		return
	}
	layout := calculateLayout(m.width, m.height, false)
	width := max(1, min(72, layout.workspace.width-6)-4)
	m.detailViewport.SetWidth(width)
	if m.detailWorklog {
		m.detailContent = m.renderWorklogDetail()
	}
	content := ansi.Wrap(m.detailContent, width, "")
	m.detailViewport.SetHeight(min(max(1, layout.workspace.height-6), max(1, lipgloss.Height(content))))
	m.detailViewport.SetContent(content)
}

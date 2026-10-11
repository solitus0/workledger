package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

type moveState struct {
	preview     worklogs.MovePreview
	destination time.Time
	loading     bool
	saving      bool
	err         string
}

type movePreviewMsg struct {
	generation int
	preview    worklogs.MovePreview
	err        error
}

func (m *model) openMove(item worklogs.LocalWorklog) tea.Cmd {
	m.move = &moveState{preview: worklogs.MovePreview{Source: item}, destination: item.StartedAtUTC}
	m.clearNotice()
	return m.loadMove(item.Revision)
}

func (m *model) loadMove(revision int64) tea.Cmd {
	m.moveGen++
	generation, id := m.moveGen, m.move.preview.Source.ID
	m.move.loading = true
	m.move.err = ""
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		preview, err := ws.worklogs.PreviewMove(ctx, ws.cfg, id, revision)
		return movePreviewMsg{generation: generation, preview: preview, err: err}
	}
}

func (m model) applyMovePreview(msg movePreviewMsg) (tea.Model, tea.Cmd) {
	if m.move == nil || msg.generation != m.moveGen {
		return m, nil
	}
	m.move.loading = false
	if msg.err != nil {
		m.move.err = msg.err.Error()
		m.move.preview.Positions = nil
		return m, nil
	}
	destination := m.move.destination
	m.move.preview = msg.preview
	m.selectedDate = beginningOfDay(msg.preview.Source.StartedAtUTC, m.workspace.cfg.Location)
	if !destination.Equal(msg.preview.Source.StartedAtUTC) && !slices.ContainsFunc(msg.preview.Positions, func(t time.Time) bool { return t.Equal(destination) }) {
		m.move.destination = msg.preview.Source.StartedAtUTC
		m.move.err = "availability changed; choose a new position"
	}
	return m, nil
}

func (m model) updateMove(key string) (tea.Model, tea.Cmd) {
	if m.move.saving {
		return m, nil
	}
	switch key {
	case "esc":
		m.move = nil
		m.clearNotice()
		return m, m.startWorklogLoad()
	case "r":
		return m, m.loadMove(0)
	case "up", "down":
		if m.move.loading {
			return m, nil
		}
		if len(m.move.preview.Positions) == 0 {
			m.setNotice(noticeInfo, "no valid position in this day")
			return m, nil
		}
		positions := m.move.preview.Positions
		found := false
		if key == "up" {
			for i := len(positions) - 1; i >= 0; i-- {
				if positions[i].Before(m.move.destination) {
					m.move.destination = positions[i]
					found = true
					break
				}
			}
		} else {
			for _, position := range positions {
				if position.After(m.move.destination) {
					m.move.destination = position
					found = true
					break
				}
			}
		}
		if found {
			m.move.err = ""
			m.clearNotice()
		} else {
			m.setNotice(noticeInfo, "no further valid position")
		}
	case "ctrl+s":
		if m.move.loading {
			m.setNotice(noticeInfo, "wait for the move preview")
			return m, nil
		}
		if m.move.destination.Equal(m.move.preview.Source.StartedAtUTC) {
			m.setNotice(noticeInfo, "choose a different position")
			return m, nil
		}
		if !slices.ContainsFunc(m.move.preview.Positions, func(t time.Time) bool { return t.Equal(m.move.destination) }) {
			return m, nil
		}
		m.move.saving = true
		source, destination, ws, ctx, writable := m.move.preview.Source, m.move.destination, m.workspace, m.ctx, m.deps.checkWritable
		command := func() tea.Msg {
			if err := writable(ws.sqlitePath, "tui worklog move"); err != nil {
				return mutationResultMsg{kind: "move", err: err}
			}
			record, err := ws.mutations.Move(ctx, ws.cfg, source.ID, source.Revision, destination)
			return mutationResultMsg{kind: "move", records: []worklogs.LocalWorklog{record}, err: err}
		}
		return m, m.beginActivity("worklogs.update", tuiActivitySummary("id="+source.ID, "date="+m.selectedDate.Format("2006-01-02"), "placement=move"), command)
	}
	return m, nil
}

func (m model) renderMove(width, height int) string {
	state := m.move
	source := state.preview.Source
	lines := []string{"Move worklog · " + m.selectedDate.Format("2006-01-02"), source.IssueKey + " · " + formatDuration(source.DurationSeconds), ""}
	window := func(start time.Time) string {
		return start.In(m.workspace.cfg.Location).Format("15:04") + "–" + start.Add(time.Duration(source.DurationSeconds)*time.Second).In(m.workspace.cfg.Location).Format("15:04")
	}
	lines = append(lines, "Original  "+window(source.StartedAtUTC), "Proposed  "+window(state.destination))
	if state.loading {
		lines = append(lines, "Calculating positions…")
	} else if state.saving {
		lines = append(lines, "Saving…")
	} else {
		lines = append(lines, fmt.Sprintf("%d valid positions · other worklogs stay fixed", len(state.preview.Positions)))
	}
	// Render authoritative bookings without the source so its old slot is free.
	timeline := m
	timeline.worklogs = state.preview.Bookings
	timeline.dayContext = worklogs.ContextDay{}
	timeline.contextSettings = state.preview.Settings
	proposed := source
	proposed.StartedAtUTC = state.destination
	lines = append(lines, "", timeline.renderDayTimelineWidth([]worklogs.LocalWorklog{proposed}, "", width-2), "", timeline.renderTimelineLegendWidth([]worklogs.LocalWorklog{proposed}, "", width-2))
	if state.err != "" {
		lines = append(lines, m.paint(state.err, m.colors.Destructive))
	}
	return m.pane(strings.Join(lines, "\n"), width, height)
}

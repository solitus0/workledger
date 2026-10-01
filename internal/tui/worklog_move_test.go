package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

func moveTestModel(t *testing.T) (model, *fakeWorklogState) {
	t.Helper()
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.activeTab = worklogsTab
	m.worklogLoading = false
	source := worklogs.LocalWorklog{ID: "selected", IssueKey: "ITO-4807", StartedAtUTC: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC), DurationSeconds: 3600, Revision: 3}
	m.worklogs = []worklogs.LocalWorklog{source}
	state := worklogState(ws)
	state.movePreview = worklogs.MovePreview{Source: source, Positions: []time.Time{source.StartedAtUTC, source.StartedAtUTC.Add(4 * time.Hour), source.StartedAtUTC.Add(5 * time.Hour)}, Settings: worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}}
	state.context.Days = []worklogs.ContextDay{{Date: "2026-05-21", Worklogs: m.worklogs}}
	return m, state
}

func TestMoveKeyboardPreviewSaveAndSelection(t *testing.T) {
	m, state := moveTestModel(t)
	if !strings.Contains(stripANSI(m.renderFooter(120)), "M Move") {
		t.Fatal("missing shortcut")
	}
	next, cmd := m.Update(textKey("M"))
	m = next.(model)
	if m.move == nil || !m.move.loading {
		t.Fatal("move not opened")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	for _, key := range []tea.Key{tea.Key{Code: tea.KeyDown}, tea.Key{Code: tea.KeyDown}} {
		next, _ = m.Update(tea.KeyPressMsg(key))
		m = next.(model)
	}
	if !m.move.destination.Equal(state.movePreview.Positions[2]) || m.selectedWorklogID() != "selected" {
		t.Fatal("arrows changed selection or wrong destination")
	}
	content := stripANSI(m.render())
	if !strings.Contains(content, "16:00–17:00") || !strings.Contains(content, "11:00–12:00") || !strings.Contains(content, "▓") {
		t.Fatalf("missing preview: %s", content)
	}
	if lipgloss.Width(content) > 100 || lipgloss.Height(content) > 28 {
		t.Fatal("move exceeds minimum viewport")
	}
	checked := 0
	m.deps.checkWritable = func(string, string) error { checked++; return nil }
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	next, duplicate := m.Update(ctrlKey('s'))
	m = next.(model)
	if duplicate != nil {
		t.Fatal("duplicate submit")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if checked != 1 || len(state.moves) != 1 || state.moves[0].revision != 3 || m.move != nil || m.selectAfterID != "selected" {
		t.Fatalf("save failed: %#v", state.moves)
	}
	next, _ = m.Update(worklogResultMsg{generation: m.worklogGen, date: m.selectedDate, items: []worklogs.LocalWorklog{{ID: "other"}, state.movePreview.Source}})
	m = next.(model)
	if m.selectedWorklogID() != "selected" {
		t.Fatal("selection lost after refresh")
	}
}

func TestMoveCancelPollingAndLateResults(t *testing.T) {
	m, state := moveTestModel(t)
	next, cmd := m.Update(textKey("M"))
	m = next.(model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	next, _ = m.Update(textKey("3"))
	m = next.(model)
	if m.activeTab != worklogsTab {
		t.Fatal("navigation escaped move")
	}
	next, _ = m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if !m.move.loading {
		t.Fatal("poll did not invalidate preview")
	}
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	if cmd != nil {
		t.Fatal("saved invalidated preview")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	next, _ = m.Update(result)
	m = next.(model)
	if m.move != nil || len(state.moves) != 0 {
		t.Fatal("cancel mutated or late preview reopened")
	}
}

func TestMoveRejectsUnavailableSaveAndAllowsRefresh(t *testing.T) {
	m, state := moveTestModel(t)
	next, cmd := m.Update(textKey("M"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	state.updateErr = worklogs.ErrPlacementChanged
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	next, cmd = m.Update(cmd())
	m = next.(model)
	if !m.move.loading || m.move.saving || cmd == nil {
		t.Fatal("conflict did not refresh")
	}
	state.movePreview.Positions = state.movePreview.Positions[:1]
	next, _ = m.Update(cmd())
	m = next.(model)
	if !m.move.destination.Equal(m.move.preview.Source.StartedAtUTC) {
		t.Fatal("invalid destination retained")
	}
	state.moveErr = errors.New("source changed")
	next, cmd = m.Update(textKey("r"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if len(m.move.preview.Positions) != 0 {
		t.Fatal("failed preview retained save targets")
	}
}

func TestMoveKeyboardToSQLiteAndActivity(t *testing.T) {
	ws, deps := testWorkspace(t)
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := worklogs.NewService(store)
	ws.cfg.DayEnd = "17:00"
	ws.cfg.DailyLunch = "12:00-12:45"
	ws.worklogs = service
	ws.mutations = service
	ws.activities = &fakeActivities{}
	var source worklogs.LocalWorklog
	for _, slot := range [][2]string{{"08:00", "3h"}, {"11:00", "1h"}, {"13:00", "2h"}} {
		result, err := service.Add(t.Context(), ws.cfg, worklogs.AddInput{IssueKey: "ITO-4807", Started: "2026-05-21T" + slot[0], Duration: slot[1], Description: "current session work"})
		if err != nil {
			t.Fatal(err)
		}
		if slot[0] == "11:00" {
			source = result.Records[0]
		}
	}
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.activeTab = worklogsTab
	cmd := m.startWorklogLoad()
	next, _ := m.Update(cmd())
	m = next.(model)
	m.restoreWorklogSelection(source.ID)
	next, cmd = m.Update(textKey("M"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	for range 2 {
		next, _ = m.Update(specialKey(tea.KeyDown))
		m = next.(model)
	}
	for _, noColor := range []bool{true, false} {
		m.deps.noColor = noColor
		m.activityOpen = true
		view := m.render()
		if lipgloss.Width(view) > 100 || lipgloss.Height(view) > 28 || !strings.Contains(stripANSI(view), "16:00–17:00") {
			t.Fatal("move preview clipped")
		}
	}
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	moved, err := service.Show(t.Context(), source.ID)
	if err != nil || moved.StartedAtUTC.Format("15:04") != "16:00" || moved.DurationSeconds != 3600 || moved.Revision != source.Revision+1 {
		t.Fatalf("persisted move = %#v, %v", moved, err)
	}
	entries := ws.activities.(*fakeActivities)
	if len(entries.started) != 1 || entries.started[0].Operation != "worklogs.update" || len(entries.finished) != 1 {
		t.Fatal("move activity not recorded")
	}
	cmd = m.startWorklogLoad()
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.selectedWorklogID() != source.ID || m.dayBookedSeconds() != 21600 {
		t.Fatal("selection or daily total changed")
	}
}

func TestMoveAvailabilityAndWritableFailure(t *testing.T) {
	m, state := moveTestModel(t)
	m.worklogs = nil
	next, cmd := m.Update(textKey("M"))
	m = next.(model)
	if m.move != nil || cmd != nil {
		t.Fatal("move available without selection")
	}
	m.worklogs = []worklogs.LocalWorklog{state.movePreview.Source}
	m.worklogView = weekWorklogView
	next, cmd = m.Update(textKey("M"))
	m = next.(model)
	if m.move != nil || cmd != nil {
		t.Fatal("move available in week mode")
	}
	m.worklogView = dayWorklogView
	next, cmd = m.Update(textKey("M"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	m.deps.checkWritable = func(string, string) error { return errors.New("read only storage") }
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if len(state.moves) != 0 || m.move == nil || m.move.saving || !strings.Contains(m.move.err, "read only") {
		t.Fatal("writability failure not guarded")
	}
}

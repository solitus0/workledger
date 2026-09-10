package tui

import (
	"context"
	"github.com/solitus0/workledger/internal/worklogs"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTrashTabFiltersAndRestoresOnlyLocalRows(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	originalID := "local-id"
	m.trashItems = []worklogs.TrashRecord{
		{ID: "remote", StorageScope: worklogs.TrashScopeRemote, IssueKey: "APP-1"},
		{ID: "local", StorageScope: worklogs.TrashScopeLocal, SourceWorklogID: &originalID, IssueKey: "APP-2", DurationSeconds: 3600},
	}
	next, _ := m.Update(textKey("5"))
	m = next.(model)
	if m.activeTab != trashTab || !strings.Contains(m.render(), "Trash") || !strings.Contains(m.render(), "[All]") {
		t.Fatalf("trash tab did not render expected defaults")
	}
	next, _ = m.Update(textKey("R"))
	m = next.(model)
	if m.overlay != noOverlay || !strings.Contains(m.notice, "audit-only") {
		t.Fatalf("remote row should be audit-only: overlay=%v notice=%q", m.overlay, m.notice)
	}
	next, _ = m.Update(textKey("j"))
	m = next.(model)
	next, _ = m.Update(textKey("R"))
	m = next.(model)
	if m.overlay != restoreOverlay || len(m.restoreSelection) != 1 || !strings.Contains(m.render(), "Original ID local-id") {
		t.Fatalf("local restore confirmation missing")
	}
}

func TestTrashWeekRestoreSnapshotsOnlyEligibleLocalSubset(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.activeTab = trashTab
	m.trashView = weekWorklogView
	id := "source"
	m.trashItems = []worklogs.TrashRecord{{ID: "local", StorageScope: worklogs.TrashScopeLocal, SourceWorklogID: &id, DurationSeconds: 1800}, {ID: "remote", StorageScope: worklogs.TrashScopeRemote, DurationSeconds: 1800}}
	next, _ := m.Update(textKey("R"))
	m = next.(model)
	if m.overlay != restoreDayOverlay || len(m.restoreSelection) != 1 || m.restoreSelection[0].ID != "local" {
		t.Fatalf("week restore selection=%#v", m.restoreSelection)
	}
}

func TestTrashWeekFitsMinimumViewportWithoutColor(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.activeTab = trashTab
	m.trashView = weekWorklogView
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("trash render=%dx%d", width, height)
	}
	if strings.Contains(rendered, "\x1b[") || !strings.Contains(rendered, "[Week]") || !strings.Contains(rendered, "[All]") {
		t.Fatalf("trash minimum no-color render missing state")
	}
}

func TestTrashDetailDoesNotRepeatRestoreShortcut(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)
	m.activeTab = trashTab
	originalID := "source"
	m.trashItems = []worklogs.TrashRecord{{
		ID:              "local",
		StorageScope:    worklogs.TrashScopeLocal,
		SourceWorklogID: &originalID,
	}}

	if detail := m.renderTrashDetail(); strings.Contains(detail, "with R") || !strings.Contains(detail, "Restorable") {
		t.Fatalf("day detail should describe restore state without its shortcut: %q", detail)
	}
	if footer := m.renderFooter(120); !strings.Contains(footer, "R Restore") {
		t.Fatalf("action bar missing restore shortcut: %q", footer)
	}

	m.trashView = weekWorklogView
	if detail := m.renderTrashDetail(); strings.Contains(detail, "with R") || !strings.Contains(detail, "restored atomically") {
		t.Fatalf("week detail should describe restore behavior without its shortcut: %q", detail)
	}
}

func TestDeleteAndRestoreShortcutsAppearOnlyInActionBar(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)

	tests := []struct {
		name           string
		overlay        overlay
		footerShortcut string
	}{
		{name: "delete worklog", overlay: deleteOverlay, footerShortcut: "Confirm delete"},
		{name: "delete day", overlay: deleteDayOverlay, footerShortcut: "Move day to trash"},
		{name: "restore worklog", overlay: restoreOverlay, footerShortcut: "Restore"},
		{name: "restore day", overlay: restoreDayOverlay, footerShortcut: "Restore"},
		{name: "delete preset", overlay: deletePresetOverlay, footerShortcut: "Delete preset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.overlay = tt.overlay
			modal := m.renderConfirmation(100, 24)
			if strings.Contains(modal, "y / Enter") || strings.Contains(modal, "n / Esc") {
				t.Fatalf("modal repeats action-bar shortcuts: %q", modal)
			}
			footer := m.renderFooter(120)
			if !strings.Contains(footer, "y / Enter") || !strings.Contains(footer, tt.footerShortcut) {
				t.Fatalf("action bar missing shortcut %q: %q", tt.footerShortcut, footer)
			}
		})
	}
}

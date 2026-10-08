package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestIssuePickerModesPagingDetailsAndDraft(t *testing.T) {
	ws, deps := testWorkspace(t)
	state := worklogState(ws)
	state.issueDetails = map[string][]worklogs.IssueWorklogDescription{}
	for i := 124; i >= 0; i-- {
		key := fmt.Sprintf("APP-%03d", i)
		state.usedIssues = append(state.usedIssues, worklogs.UsedIssue{IssueKey: key})
		state.issueDetails[key] = []worklogs.IssueWorklogDescription{{Description: "recognizable work", StartedAtUTC: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC), DurationSeconds: 5400}}
	}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	m.form.inputs[descriptionInput].SetValue("unfinished draft")
	next, cmd := m.Update(ctrlKey('k'))
	m = next.(model)
	if m.issuePicker == nil || m.issuePicker.searching || cmd == nil {
		t.Fatalf("picker did not open in browse mode: %#v", m.issuePicker)
	}
	next, cmd = m.Update(cmd())
	m = next.(model)
	if got := m.issuePicker.items[0].IssueKey; got != "APP-124" {
		t.Fatalf("first key = %q", got)
	}
	if cmd == nil {
		t.Fatal("details did not load")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	view := m.render()
	if !strings.Contains(view, "recognizable work") || !strings.Contains(view, "2026-05-04 · 1h 30m") || strings.Contains(view, "\x1b[") {
		t.Fatalf("detail pane / NO_COLOR failed:\n%s", view)
	}
	if width, height := lipgloss.Width(view), lipgloss.Height(view); width != minimumWidth || height != minimumHeight {
		t.Fatalf("picker render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for page := 1; page <= 2; page++ {
		next, cmd = m.Update(specialKey(tea.KeyPgDown))
		m = next.(model)
		if cmd == nil {
			t.Fatalf("page %d unreachable", page)
		}
		next, _ = m.Update(cmd())
		m = next.(model)
	}
	if m.issuePicker.page != 2 || m.issuePicker.items[len(m.issuePicker.items)-1].IssueKey != "APP-000" {
		t.Fatalf("oldest key unreachable: %#v", m.issuePicker)
	}
	next, _ = m.Update(ctrlKey('f'))
	m = next.(model)
	if !m.issuePicker.searching || m.issuePicker.mode != worklogs.IssueFindByPrefix {
		t.Fatal("prefix search did not open")
	}
	for _, letter := range "APP-000" {
		next, _ = m.Update(textKey(string(letter)))
		m = next.(model)
	}
	next, _ = m.Update(m.issuePickerCmd()())
	m = next.(model)
	if len(m.issuePicker.items) != 1 || m.issuePicker.items[0].IssueKey != "APP-000" {
		t.Fatalf("prefix results = %#v", m.issuePicker.items)
	}
	next, cmd = m.Update(ctrlKey('f'))
	m = next.(model)
	if m.issuePicker.mode != worklogs.IssueFindByDescription || m.issuePicker.query != "" {
		t.Fatal("description mode did not reset find")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	for _, letter := range "recognizable" {
		next, _ = m.Update(textKey(string(letter)))
		m = next.(model)
	}
	next, _ = m.Update(m.issuePickerCmd()())
	m = next.(model)
	if len(m.issuePicker.items) != 50 || m.issuePicker.mode != worklogs.IssueFindByDescription {
		t.Fatalf("description results = %#v", m.issuePicker)
	}
	if state.usedIssueCalls[len(state.usedIssueCalls)-1].mode != worklogs.IssueFindByDescription {
		t.Fatal("description query did not reach service")
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.issuePicker != nil || m.form.inputs[issueInput].Value() != "APP-124" || m.form.inputs[descriptionInput].Value() != "unfinished draft" {
		t.Fatal("selection lost draft")
	}
	if len(state.addInputs) != 0 {
		t.Fatal("picker saved worklog")
	}

	next, cmd = m.Update(ctrlKey('k'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(ctrlKey('f'))
	m = next.(model)
	next, cmd = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issuePicker == nil || m.issuePicker.searching || cmd == nil {
		t.Fatal("first Esc did not close search")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issuePicker != nil || m.form.inputs[issueInput].Value() != "APP-124" || m.form.inputs[descriptionInput].Value() != "unfinished draft" {
		t.Fatal("cancel changed draft")
	}
}

func TestIssuePickerIgnoresStaleResultsAndFailures(t *testing.T) {
	ws, deps := testWorkspace(t)
	state := worklogState(ws)
	state.usedIssues = []worklogs.UsedIssue{{IssueKey: "APP-1"}, {IssueKey: "APP-2"}}
	state.issueDetails = map[string][]worklogs.IssueWorklogDescription{"APP-1": {{Description: "one"}}, "APP-2": {{Description: "two"}}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	old := m.openIssuePicker()
	next, _ := m.Update(ctrlKey('f'))
	m = next.(model)
	next, _ = m.Update(textKey("A"))
	m = next.(model)
	next, _ = m.Update(old())
	m = next.(model)
	if len(m.issuePicker.items) != 0 || m.issuePicker.query != "A" {
		t.Fatal("stale browse result applied")
	}
	next, cmd := m.Update(m.issuePickerCmd()())
	m = next.(model)
	if cmd == nil {
		t.Fatal("missing detail query")
	}
	staleDetail := cmd()
	next, cmd = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	if cmd == nil {
		t.Fatal("selection did not reload details")
	}
	next, _ = m.Update(staleDetail)
	m = next.(model)
	if len(m.issuePicker.details) != 0 {
		t.Fatal("stale detail result applied")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.issuePicker.detailIssue != "APP-2" || len(m.issuePicker.details) != 1 {
		t.Fatalf("selected detail = %#v", m.issuePicker)
	}
	state.usedIssueErr = errors.New("offline local query")
	next, _ = m.Update(m.issuePickerCmd()())
	m = next.(model)
	if m.issuePicker.err == "" || m.form == nil || !strings.Contains(m.render(), "Issue history unavailable") {
		t.Fatal("query failure damaged form")
	}
	state.issueDetailErr = errors.New("offline detail query")
	m.issuePicker.err = ""
	next, _ = m.Update(m.loadIssuePickerDetails()())
	m = next.(model)
	if m.issuePicker.detailErr == "" || !strings.Contains(m.render(), "Descriptions unavailable") {
		t.Fatal("detail failure not shown")
	}
}

func TestIssuePickerIgnoresResultsFromPreviousOpening(t *testing.T) {
	ws, deps := testWorkspace(t)
	state := worklogState(ws)
	state.usedIssues = []worklogs.UsedIssue{{IssueKey: "APP-1"}}
	state.issueDetails = map[string][]worklogs.IssueWorklogDescription{"APP-1": {{Description: "old detail"}}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	firstPage := m.openIssuePicker()
	oldPage := firstPage()
	next, detailCmd := m.Update(oldPage)
	m = next.(model)
	oldDetail := detailCmd()
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	state.issueDetails["APP-1"] = []worklogs.IssueWorklogDescription{{Description: "new detail"}}
	secondPage := m.openIssuePicker()
	next, _ = m.Update(oldPage)
	m = next.(model)
	if !m.issuePicker.loading || len(m.issuePicker.items) != 0 {
		t.Fatal("old page applied to reopened picker")
	}
	next, detailCmd = m.Update(secondPage())
	m = next.(model)
	next, _ = m.Update(oldDetail)
	m = next.(model)
	if !m.issuePicker.detailLoad || len(m.issuePicker.details) != 0 {
		t.Fatal("old detail applied to reopened picker")
	}
	next, _ = m.Update(detailCmd())
	m = next.(model)
	if len(m.issuePicker.details) != 1 || m.issuePicker.details[0].Description != "new detail" {
		t.Fatalf("new detail = %#v", m.issuePicker.details)
	}
}

func TestIssuePickerEntryDateAndDuration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration int
		search   bool
		header   string
	}{
		{name: "minutes", duration: 900, header: "2026-10-07 · 0h 15m"},
		{name: "multi-hour search match", duration: 12600, search: true, header: "* 2026-10-07 · 3h 30m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			ws.cfg.Location = time.FixedZone("local", 2*60*60)
			state := worklogState(ws)
			state.usedIssues = []worklogs.UsedIssue{{IssueKey: "APP-1"}}
			state.issueDetails = map[string][]worklogs.IssueWorklogDescription{"APP-1": {{
				StartedAtUTC:    time.Date(2026, 10, 6, 23, 0, 0, 0, time.UTC),
				DurationSeconds: tc.duration,
				Description:     "Recognizable work",
			}}}
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = minimumWidth, minimumHeight
			m.openAddForm()
			cmd := m.openIssuePicker()
			next, cmd := m.Update(cmd())
			m = next.(model)
			next, _ = m.Update(cmd())
			m = next.(model)
			if tc.search {
				m.issuePicker.searching = true
				m.issuePicker.mode = worklogs.IssueFindByDescription
				m.issuePicker.query = "recognizable"
			}
			view := m.render()
			if !strings.Contains(view, tc.header) || !strings.Contains(view, "Recognizable work") || strings.Contains(view, "\x1b[") {
				t.Fatalf("entry header / NO_COLOR failed:\n%s", view)
			}
			if width, height := lipgloss.Width(view), lipgloss.Height(view); width != minimumWidth || height != minimumHeight {
				t.Fatalf("picker render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
			}
		})
	}
}

func TestIssuePickerScrollsAllDescriptions(t *testing.T) {
	ws, deps := testWorkspace(t)
	state := worklogState(ws)
	state.usedIssues = []worklogs.UsedIssue{{IssueKey: "APP-1"}}
	state.issueDetails = map[string][]worklogs.IssueWorklogDescription{}
	for i := 0; i < 60; i++ {
		state.issueDetails["APP-1"] = append(state.issueDetails["APP-1"], worklogs.IssueWorklogDescription{Description: fmt.Sprintf("description %02d", i)})
	}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	cmd := m.openIssuePicker()
	next, cmd := m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if !m.issuePicker.detailFocus {
		t.Fatal("detail pane did not focus")
	}
	for i := 0; i < 30; i++ {
		next, cmd = m.Update(specialKey(tea.KeyPgDown))
		m = next.(model)
		if cmd != nil {
			next, _ = m.Update(cmd())
			m = next.(model)
		}
	}
	if len(m.issuePicker.details) != 60 || !strings.Contains(m.render(), "description 59") {
		t.Fatalf("details not fully reachable: loaded=%d", len(m.issuePicker.details))
	}
}

func TestIssuePickerShowsEmptySearchAndCancelKeepsDraft(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	m.form.inputs[descriptionInput].SetValue("keep this")
	cmd := m.openIssuePicker()
	next, _ := m.Update(cmd())
	m = next.(model)
	if !strings.Contains(m.render(), "No matching issues") || !strings.Contains(m.render(), "Select an issue") {
		t.Fatalf("empty picker state missing:\n%s", m.render())
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issuePicker != nil || m.form.inputs[descriptionInput].Value() != "keep this" {
		t.Fatal("cancel lost draft")
	}
}

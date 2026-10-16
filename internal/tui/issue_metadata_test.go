package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestEstimateUsageAndMetadataLayout(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		ws, deps := testWorkspace(t)
		deps.noColor = noColor
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height = minimumWidth, minimumHeight
		m.worklogs = testRows()
		m.worklogLoading = false
		key := m.selectedWorklog().IssueKey
		estimate := int64(16 * 3600)
		m.issueMetadata = map[string]worklogs.IssueMetadata{key: {IssueKey: key, Title: "Product data lifecycle", MaxEstimateSeconds: &estimate}}
		m.issueTotalIssue, m.issueTotal = key, 10*3600
		for _, scenario := range []struct {
			total    int
			loading  bool
			failure  string
			expected string
		}{
			{10 * 3600, false, "", "62.5% (10h / 16h)"},
			{20 * 3600, false, "", "125.0% (20h / 16h) · 4h over estimate"},
			{0, false, "", "0.0% (0h / 16h)"},
			{0, true, "", "Estimate used:    Loading…"},
			{0, false, "failed", "Estimate used:    Unavailable"},
		} {
			m.issueTotal, m.issueTotalLoading, m.issueTotalErr = scenario.total, scenario.loading, scenario.failure
			if detail := m.renderWorklogDetail(); !strings.Contains(detail, scenario.expected) || !strings.Contains(detail, "Product data lifecycle") {
				t.Fatalf("detail: %s", detail)
			}
			compactParts := []string{strings.Join(strings.Fields(scenario.expected), " ")}
			if parts := strings.SplitN(scenario.expected, " (", 2); len(parts) == 2 {
				durations := strings.SplitN(parts[1], ")", 2)
				compactParts = []string{"Estimate used: " + parts[0], durations[0]}
				if durations[1] != "" {
					compactParts = append(compactParts, strings.TrimPrefix(durations[1], " · "))
				}
			}
			view := m.render()
			for _, expected := range compactParts {
				if !strings.Contains(view, expected) {
					t.Fatalf("estimate usage %q hidden at minimum size: %s", expected, view)
				}
			}
			m.activityOpen = true
			drawerView := m.render()
			m.activityOpen = false
			for _, expected := range compactParts {
				if !strings.Contains(drawerView, expected) {
					t.Fatalf("metric %q hidden with Activity: %s", expected, drawerView)
				}
			}
			if lipgloss.Width(drawerView) != minimumWidth || lipgloss.Height(drawerView) != minimumHeight {
				t.Fatalf("estimate usage with activity drawer: %s", drawerView)
			}
			if lipgloss.Width(view) != minimumWidth || lipgloss.Height(view) != minimumHeight {
				t.Fatalf("detail layout: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
			}
			if noColor && strings.Contains(view, "\x1b[") {
				t.Fatal("NO_COLOR emitted ANSI")
			}
		}
		for _, maximum := range []*int64{nil, new(int64)} {
			m.issueMetadata[key] = worklogs.IssueMetadata{IssueKey: key, MaxEstimateSeconds: maximum}
			if strings.Contains(m.renderWorklogDetail(), "Estimate used") {
				t.Fatal("missing/zero estimate rendered percentage")
			}
		}
	}
}

func TestFormMetadataWithoutDurationIgnoresStaleAndRemovedValues(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("SBAH-2")
	worklogState(ws).metadata = map[string]worklogs.IssueMetadata{"SBAH-2": {IssueKey: "SBAH-2", Title: "Product data lifecycle"}}
	cmd := m.schedulePreview()
	if cmd == nil {
		t.Fatal("metadata did not load before duration")
	}
	msg := cmd()
	next, _ := m.Update(msg)
	m = next.(model)
	if !strings.Contains(m.renderIssueContext(), "Product data lifecycle") {
		t.Fatalf("metadata missing: %#v", m.issueMetadata)
	}
	if view := m.render(); !strings.Contains(view, "Product data lifecycle") {
		t.Fatalf("title hidden in minimum-size add form: %s", view)
	}
	m.form.inputs[issueInput].SetValue("SBAH-3")
	next, _ = m.Update(msg)
	m = next.(model)
	if strings.Contains(m.renderIssueContext(), "Product data lifecycle") {
		t.Fatal("stale metadata rendered for new issue")
	}
	m.form.inputs[issueInput].SetValue("SBAH-2")
	m.formMetadataGen++
	next, _ = m.Update(formMetadataResultMsg{generation: m.formMetadataGen, issueKey: "SBAH-2", err: errors.New("unavailable")})
	m = next.(model)
	if m.renderIssueContext() != "" {
		t.Fatal("failed metadata read left stale title")
	}
}

func TestUnusedIssuePickerTitleModeAndLayout(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		ws, deps := testWorkspace(t)
		deps.noColor = noColor
		state := worklogState(ws)
		estimate := int64(16 * 3600)
		state.usedIssues = []worklogs.UsedIssue{{IssueKey: "SBAH-2", Title: "Product data lifecycle", MaxEstimateSeconds: &estimate}}
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height = minimumWidth, minimumHeight
		m.openAddForm()
		cmd := m.openIssuePicker()
		next, _ := m.Update(cmd())
		m = next.(model)
		view := m.render()
		if !strings.Contains(view, "Unused") || strings.Contains(view, "0001-") || !strings.Contains(view, "Max estimate: 16h") || !strings.Contains(view, "Product data") {
			t.Fatalf("picker: %s", view)
		}
		if lipgloss.Width(view) != minimumWidth || lipgloss.Height(view) != minimumHeight {
			t.Fatalf("picker dimensions: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
		}
		if noColor && strings.Contains(view, "\x1b[") {
			t.Fatal("picker emitted ANSI")
		}
		for range 2 {
			next, _ = m.Update(ctrlKey('t'))
			m = next.(model)
		}
		if m.issuePicker.mode != worklogs.IssueFindByTitle {
			t.Fatal("title mode unreachable")
		}
		m.issuePicker.input.SetValue("LIFECYCLE")
		cmd = m.loadIssuePickerPage(0, false)
		next, _ = m.Update(cmd())
		m = next.(model)
		if len(m.issuePicker.items) != 1 {
			t.Fatal("title query did not reach service")
		}
	}
}

func TestExternalMetadataRefreshUpdatesUsageAndPreservesSelection(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.worklogs = testRows()
	selected := *m.selectedWorklog()
	oldEstimate, newEstimate := int64(8*3600), int64(16*3600)
	m.issueMetadata = map[string]worklogs.IssueMetadata{selected.IssueKey: {Title: "Old title", MaxEstimateSeconds: &oldEstimate}}
	worklogState(ws).issueTotals = map[string]int{selected.IssueKey: 10 * 3600}
	next, _ := m.Update(pollResultMsg{changed: true})
	m = next.(model)
	next, cmd := m.Update(worklogResultMsg{generation: m.worklogGen, date: m.selectedDate, items: m.worklogs,
		metadata: map[string]worklogs.IssueMetadata{selected.IssueKey: {Title: "Updated title", MaxEstimateSeconds: &newEstimate}}})
	m = next.(model)
	if m.selectedWorklog().ID != selected.ID || cmd == nil {
		t.Fatal("metadata refresh lost selection or skipped aggregate refresh")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if detail := m.renderWorklogDetail(); !strings.Contains(detail, "Updated title") || !strings.Contains(detail, "62.5% (10h / 16h)") || strings.Contains(detail, "Old title") {
		t.Fatalf("external metadata stale: %s", detail)
	}
	m.openAddForm()
	m.form.inputs[issueInput].SetValue(selected.IssueKey)
	m.form.inputs[durationInput].SetValue("1h")
	next, _ = m.Update(previewResultMsg{generation: m.form.previewGeneration})
	m = next.(model)
	if m.renderIssueContext() != "" {
		t.Fatal("missing metadata in refreshed preview retained removed decoration")
	}
}

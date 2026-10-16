package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestHelpPreservesEveryWindowAndRemainsVisible(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*model)
	}{
		{"worklog editor", func(m *model) {
			m.activeTab = worklogsTab
			m.openAddForm()
			m.form.inputs[descriptionInput].SetValue("Unfinished description")
			m.form.inputs[descriptionInput].SetCursor(3)
		}},
		{"preset editor", func(m *model) { m.openPresetForm(nil) }},
		{"issue editor", func(m *model) { m.openIssueForm(m.selectedIssue()) }},
		{"saving issue editor", func(m *model) { m.openIssueForm(nil); m.issueForm.saving = true }},
		{"issue picker", func(m *model) { m.openAddForm(); m.openIssuePicker() }},
		{"issue search", func(m *model) {
			m.openAddForm()
			m.openIssuePicker()
			next, _ := m.Update(actionKey("/"))
			*m = next.(model)
			m.issuePicker.input.SetValue("SBAH-2")
		}},
		{"preset picker", func(m *model) {
			m.presets = []presets.Preset{{Name: "Standup", IssueKey: "APP-1", DurationSeconds: 900}}
			m.openPresetPicker()
		}},
		{"preset search", func(m *model) {
			m.presets = []presets.Preset{{Name: "Standup", IssueKey: "APP-1", DurationSeconds: 900}}
			m.openPresetPicker()
			next, _ := m.Update(actionKey("/"))
			*m = next.(model)
			m.presetPicker.input.SetValue("draft")
		}},
		{"move", func(m *model) {
			m.activeTab = worklogsTab
			m.openMove(worklogs.LocalWorklog{ID: "move", IssueKey: "APP-1", StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: 900})
		}},
		{"saving move", func(m *model) {
			m.activeTab = worklogsTab
			m.openMove(worklogs.LocalWorklog{ID: "move", IssueKey: "APP-1", StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: 900})
			m.move.saving = true
		}},
		{"sort", func(m *model) { m.openIssueSort() }},
		{"epic assignment", func(m *model) {
			m.issueMarks = map[string]bool{"SBAH-2": true}
			next, _ := m.openEpicAssignment()
			*m = next.(model)
			m.epicAssignment.input.SetValue("October")
		}},
		{"saving epic assignment", func(m *model) {
			m.issueMarks = map[string]bool{"SBAH-2": true}
			next, _ := m.openEpicAssignment()
			*m = next.(model)
			m.epicAssignment.saving = true
		}},
		{"details", func(m *model) {
			m.openDetails("Test details", strings.Repeat("content\n", 30))
			m.detailViewport.GotoBottom()
		}},
		{"issue history", func(m *model) { next, _ := m.openIssueHistory(); *m = next.(model) }},
		{"plan editor", func(m *model) { m.activeTab = plansTab; m.openPlanForm() }},
		{"plan operation", func(m *model) {
			m.activeTab = plansTab
			m.planOperation = &planOperationState{kind: "plan.reconcile", cancel: func() {}}
		}},
		{"issue filter", func(m *model) { *m = sendIssueKey(*m, "/"); m.issueSearch.input.SetValue("SBAH") }},
		{"epic filter", func(m *model) {
			m.issueView = epicListView
			*m = sendIssueKey(*m, "/")
			m.epicSearch.input.SetValue("Document")
		}},
		{"worklog filter", func(m *model) {
			m.activeTab = worklogsTab
			m.openWorklogFilter()
			m.worklogFilter.input.SetValue("draft")
		}},
		{"activity", func(m *model) { m.activityOpen = true; m.focus = focusActivity }},
		{"navigation rail", func(m *model) { m.focus = focusRail }},
	}
	for _, overlay := range []overlay{deleteOverlay, deleteDayOverlay, forceOverlay, reloadOverlay, restoreOverlay, restoreDayOverlay, deletePresetOverlay, reconcilePlanOverlay, applyPlanOverlay, retryFailedPlanOverlay, retryUncertainPlanOverlay} {
		cases = append(cases, struct {
			name  string
			setup func(*model)
		}{
			name: "confirmation " + string(rune('A'+overlay)),
			setup: func(m *model) {
				m.activeTab = plansTab
				m.plan = &reconcile.Plan{ID: "test", Direction: "push"}
				m.openPlanForm()
				m.overlay = overlay
			},
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := issueFixture(t)
			m.width, m.height = minimumWidth, minimumHeight
			tc.setup(&m)
			before, overlay, focus, kind := m.render(), m.overlay, m.focus, m.interactionState.kind()
			if !strings.Contains(stripANSI(m.renderFooter(m.width)), "? Actions") {
				t.Fatal("help hint missing")
			}
			label := m.actionContextLabel()
			next, _ := m.Update(actionKey("?"))
			m = next.(model)
			if m.actions == nil || m.overlay != overlay || m.focus != focus {
				t.Fatal("help replaced underlying context")
			}
			if !strings.Contains(stripANSI(m.render()), "Actions · "+label) {
				t.Fatalf("wrong help context: %s", m.render())
			}
			assertRedesignBounds(t, m, tc.name+" help")
			next, _ = m.Update(actionKey("esc"))
			m = next.(model)
			if m.actions != nil || m.overlay != overlay || m.interactionState.kind() != kind || m.render() != before {
				t.Fatal("closing help changed underlying window")
			}
			next, _ = m.Update(actionKey("?"))
			m = next.(model)
			next, _ = m.Update(actionKey("?"))
			m = next.(model)
			if m.actions != nil || m.render() != before {
				t.Fatal("help toggle lost context")
			}
		})
	}
}

func TestHelpDispatchesPickerModeShortcutAndPreservesDraft(t *testing.T) {
	m, _ := issueFixture(t)
	m.openAddForm()
	m.form.inputs[descriptionInput].SetValue("Draft")
	m.openIssuePicker()
	m = sendIssueKey(m, "?")
	m.actions.input.SetValue("Cycle key")
	if len(m.matchingActions()) != 1 {
		t.Fatal("mode action unavailable")
	}
	m = sendIssueKey(m, "enter")
	if m.actions != nil || m.issuePicker == nil || !m.issuePicker.searching || m.issuePicker.mode != worklogs.IssueFindByDescription || m.form.inputs[descriptionInput].Value() != "Draft" {
		t.Fatal("help shortcut did not reach picker")
	}
}

func TestIssueHistoryResultsContinueBehindHelp(t *testing.T) {
	m, _ := issueFixture(t)
	next, _ := m.openIssueHistory()
	m = next.(model)
	m = sendIssueKey(m, "?")
	next, _ = m.Update(issueHistoryResultMsg{generation: m.issueHistory.generation, issueKey: m.issueHistory.issueKey, items: []worklogs.IssueWorklogDescription{{Description: "Arrived during help", StartedAtUTC: time.Now(), DurationSeconds: 900}}})
	m = next.(model)
	if m.actions == nil || len(m.issueHistory.items) != 1 {
		t.Fatal("help blocked background history")
	}
	m = sendIssueKey(m, "esc")
	if !strings.Contains(stripANSI(m.render()), "Arrived during help") {
		t.Fatal("history result lost when help closed")
	}
}

func TestIssuePickerFindIsSeparateFromModeAndPreservesQueryOnRefocus(t *testing.T) {
	m, _ := issueFixture(t)
	m.openAddForm()
	m.openIssuePicker()
	m = sendIssueKey(m, "/")
	m.issuePicker.input.SetValue("SBAH")
	m.issuePicker.detailFocus = true
	m = sendIssueKey(m, "/")
	if m.issuePicker.detailFocus || m.issuePicker.input.Value() != "SBAH" || m.issuePicker.mode != worklogs.IssueFindByPrefix {
		t.Fatal("Find changed query or mode")
	}
	m = sendIssueKey(m, "ctrl+t")
	if m.issuePicker.mode != worklogs.IssueFindByDescription {
		t.Fatal("mode did not advance")
	}
	m = sendIssueKey(m, "ctrl+t")
	if m.issuePicker.mode != worklogs.IssueFindByTitle {
		t.Fatal("title mode unavailable")
	}
	m = sendIssueKey(m, "ctrl+t")
	if m.issuePicker.mode != worklogs.IssueFindByPrefix {
		t.Fatal("mode did not cycle")
	}
}

func TestPasteLiteralQuestionMarkAndHelpPasteIsolation(t *testing.T) {
	m, _ := issueFixture(t)
	m.openIssueForm(m.selectedIssue())
	m.issueForm.inputs[1].SetValue("")
	next, _ := m.Update(tea.PasteMsg{Content: "Question?"})
	m = next.(model)
	if m.actions != nil || m.issueForm.inputs[1].Value() != "Question?" {
		t.Fatal("pasted question mark opened help or lost text")
	}
	m.issueForm = nil
	m.issueMarks = map[string]bool{"SBAH-2": true}
	next, _ = m.openEpicAssignment()
	m = next.(model)
	m.epicAssignment.input.SetValue("October")
	m = sendIssueKey(m, "?")
	next, _ = m.Update(tea.PasteMsg{Content: "Cancel"})
	m = next.(model)
	if m.actions.input.Value() != "Cancel" || m.epicAssignment.input.Value() != "October" {
		t.Fatal("help paste reached the underlying epic picker")
	}
}

func TestCompletedOperationDoesNotResurrectItsWindowAfterHelp(t *testing.T) {
	m, _ := issueFixture(t)
	m.activeTab = plansTab
	m.openPlanForm()
	m.planOperation = &planOperationState{kind: "plan.reconcile", cancel: func() {}}
	m = sendIssueKey(m, "?")
	next, _ := m.Update(planOperationResultMsg{kind: "plan.reconcile"})
	m = next.(model)
	if m.actions == nil || m.planOperation != nil || m.planForm != nil || m.actionContextLabel() != "Plans" {
		t.Fatal("help retained an obsolete operation context")
	}
	m = sendIssueKey(m, "esc")
	if m.actions != nil || m.planOperation != nil || m.planForm != nil {
		t.Fatal("closing help resurrected a finished operation")
	}
}

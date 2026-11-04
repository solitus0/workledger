package tui

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestPresetPickerKeepsKeyboardSelectionVisible(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	for i := 0; i < 40; i++ {
		m.presets = append(m.presets, presets.Preset{Name: fmt.Sprintf("review-%02d", i), IssueKey: "APP-1", DurationSeconds: 900})
	}
	m.openPresetPicker()
	for _, key := range []rune{tea.KeyDown, tea.KeyPgDown, tea.KeyEnd, tea.KeyPgUp, tea.KeyHome} {
		next, _ := m.Update(specialKey(key))
		m = next.(model)
		name := m.presetPicker.items[m.presetPicker.selected].Name
		if !strings.Contains(m.render(), "> "+name) {
			t.Fatalf("selected %q is hidden after %v", name, key)
		}
		assertRedesignBounds(t, m, "preset viewport")
	}
	m.presetPicker.searching = true
	m.presetPicker.input.SetValue("review-3")
	m.filterPresetPicker()
	next, _ := m.Update(specialKey(tea.KeyEnd))
	m = next.(model)
	if m.presetPicker.items[m.presetPicker.selected].Name != "review-39" || !strings.Contains(m.render(), "> review-39") {
		t.Fatal("filtered selection is hidden")
	}
}

func TestNarrowPlacementProtectsControlsAndExactTimes(t *testing.T) {
	for _, mode := range []placementMode{manualPlacement, fitPlacement, fillPlacement} {
		for _, theme := range []string{"light", "dark", "mono"} {
			t.Run(mode.String()+theme, func(t *testing.T) {
				ws, deps := testWorkspace(t)
				deps.noColor = theme == "mono"
				m := newModel(t.Context(), deps, ws, nil)
				m.width, m.height = 100, 28
				if theme == "light" {
					m.colors = deps.theme.Light
				}
				m.openAddForm()
				m.form.placement = mode
				m.form.inputs[issueInput].SetValue("APP-1")
				m.form.inputs[durationInput].SetValue("1h")
				m.form.inputs[descriptionInput].SetValue(strings.Repeat("Detailed work description ", 8))
				m.contextSettings.Lunch = &worklogs.ContextLunch{Start: "12:00", End: "12:45"}
				m.form.previewRecords = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 1800}, {StartedAtUTC: m.selectedDate.Add(14 * time.Hour), DurationSeconds: 900}, {StartedAtUTC: m.selectedDate.Add(16 * time.Hour), DurationSeconds: 900}}
				view := ansi.Strip(m.render())
				for _, cue := range []string{"Worklog details", "Description", "Placement", "Calculated", "08:00–08:30", "F2 Times"} {
					if !strings.Contains(view, cue) {
						t.Fatalf("missing %q:\n%s", cue, view)
					}
				}
				if mode != manualPlacement {
					for _, cue := range []string{"Allow starts after", "Use lunch time", "Ctrl+O", "Ctrl+L"} {
						if !strings.Contains(view, cue) {
							t.Fatalf("missing placement control %q", cue)
						}
					}
				}
				assertRedesignBounds(t, m, "narrow placement")
				next, _ := m.Update(specialKey(tea.KeyF2))
				m = next.(model)
				if m.overlay != detailsOverlay || !strings.Contains(m.detailContent, "16:00–16:15") || !strings.Contains(m.detailContent, m.form.inputs[descriptionInput].Value()) {
					t.Fatal("full preview lost windows or description")
				}
			})
		}
	}
}

func TestDirtyDraftCancellationAndReloadAcrossEditors(t *testing.T) {
	for _, kind := range []string{"worklog", "preset", "issue"} {
		t.Run(kind, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			m := newModel(t.Context(), deps, ws, nil)
			m.width, m.height = 100, 28
			var value func() string
			switch kind {
			case "worklog":
				row := testRows()[0]
				worklogState(ws).items = []worklogs.LocalWorklog{row}
				m.openEditForm(row)
				m.form.inputs[descriptionInput].SetValue("Retain this draft")
				value = func() string { return m.form.inputs[descriptionInput].Value() }
			case "preset":
				p := presets.Preset{ID: "preset-id", Name: "daily", Revision: 1, Description: "Original"}
				ws.presets.(*fakePresets).items = []presets.Preset{p}
				m.openPresetForm(&p)
				m.presetForm.inputs[4].SetValue("Retain this draft")
				value = func() string { return m.presetForm.inputs[4].Value() }
			case "issue":
				row := worklogs.IssueMetadata{IssueKey: "APP-1", Title: "Original", ManualTitle: "Original"}
				ws.issues.(*fakeIssues).items = []worklogs.IssueMetadata{row}
				m.openIssueForm(&row)
				m.issueForm.inputs[1].SetValue("Retain this draft")
				value = func() string { return m.issueForm.inputs[1].Value() }
			}
			next, _ := m.Update(specialKey(tea.KeyEsc))
			m = next.(model)
			if m.overlay != discardDraftOverlay {
				t.Fatal("dirty draft closed without explicit discard")
			}
			next, _ = m.Update(specialKey(tea.KeyEsc))
			m = next.(model)
			if value() != "Retain this draft" {
				t.Fatal("cancel discard changed the draft")
			}
			switch kind {
			case "worklog":
				m.form.stale = true
				worklogState(ws).items[0].Description = "Latest record"
				worklogState(ws).items[0].Revision = 4
			case "preset":
				m.presetForm.stale = true
				ws.presets.(*fakePresets).items[0].Name = "renamed"
				ws.presets.(*fakePresets).items[0].Description = "Latest record"
				ws.presets.(*fakePresets).items[0].Revision = 4
			case "issue":
				m.issueForm.stale = true
				ws.issues.(*fakeIssues).items[0].ManualTitle = "Latest record"
			}
			next, _ = m.Update(ctrlKey('s'))
			m = next.(model)
			if m.overlay != reloadOverlay {
				t.Fatal("stale draft has no common reload confirmation")
			}
			next, cmd := m.Update(specialKey(tea.KeyEnter))
			m = next.(model)
			if cmd == nil || !m.draftReloading {
				t.Fatal("confirmed reload did not start")
			}
			next, _ = m.Update(textKey("?"))
			m = next.(model)
			next, _ = m.Update(specialKey(tea.KeyEsc))
			m = next.(model)
			if m.actions != nil || !m.draftReloading {
				t.Fatal("reload help did not close independently of the pending read")
			}
			next, _ = m.Update(cmd())
			m = next.(model)
			if m.draftReloading || m.overlay != noOverlay || value() != "Latest record" {
				t.Fatalf("latest record was not reloaded: %s", m.detailContent)
			}
		})
	}
}

func TestBusyEditorsAdvertiseOnlyLiveActions(t *testing.T) {
	for _, kind := range []string{"worklog", "preset", "issue", "assignment", "move"} {
		t.Run(kind, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			m := newModel(t.Context(), deps, ws, nil)
			m.width, m.height = 100, 28
			switch kind {
			case "worklog":
				m.openAddForm()
				m.form.saving = true
			case "preset":
				m.openPresetForm(nil)
				m.presetForm.saving = true
			case "issue":
				m.openIssueForm(nil)
				m.issueForm.saving = true
			case "assignment":
				m.overlay = worklogAssignmentOverlay
				m.worklogAssignment = &worklogAssignmentState{saving: true}
			case "move":
				m.move = &moveState{saving: true}
			}
			footer := ansi.Strip(m.renderFooter(100))
			if !strings.Contains(footer, "Saving") || strings.Contains(footer, "Esc Cancel") || strings.Contains(footer, "Ctrl+S") {
				t.Fatalf("misleading busy footer: %s", footer)
			}
			if actions := m.commandActions(); len(actions) != 1 || actions[0].key != "ctrl+c" {
				t.Fatalf("busy actions: %#v", actions)
			}
		})
	}
}

func TestThemeDateInputsAndPermanentDeleteCopy(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.activeTab = plansTab
	m.openPlanForm()
	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	m = next.(model)
	m.planForm.window = planCustomWindow
	if strings.Contains(m.renderPlanForm(), "\x1b[37m") {
		t.Fatal("date input bypasses theme")
	}
	m.planForm = nil
	m.activeTab = presetsTab
	m.presets = []presets.Preset{{Name: "daily"}}
	if !strings.Contains(ansi.Strip(m.renderFooter(100)), "D Delete preset") {
		t.Fatal("permanent deletion is labelled Trash")
	}
	m.presetDelete = &m.presets[0]
	m.overlay = deletePresetOverlay
	if !strings.Contains(ansi.Strip(m.render()), "cannot be restored") {
		t.Fatal("delete lacks its recovery consequence")
	}
}

func TestValidationFocusAndCompleteErrorDetails(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.openAddForm()
	m.worklogValidation(worklogs.ValidationError{Issues: []worklogs.ValidationIssue{{Field: "duration", Message: "must be a valid Go duration"}, {Field: "description", Message: "Description correction"}}})
	if m.form.focus != durationField || strings.Count(ansi.Strip(m.render()), "Enter a duration such as") != 1 {
		t.Fatal("validation is duplicated or points at the wrong field")
	}
	if !strings.Contains(m.draftDetails(), "Description correction") {
		t.Fatal("full validation details lost a later error")
	}
	m.form = nil
	m.activeTab = issuesTab
	m.openIssueForm(nil)
	m.issueForm.focus = 3
	next, _ := m.beginIssueFormSubmit()
	m = next.(model)
	if m.issueForm.focus != 0 {
		t.Fatal("invalid issue key did not receive focus")
	}
	m.issueForm = nil
	m.issues = []worklogs.IssueMetadata{{IssueKey: "APP-1"}}
	m.issueSelected = 0
	m.syncIssueHistory()
	message := strings.Repeat("Detailed local history failure. ", 30) + "ErrorEnd"
	next, _ = m.Update(issueHistoryResultMsg{generation: m.issueHistory.generation, issueKey: "APP-1", err: errors.New(message)})
	m = next.(model)
	m.focus = focusIssueHistory
	next, _ = m.Update(specialKey(tea.KeyF2))
	m = next.(model)
	if !strings.Contains(m.detailContent, "ErrorEnd") {
		t.Fatal("full history error is inaccessible")
	}
	m.detailViewport.GotoBottom()
	if !strings.Contains(ansi.Strip(m.render()), "ErrorEnd") || !strings.Contains(m.detailPosition(), "of") {
		t.Fatal("detail scrolling lacks full text or position")
	}
}

func TestFailureCountsAndFooterContext(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.activeTab = statusTab
	m.statusLoading = false
	m.statusErr = "storage unavailable"
	if view := ansi.Strip(m.render()); strings.Contains(view, "Errors 0") || !strings.Contains(view, "results unavailable") {
		t.Fatal("failed status looks healthy")
	}
	m.statusErr = ""
	m.activeTab = issuesTab
	m.issuesErr = "unavailable"
	if footer := ansi.Strip(m.renderFooter(120)); strings.Count(footer, "r Retry") != 1 {
		t.Fatalf("duplicate retry: %s", footer)
	}
	m.activeTab = worklogsTab
	m.worklogs = testRows()
	m.worklogSelected = 0
	m.activityOpen = true
	m.focus = focusActivity
	if footer := ansi.Strip(m.renderFooter(120)); strings.Contains(footer, "1 of 1") {
		t.Fatal("activity footer reports a background worklog counter")
	}
	m.activeTab = statusTab
	m.statusItems = []status.Item{{Status: "error"}}
	m.focus = focusWorkspace
	if strings.Contains(m.renderRail(24), "1 errors") {
		t.Fatal("singular error label is wrong")
	}
	if lipgloss.Width(m.render()) != 100 {
		t.Fatal("footer change overflowed terminal")
	}
}

func TestActivityPreservesSelectedTimeAndDescriptionAtMinimum(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.worklogs = testRows()
	m.worklogLoading = false
	m.activityOpen = true
	m.worklogs[0].Description = "Implement the activity feed"
	m.issueTotalIssue = m.worklogs[0].IssueKey
	m.issueTotal = 7200
	m.issueTotalLoading = false
	view := ansi.Strip(m.render())
	for _, cue := range []string{"09:00–11:00", "Implement the activity", "feed", "Issue effort · all dates", "2h local"} {
		if !strings.Contains(view, cue) {
			t.Fatalf("activity hid %q:\n%s", cue, view)
		}
	}
	assertRedesignBounds(t, m, "activity detail")
}

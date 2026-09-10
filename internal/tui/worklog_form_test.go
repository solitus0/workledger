package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestAddDefaultsToFillAndManualHasNoStartDefault(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	if m.form.placement != fillPlacement {
		t.Fatalf("default placement = %s, want Fill", m.form.placement)
	}
	if got := m.form.inputs[startInput].Value(); got != "" {
		t.Fatalf("manual start default = %q, want empty", got)
	}
	if rendered := m.render(); strings.Contains(rendered, "Start        ") || !strings.Contains(rendered, "[Fill]") {
		t.Fatalf("fill form should hide Start and select Fill:\n%s", rendered)
	}
	next, _ := m.Update(ctrlKey('p'))
	m = next.(model)
	if m.form.placement != manualPlacement || !strings.Contains(m.renderFooter(m.width), "Ctrl+P Fit") {
		t.Fatalf("Ctrl+P did not cycle Fill to Manual: placement=%s footer=%s", m.form.placement, m.renderFooter(m.width))
	}
	next, _ = m.Update(ctrlKey('p'))
	m = next.(model)
	if m.form.placement != fitPlacement || !strings.Contains(m.renderFooter(m.width), "Ctrl+P Fill") {
		t.Fatalf("Ctrl+P did not cycle Manual to Fit: placement=%s footer=%s", m.form.placement, m.renderFooter(m.width))
	}
	next, _ = m.Update(ctrlKey('p'))
	m = next.(model)
	if m.form.placement != fillPlacement || !strings.Contains(m.renderFooter(m.width), "Ctrl+P Manual") {
		t.Fatalf("Ctrl+P did not cycle Fit to Fill: placement=%s footer=%s", m.form.placement, m.renderFooter(m.width))
	}

	m.focusForm(placementField)
	next, _ = m.Update(specialKey(tea.KeyLeft))
	m = next.(model)
	if m.form.placement != fitPlacement {
		t.Fatalf("placement = %s, want Fit", m.form.placement)
	}
	next, _ = m.Update(specialKey(tea.KeyLeft))
	m = next.(model)
	if m.form.placement != manualPlacement || !strings.Contains(m.render(), "Start") {
		t.Fatalf("manual placement did not expose Start")
	}
	if rendered := m.render(); !strings.Contains(rendered, "Start time") || !strings.Contains(rendered, "HH:MM") {
		t.Fatalf("manual add should request only local time:\n%s", rendered)
	}
	if got := m.form.inputs[startInput].CharLimit; got != 5 {
		t.Fatalf("manual add start limit = %d, want 5", got)
	}
}

func TestManualStartOffersLatestWorklogEndThroughExplicitTabAcceptance(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = []worklogs.LocalWorklog{
		{StartedAtUTC: time.Date(2026, 5, 21, 13, 50, 0, 0, time.UTC), DurationSeconds: 3600},
		{StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 5400},
		{StartedAtUTC: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC), DurationSeconds: 3000},
		{StartedAtUTC: time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC), DurationSeconds: 9000},
	}
	m.openAddForm()
	if got := m.form.suggestedStart; got != "14:50" {
		t.Fatalf("manual start suggestion = %q, want 14:50", got)
	}
	m.form.placement = manualPlacement
	m.form.inputs[issueInput].SetValue("APPS-993")
	m.form.inputs[durationInput].SetValue("1h")
	m.focusForm(startField)
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Tab Use 14:50") {
		t.Fatalf("manual start suggestion missing from footer: %q", footer)
	}

	next, cmd := m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if got := m.form.inputs[startInput].Value(); got != "14:50" || m.form.focus != startField || cmd == nil {
		t.Fatalf("first Tab value=%q focus=%v command=%v", got, m.form.focus, cmd)
	}
	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if m.form.focus != durationField {
		t.Fatalf("second Tab focus=%v, want duration", m.form.focus)
	}
}

func TestManualStartSuggestionDoesNotOverwriteTypedInput(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 13, 50, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	m.openAddForm()
	m.form.placement = manualPlacement
	m.form.inputs[startInput].SetValue("12:00")
	m.focusForm(startField)

	next, _ := m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if got := m.form.inputs[startInput].Value(); got != "12:00" || m.form.focus != durationField {
		t.Fatalf("typed start was changed or focus did not advance: value=%q focus=%v", got, m.form.focus)
	}
}

func TestManualStartSuggestionHandlesMinuteAndDayBoundaries(t *testing.T) {
	selected := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	if got := suggestedManualStart([]worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 13, 50, 30, 0, time.UTC), DurationSeconds: 3600,
	}}, selected, time.UTC); got != "14:51" {
		t.Fatalf("sub-minute end suggestion = %q, want 14:51", got)
	}
	if got := suggestedManualStart([]worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 23, 30, 0, 0, time.UTC), DurationSeconds: 3600,
	}}, selected, time.UTC); got != "" {
		t.Fatalf("cross-day suggestion = %q, want empty", got)
	}
	if got := suggestedManualStart(nil, selected, time.UTC); got != "" {
		t.Fatalf("empty-day suggestion = %q, want empty", got)
	}
	vilnius, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	fallbackDay := time.Date(2026, 10, 25, 0, 0, 0, 0, vilnius)
	if got := suggestedManualStart([]worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), DurationSeconds: 1800,
	}}, fallbackDay, vilnius); got != "" {
		t.Fatalf("ambiguous local-time suggestion = %q, want empty", got)
	}
}

func TestLoadedWorklogsRefreshUnacceptedManualStartSuggestion(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.placement = manualPlacement
	m.worklogGen = 4

	next, _ := m.Update(worklogResultMsg{
		generation: 4,
		date:       m.selectedDate,
		items: []worklogs.LocalWorklog{{
			StartedAtUTC: time.Date(2026, 5, 21, 13, 50, 0, 0, time.UTC), DurationSeconds: 3600,
		}},
	})
	m = next.(model)
	if got := m.form.suggestedStart; got != "14:50" || m.form.inputs[startInput].Value() != "" {
		t.Fatalf("refreshed suggestion=%q input=%q", got, m.form.inputs[startInput].Value())
	}
}

func TestAddFormSupportsUpAndDownFieldNavigation(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	if m.form.focus != issueField {
		t.Fatalf("initial focus = %v, want issue", m.form.focus)
	}

	next, _ := m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	if m.form.focus != durationField {
		t.Fatalf("down focus = %v, want duration", m.form.focus)
	}
	next, _ = m.Update(specialKey(tea.KeyUp))
	m = next.(model)
	if m.form.focus != issueField {
		t.Fatalf("up focus = %v, want issue", m.form.focus)
	}
	next, _ = m.Update(specialKey(tea.KeyUp))
	m = next.(model)
	if m.form.focus != descriptionField {
		t.Fatalf("up focus = %v, want description", m.form.focus)
	}
	next, _ = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	if m.form.focus != issueField {
		t.Fatalf("down from description focus = %v, want issue", m.form.focus)
	}
}

func TestAddFormCompletesKnownIssuesInServiceOrder(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.knownIssueKeys = []string{"APP-104", "APP-42", "META-1"}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	cmd := m.openAddForm()
	if cmd == nil {
		t.Fatal("known-issue completion was not scheduled")
	}

	next, _ := m.Update(textKey("A"))
	m = next.(model)
	next, _ = m.Update(textKey("P"))
	m = next.(model)
	next, _ = m.Update(textKey("P"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if service.knownIssueCalls != 1 {
		t.Fatalf("known-issue calls = %d, want 1", service.knownIssueCalls)
	}
	if got := m.form.inputs[issueInput].CurrentSuggestion(); got != "APP-104" {
		t.Fatalf("current suggestion = %q, want most recent APP-104", got)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Tab Complete") {
		t.Fatalf("completion affordance missing from footer: %q", footer)
	}

	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if got := m.form.inputs[issueInput].Value(); got != "APP-104" {
		t.Fatalf("completed issue = %q, want APP-104", got)
	}
	if m.form.focus != issueField {
		t.Fatalf("Tab completion moved focus to %v", m.form.focus)
	}

	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if m.form.focus != durationField {
		t.Fatalf("Tab after completed issue focused %v, want duration", m.form.focus)
	}
}

func TestAddFormOffersSelectedThenSessionIssueWithoutPrefilling(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.lastAddedIssue = "LAST-2"
	m.worklogs = []worklogs.LocalWorklog{{ID: "selected", IssueKey: "SELECTED-1"}}
	m.openAddForm()
	if got := m.form.inputs[issueInput].Value(); got != "" {
		t.Fatalf("issue was silently prefilled with %q", got)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Tab Use SELECTED-1") {
		t.Fatalf("selected issue suggestion missing: %q", footer)
	}
	next, _ := m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if got := m.form.inputs[issueInput].Value(); got != "SELECTED-1" || m.form.focus != issueField {
		t.Fatalf("selected issue was not explicitly accepted: value=%q focus=%v", got, m.form.focus)
	}

	m.worklogs = nil
	m.openAddForm()
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Tab Use LAST-2") {
		t.Fatalf("session issue suggestion missing: %q", footer)
	}
}

func TestAddFormReusesIssueDescriptionAndEnterSubmits(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.descriptions = map[string][]string{
		"APP-1": {"Implement recent completion", "Review older changes"},
	}
	service.addRecords = []worklogs.LocalWorklog{{
		ID: "created", IssueKey: "APP-1", StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
		DurationSeconds: 5400, Description: "Implement recent completion",
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("APP-1")

	next, descriptionsCmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.form.focus != durationField || descriptionsCmd == nil {
		t.Fatalf("Enter from issue focused %v with command %v", m.form.focus, descriptionsCmd)
	}
	next, _ = m.Update(descriptionsCmd())
	m = next.(model)
	if fmt.Sprint(service.descriptionKeys) != "[APP-1]" {
		t.Fatalf("description lookup keys = %v", service.descriptionKeys)
	}

	m.form.inputs[durationInput].SetValue("1:30")
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.form.focus != descriptionField || m.form.inputs[durationInput].Value() != "1h30m" {
		t.Fatalf("duration progression focus=%v duration=%q", m.form.focus, m.form.inputs[durationInput].Value())
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Tab Reuse description") {
		t.Fatalf("description suggestion missing: %q", footer)
	}
	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if got := m.form.inputs[descriptionInput].Value(); got != "Implement recent completion" || m.form.focus != descriptionField {
		t.Fatalf("description completion value=%q focus=%v", got, m.form.focus)
	}

	m.form.previewRecords = append([]worklogs.LocalWorklog(nil), service.addRecords...)
	next, submitCmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if submitCmd == nil {
		t.Fatal("Enter from description did not submit")
	}
	result := submitCmd().(mutationResultMsg)
	if result.err != nil || len(service.addInputs) != 1 {
		t.Fatalf("submit result=%#v inputs=%#v", result, service.addInputs)
	}
	if input := service.addInputs[0]; input.Duration != "1h30m" || input.Description != "Implement recent completion" {
		t.Fatalf("normalized submit input = %#v", input)
	}
}

func TestTUIDurationNormalization(t *testing.T) {
	tests := map[string]string{
		"1h 30m": "1h30m",
		"1:30":   "1h30m",
		"0:45":   "45m",
		"2:00":   "2h",
		"90":     "90",
		"1:75":   "1:75",
	}
	for input, want := range tests {
		if got := normalizeTUIDuration(input); got != want {
			t.Errorf("normalizeTUIDuration(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAddFormIgnoresFailedAndObsoleteIssueCompletions(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	currentGeneration := m.issueSuggestionGen

	next, _ := m.Update(issueSuggestionsResultMsg{generation: currentGeneration - 1, items: []string{"OLD-1"}})
	m = next.(model)
	if got := m.form.inputs[issueInput].AvailableSuggestions(); len(got) != 0 {
		t.Fatalf("obsolete suggestions applied: %v", got)
	}
	next, _ = m.Update(issueSuggestionsResultMsg{generation: currentGeneration, err: errors.New("completion unavailable")})
	m = next.(model)
	if m.form == nil || m.formError != "" {
		t.Fatalf("advisory completion failure affected form: form=%#v error=%q", m.form, m.formError)
	}
}

func TestAutomaticPreviewUsesSelectedDayAndProtectsGeneration(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	estimate := int64(14400)
	service.metadata = map[string]worklogs.IssueMetadata{
		"WL-1234": {IssueKey: "WL-1234", SourceAdapterFamily: "jira_cloud", MaxEstimateSeconds: &estimate},
	}
	service.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC), DurationSeconds: 5400,
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[durationInput].SetValue("1h30m")
	cmd := m.schedulePreview()
	m = resolvePreview(t, m, cmd)
	if len(service.previewInputs) != 1 {
		t.Fatalf("preview calls = %d, want 1", len(service.previewInputs))
	}
	input := service.previewInputs[0]
	if input.Fit || !input.Fill || input.From != "2026-05-21" || input.To != "2026-05-21" {
		t.Fatalf("preview input = %#v", input)
	}
	if rendered := m.render(); !strings.Contains(rendered, "09:30–11:00") {
		t.Fatalf("preview render missing placement:\n%s", rendered)
	}
	if metadata, ok := m.issueMetadata["WL-1234"]; !ok || metadata.MaxEstimateSeconds == nil || *metadata.MaxEstimateSeconds != estimate {
		t.Fatalf("preview did not retain cached issue metadata: %#v", m.issueMetadata)
	}

	current := m.form.previewGeneration
	next, _ := m.Update(previewResultMsg{
		generation: current - 1,
		records:    []worklogs.LocalWorklog{{StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 3600}},
	})
	m = next.(model)
	if !m.form.previewRecords[0].StartedAtUTC.Equal(service.previewRecords[0].StartedAtUTC) {
		t.Fatalf("stale preview overwrote current placement")
	}
}

func TestManualConflictPreviewRemainsVisibleAndMarked(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.previewErr = worklogs.ValidationError{Conflict: &worklogs.ConflictDetail{
		Reason: "overlap",
		Attempted: worklogs.RecordView{
			IssueKey: "WL-1234", StartedAtUTC: "2026-05-21T12:00:00Z", DurationSeconds: 3600,
		},
		ConflictingIDs: []string{"existing"},
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "existing", IssueKey: "WL-1000", StartedAtUTC: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	m.openAddForm()
	m.form.placement = manualPlacement
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[startInput].SetValue("12:00")
	m.form.inputs[durationInput].SetValue("1h")

	m = resolvePreview(t, m, m.schedulePreview())
	if len(m.form.previewRecords) != 1 || m.form.previewConflict != "overlap" {
		t.Fatalf("manual conflict preview = records %#v conflict %q", m.form.previewRecords, m.form.previewConflict)
	}
	rendered := m.render()
	for _, expected := range []string{"! conflict", "1 worklog · 12:00–13:00 · overlap"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("manual conflict render missing %q:\n%s", expected, rendered)
		}
	}
	if timeline := m.renderDayTimeline(m.form.previewRecords, ""); !strings.Contains(timeline, "!") {
		t.Fatalf("manual conflict is not visible in timeline: %s", timeline)
	}
}

func TestAutomaticSubmitUsesPreviewExpectationAndReportsFillCount(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	created := []worklogs.LocalWorklog{
		{ID: "new-1", StartedAtUTC: time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC), DurationSeconds: 1800},
		{ID: "new-2", StartedAtUTC: time.Date(2026, 5, 21, 10, 30, 0, 0, time.UTC), DurationSeconds: 1800},
	}
	service.addRecords = created
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.placement = fillPlacement
	m.form.overtime = true
	m.form.noLunch = true
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[durationInput].SetValue("1h")
	m.form.inputs[descriptionInput].SetValue("Fill gaps")
	m.form.previewRecords = append([]worklogs.LocalWorklog(nil), created...)

	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	result := cmd().(mutationResultMsg)
	next, _ = m.Update(result)
	m = next.(model)
	if len(service.addInputs) != 1 {
		t.Fatalf("add calls = %d, want 1", len(service.addInputs))
	}
	input := service.addInputs[0]
	if !input.Fill || input.Fit || !input.Overtime || !input.NoLunch || input.Started != "" || len(input.ExpectedPlacement) != 2 {
		t.Fatalf("fill add input = %#v", input)
	}
	if m.notice != "added 2 worklogs" || m.selectAfterID != "new-1" {
		t.Fatalf("post-fill notice=%q selection=%q", m.notice, m.selectAfterID)
	}
}

func TestExternalChangeRecalculatesAutomaticAddWithoutStalingDraft(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[durationInput].SetValue("1h")
	before := m.form.previewGeneration
	next, cmd := m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if m.form.stale || m.form.previewGeneration <= before || cmd == nil {
		t.Fatalf("automatic draft was not recalculated: %#v", m.form)
	}
	worklogGeneration := m.worklogGen
	next, cmd = m.Update(specialKey(tea.KeyEscape))
	m = next.(model)
	if m.form != nil || cmd == nil || m.worklogGen != worklogGeneration+1 {
		t.Fatalf("cancelling externally changed draft did not reload list")
	}
}

func TestChangedAutomaticPlacementRejectsSaveAndRefreshesPreview(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[durationInput].SetValue("1h")
	m.form.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	next, cmd := m.Update(mutationResultMsg{kind: "add", err: worklogs.ErrPlacementChanged})
	m = next.(model)
	if m.form == nil || !m.form.previewLoading || cmd == nil || !strings.Contains(m.formError, "availability changed") {
		t.Fatalf("changed placement did not preserve draft and refresh preview")
	}
}

func TestAddEditDeleteMutationsUseWritabilityAndRevision(t *testing.T) {
	ws, deps := testWorkspace(t)
	checks := 0
	deps.checkWritable = func(string, string) error { checks++; return nil }
	service := worklogState(ws)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32

	m.openAddForm()
	m.form.placement = manualPlacement
	if m.form.inputs[startInput].Value() != "" {
		t.Fatalf("manual start should not have a colliding default")
	}
	m.form.inputs[0].SetValue("WL-1234")
	m.form.inputs[startInput].SetValue("08:00")
	m.form.inputs[2].SetValue("1h")
	result := m.submitFormCmd()().(mutationResultMsg)
	next, _ := m.Update(result)
	m = next.(model)
	if result.err != nil || len(service.addInputs) != 1 || service.addInputs[0].Force || m.form != nil {
		t.Fatalf("unexpected add result=%#v inputs=%#v form=%#v", result, service.addInputs, m.form)
	}
	if got := service.addInputs[0].Started; got != "2026-05-21T08:00" {
		t.Fatalf("manual add composed start = %q, want selected date plus entered time", got)
	}

	row := testRows()[0]
	m.worklogs = []worklogs.LocalWorklog{row}
	m.openEditForm(row)
	m.form.inputs[3].SetValue("Updated description")
	result = m.submitFormCmd()().(mutationResultMsg)
	next, _ = m.Update(result)
	m = next.(model)
	if len(service.updates) != 1 || service.updates[0].id != row.ID || service.updates[0].input.ExpectedRevision != row.Revision {
		t.Fatalf("edit did not retain revision: %#v", service.updates)
	}
	if got := *service.updates[0].input.Started; got != "2026-05-21T09:00" {
		t.Fatalf("edit start = %q, want complete original local timestamp", got)
	}

	m.worklogs = []worklogs.LocalWorklog{row}
	next, _ = m.Update(textKey("D"))
	m = next.(model)
	next, cmd := m.Update(textKey("y"))
	m = next.(model)
	deleteResult := cmd().(mutationResultMsg)
	if deleteResult.err != nil || len(service.deletes) != 1 || service.deletes[0] != (recordedDelete{id: row.ID, revision: row.Revision}) {
		t.Fatalf("delete did not retain revision: result=%#v deletes=%#v", deleteResult, service.deletes)
	}
	if checks != 3 {
		t.Fatalf("writability checks = %d, want 3", checks)
	}
}

func TestValidationPreservesDraftAndForceResubmitsConflict(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.placement = manualPlacement
	m.form.inputs[0].SetValue("WL-1234")
	m.form.inputs[2].SetValue("1h")

	validation := worklogs.ValidationError{Issues: []worklogs.ValidationIssue{{Field: "duration", Message: "duration is invalid"}}}
	next, _ := m.Update(mutationResultMsg{kind: "add", err: validation})
	m = next.(model)
	if m.form == nil || !strings.Contains(m.render(), "duration is invalid") {
		t.Fatalf("validation did not preserve and annotate draft")
	}

	conflict := &worklogs.ConflictDetail{
		Reason:         "overlap",
		Attempted:      worklogs.RecordView{IssueKey: "WL-1234", StartedAt: "2026-05-21 09:30", DurationSeconds: 3600},
		ConflictingIDs: []string{"row-1"},
	}
	m.worklogs = testRows()
	next, _ = m.Update(mutationResultMsg{kind: "add", err: worklogs.ValidationError{Conflict: conflict}})
	m = next.(model)
	if m.overlay != forceOverlay || !strings.Contains(m.render(), "Implement ledger export") {
		t.Fatalf("force confirmation does not show conflicting row")
	}
	next, cmd := m.Update(textKey("y"))
	m = next.(model)
	result := cmd().(mutationResultMsg)
	if result.err != nil || len(service.addInputs) != 1 || !service.addInputs[0].Force {
		t.Fatalf("force resubmission = result %#v inputs %#v", result, service.addInputs)
	}
}

func TestExternalChangeMarksOpenEditorStale(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = testRows()
	m.openEditForm(m.worklogs[0])
	next, _ := m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if !m.form.stale || !strings.Contains(m.notice, "changed externally") {
		t.Fatalf("editor not marked stale: %#v", m.form)
	}

	next, _ = m.Update(ctrlKey('s'))
	m = next.(model)
	if m.overlay != reloadOverlay {
		t.Fatalf("stale save overlay = %v, want reload", m.overlay)
	}
}

func TestExternalChangeRefreshPreservesOrSelectsNearestRow(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "row-1", Revision: 1},
		{ID: "row-2", Revision: 1},
		{ID: "row-3", Revision: 1},
	}
	m.worklogSelected = 1
	next, cmd := m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if cmd == nil || m.worklogGen != 2 {
		t.Fatalf("idle external change did not schedule refresh")
	}
	next, _ = m.Update(worklogResultMsg{
		generation: m.worklogGen,
		date:       m.selectedDate,
		items:      []worklogs.LocalWorklog{{ID: "row-1", Revision: 1}, {ID: "row-3", Revision: 1}},
	})
	m = next.(model)
	if selected := m.selectedWorklog(); selected == nil || selected.ID != "row-3" {
		t.Fatalf("nearest remaining row = %#v, want row-3", selected)
	}
}

func TestConflictRequiresForceConfirmation(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.placement = manualPlacement
	conflict := &worklogs.ConflictDetail{Reason: "overlap", ConflictingIDs: []string{"other"}}
	next, _ := m.Update(mutationResultMsg{kind: "add", err: worklogs.ValidationError{Conflict: conflict}})
	m = next.(model)
	if m.overlay != forceOverlay || m.conflict != conflict {
		t.Fatalf("conflict state overlay=%v detail=%#v", m.overlay, m.conflict)
	}
}

func TestAutomaticAddFormExplainsDayAndProjectedOutcome(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "existing", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 7200,
	}}
	m.dayContext = worklogs.ContextDay{Date: "2026-05-21", BookedSeconds: 7200}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	estimate := int64(14400)
	m.issueMetadata = map[string]worklogs.IssueMetadata{
		"WL-200": {IssueKey: "WL-200", SourceAdapterFamily: "jira_cloud", MaxEstimateSeconds: &estimate},
	}
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("WL-200")
	m.form.inputs[durationInput].SetValue("3h")
	m.form.previewRecords = []worklogs.LocalWorklog{
		{IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC), DurationSeconds: 7200},
		{IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 12, 45, 0, 0, time.UTC), DurationSeconds: 3600},
	}

	rendered := m.render()
	for _, expected := range []string{
		"Add worklog · Thu, May 21 · 2h 00m logged · 6h 00m remaining",
		"jira cloud · estimate 4h 00m",
		"█ booked  ▓ proposed  · lunch  ─ free",
		"2 worklogs · 10:00–12:00 + 12:45–13:45",
		"5h 00m total · 3h 00m below daily target",
		"Ctrl+S create 2 worklogs",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("day-aware add form missing %q:\n%s", expected, rendered)
		}
	}
	formLines := strings.Split(m.renderForm(), "\n")
	timelineLine, legendLine := -1, -1
	for index, line := range formLines {
		if strings.Contains(line, "08:00") {
			timelineLine = index
		}
		if strings.Contains(line, "█ booked") {
			legendLine = index
			break
		}
	}
	if timelineLine < 0 || legendLine != timelineLine+2 {
		t.Fatalf("calculated timeline and legend are not separated by one blank row: timeline=%d legend=%d\n%s", timelineLine, legendLine, strings.Join(formLines, "\n"))
	}
}

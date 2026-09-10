package tui

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

type fakeStatus struct {
	report status.Report
	err    error
}

func (f fakeStatus) Check(context.Context) (status.Report, error) { return f.report, f.err }

type fakeTracker struct {
	changed bool
	err     error
}

type fakePlans struct {
	items           []reconcile.ListEntry
	plan            reconcile.Plan
	reconcileResult reconcile.ReconcileResult
	applyResult     reconcile.ApplyResult
	retryResult     reconcile.ApplyResult
	err             error
	rangeCalls      []planRangeCall
}

type planRangeCall struct {
	limit int
	from  time.Time
	to    time.Time
}

func (f *fakePlans) ListRecentPlansInCreatedRange(limit int, from, to time.Time) ([]reconcile.ListEntry, error) {
	f.rangeCalls = append(f.rangeCalls, planRangeCall{limit: limit, from: from, to: to})
	items := make([]reconcile.ListEntry, 0, len(f.items))
	for _, item := range f.items {
		if !item.CreatedAt.Before(from) && item.CreatedAt.Before(to) {
			items = append(items, item)
		}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, f.err
}
func (f *fakePlans) LoadPlan(string) (reconcile.Plan, error) { return f.plan, f.err }
func (f *fakePlans) Reconcile(context.Context, config.EffectiveConfig, reconcile.ReconcileRequest, ...reconcile.PlanOptions) (reconcile.ReconcileResult, error) {
	return f.reconcileResult, f.err
}
func (f *fakePlans) ApplyPlan(context.Context, config.EffectiveConfig, string, ...reconcile.ApplyOptions) (reconcile.ApplyResult, error) {
	return f.applyResult, f.err
}
func (f *fakePlans) RetryPlan(context.Context, config.EffectiveConfig, string, string, ...reconcile.ApplyOptions) (reconcile.ApplyResult, error) {
	return f.retryResult, f.err
}

type fakeActivities struct {
	items    []activity.Entry
	started  []activity.StartInput
	finished []activity.FinishInput
}

func (f *fakeActivities) Start(_ context.Context, input activity.StartInput) (activity.Entry, error) {
	f.started = append(f.started, input)
	entry := activity.Entry{ID: input.ID, Source: input.Source, Operation: input.Operation, Summary: input.Summary, Attributes: input.Attributes, State: activity.StateRunning, StartedAt: input.StartedAt}
	f.items = append([]activity.Entry{entry}, f.items...)
	return entry, nil
}
func (f *fakeActivities) Finish(_ context.Context, id string, input activity.FinishInput) (activity.Entry, error) {
	f.finished = append(f.finished, input)
	for index := range f.items {
		if f.items[index].ID == id {
			f.items[index].State = input.State
			f.items[index].FinishedAt = &input.FinishedAt
			f.items[index].ErrorCode = input.ErrorCode
			f.items[index].ErrorMessage = input.ErrorMessage
			return f.items[index], nil
		}
	}
	return activity.Entry{}, errors.New("not found")
}
func (f *fakeActivities) List(context.Context, activity.ListFilters) ([]activity.Entry, error) {
	return append([]activity.Entry(nil), f.items...), nil
}

type fakePresets struct {
	items      []presets.Preset
	created    []presets.CreateInput
	updated    []presets.PatchInput
	deleted    []string
	markedUsed []string
	err        error
}

func (f *fakePresets) List(context.Context, string, int) ([]presets.Preset, error) {
	return append([]presets.Preset(nil), f.items...), f.err
}
func (f *fakePresets) Create(_ context.Context, _ config.EffectiveConfig, input presets.CreateInput) (presets.Preset, error) {
	f.created = append(f.created, input)
	return presets.Preset{ID: "new", Name: input.Name, IssueKey: input.IssueKey, StartTime: input.StartTime, Description: input.Description, Revision: 1}, f.err
}
func (f *fakePresets) Update(_ context.Context, _ config.EffectiveConfig, _ string, input presets.PatchInput) (presets.Preset, error) {
	f.updated = append(f.updated, input)
	return presets.Preset{Name: *input.Name, Revision: input.ExpectedRevision + 1}, f.err
}
func (f *fakePresets) Delete(_ context.Context, name string, _ int64) (presets.DeleteResult, error) {
	f.deleted = append(f.deleted, name)
	return presets.DeleteResult{Name: name}, f.err
}
func (f *fakePresets) MarkUsed(_ context.Context, id string) error {
	f.markedUsed = append(f.markedUsed, id)
	return f.err
}
func (f *fakePresets) CheckRevision(context.Context, string, int64) error { return f.err }

func (f *fakeTracker) Poll(context.Context) (bool, error) {
	changed := f.changed
	f.changed = false
	return changed, f.err
}
func (*fakeTracker) Close() error { return nil }

type fakeWorklogState struct {
	items           []worklogs.LocalWorklog
	context         worklogs.ContextResult
	contextInputs   []worklogs.ContextInput
	addErr          error
	previewErr      error
	updateErr       error
	deleteErr       error
	addInputs       []worklogs.AddInput
	previewInputs   []worklogs.AddInput
	previewRecords  []worklogs.LocalWorklog
	metadata        map[string]worklogs.IssueMetadata
	issueTotals     map[string]int
	issueTotalErr   error
	issueTotalKeys  []string
	knownIssueKeys  []string
	knownIssueErr   error
	knownIssueCalls int
	descriptions    map[string][]string
	descriptionErr  error
	descriptionKeys []string
	addRecords      []worklogs.LocalWorklog
	updates         []recordedUpdate
	deletes         []recordedDelete
	batchDeletes    []recordedBatchDelete
	batchDeleteErr  error
	trashItems      []worklogs.TrashRecord
	restoreErr      error
}

type fakeWorklogQueries struct{ state *fakeWorklogState }

func (f *fakeWorklogQueries) ActiveIssueTotalSeconds(_ context.Context, issueKey string) (int, error) {
	f.state.issueTotalKeys = append(f.state.issueTotalKeys, issueKey)
	return f.state.issueTotals[issueKey], f.state.issueTotalErr
}

func (f *fakeWorklogQueries) ListKnownIssueKeys(_ context.Context, _ string, _ int) ([]string, error) {
	f.state.knownIssueCalls++
	return append([]string(nil), f.state.knownIssueKeys...), f.state.knownIssueErr
}

func (f *fakeWorklogQueries) ListRecentDescriptions(_ context.Context, issueKey string, _ int) ([]string, error) {
	f.state.descriptionKeys = append(f.state.descriptionKeys, issueKey)
	return append([]string(nil), f.state.descriptions[issueKey]...), f.state.descriptionErr
}

func (f *fakeWorklogQueries) Context(_ context.Context, _ config.EffectiveConfig, input worklogs.ContextInput) (worklogs.ContextResult, error) {
	f.state.contextInputs = append(f.state.contextInputs, input)
	return f.state.context, nil
}

func (f *fakeWorklogQueries) LookupIssueMetadata(_ context.Context, issueKeys []string) (map[string]worklogs.IssueMetadata, error) {
	items := make(map[string]worklogs.IssueMetadata)
	for _, key := range issueKeys {
		if item, ok := f.state.metadata[key]; ok {
			items[key] = item
		}
	}
	return items, nil
}

func (f *fakeWorklogQueries) PreviewAdd(_ context.Context, _ config.EffectiveConfig, input worklogs.AddInput) (worklogs.AddResult, error) {
	f.state.previewInputs = append(f.state.previewInputs, input)
	return worklogs.AddResult{DryRun: true, Records: append([]worklogs.LocalWorklog(nil), f.state.previewRecords...)}, f.state.previewErr
}

type recordedUpdate struct {
	id    string
	input worklogs.PatchInput
}

type recordedDelete struct {
	id       string
	revision int64
}

type recordedBatchDelete struct {
	filters  worklogs.ListFilters
	expected []worklogs.DeleteExpectation
}

type fakeWorklogMutations struct{ state *fakeWorklogState }

func (f *fakeWorklogMutations) Add(_ context.Context, _ config.EffectiveConfig, input worklogs.AddInput) (worklogs.AddResult, error) {
	f.state.addInputs = append(f.state.addInputs, input)
	return worklogs.AddResult{Records: append([]worklogs.LocalWorklog(nil), f.state.addRecords...)}, f.state.addErr
}

func (f *fakeWorklogMutations) Update(_ context.Context, _ config.EffectiveConfig, id string, input worklogs.PatchInput) (worklogs.LocalWorklog, error) {
	f.state.updates = append(f.state.updates, recordedUpdate{id: id, input: input})
	return worklogs.LocalWorklog{}, f.state.updateErr
}

func (f *fakeWorklogMutations) Delete(_ context.Context, id string, revision int64) (worklogs.DeleteResult, error) {
	f.state.deletes = append(f.state.deletes, recordedDelete{id: id, revision: revision})
	return worklogs.DeleteResult{}, f.state.deleteErr
}

func (f *fakeWorklogMutations) DeleteBatchExpected(_ context.Context, _ config.EffectiveConfig, filters worklogs.ListFilters, expected []worklogs.DeleteExpectation) (worklogs.DeleteBatchResult, error) {
	f.state.batchDeletes = append(f.state.batchDeletes, recordedBatchDelete{filters: filters, expected: append([]worklogs.DeleteExpectation(nil), expected...)})
	if f.state.batchDeleteErr != nil {
		return worklogs.DeleteBatchResult{}, f.state.batchDeleteErr
	}
	deleted := make([]worklogs.DeleteMapping, 0, len(expected))
	for _, item := range expected {
		deleted = append(deleted, worklogs.DeleteMapping{ID: item.ID})
	}
	return worklogs.DeleteBatchResult{Deleted: deleted}, nil
}

type fakeTrashService struct{ state *fakeWorklogState }

func (f *fakeTrashService) ListTrash(_ config.EffectiveConfig, _ worklogs.TrashFilters) ([]worklogs.TrashRecord, worklogs.EffectiveFilters, error) {
	return append([]worklogs.TrashRecord(nil), f.state.trashItems...), worklogs.EffectiveFilters{}, nil
}

func (f *fakeTrashService) RestoreTrash(_ context.Context, _ config.EffectiveConfig, id string) (worklogs.TrashRestoreItem, error) {
	return worklogs.TrashRestoreItem{TrashID: id}, f.state.restoreErr
}

func (f *fakeTrashService) RestoreTrashBatchExpected(_ context.Context, _ config.EffectiveConfig, _ worklogs.TrashFilters, ids []string) (worklogs.TrashRestoreResult, error) {
	items := make([]worklogs.TrashRestoreItem, len(ids))
	for i, id := range ids {
		items[i].TrashID = id
	}
	return worklogs.TrashRestoreResult{Items: items}, f.state.restoreErr
}

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string { return ansiSequence.ReplaceAllString(value, "") }

func testWorkspace(t *testing.T) (*workspace, dependencies) {
	t.Helper()
	now := time.Date(2026, 5, 21, 13, 42, 0, 0, time.UTC)
	state := &fakeWorklogState{}
	state.context.Summary = worklogs.ContextSummary{BookedSeconds: 84600, WorklogCount: 9}
	ws := &workspace{
		cfg: config.EffectiveConfig{
			Location: time.UTC, DayStart: "08:00", SQLitePath: "/tmp/workledger.db",
			File: config.FileConfig{Clockify: &config.ClockifyConfig{WorkspaceID: "workspace", UserID: "user", Auth: config.ClockifyAuthConfig{APIKey: "secret"}}},
		},
		worklogs:        &fakeWorklogQueries{state: state},
		mutations:       &fakeWorklogMutations{state: state},
		trash:           &fakeTrashService{state: state},
		presets:         &fakePresets{},
		plans:           &fakePlans{},
		tracker:         &fakeTracker{},
		activityTracker: &fakeTracker{},
		sqlitePath:      "/tmp/workledger.db",
		close:           func() error { return nil },
	}
	deps := dependencies{
		status: fakeStatus{report: status.Report{Items: []status.Item{{Category: "local", Target: "config", Status: "ok"}}}},
		openWorkspace: func(context.Context) (*workspace, error) {
			return ws, nil
		},
		checkWritable: func(string, string) error { return nil },
		now:           func() time.Time { return now },
		noColor:       true,
		theme:         DefaultTheme(),
	}
	return ws, deps
}

func worklogState(ws *workspace) *fakeWorklogState {
	return ws.worklogs.(*fakeWorklogQueries).state
}

func testRows() []worklogs.LocalWorklog {
	return []worklogs.LocalWorklog{{
		ID: "row-1", IssueKey: "WL-1234", StartedAtUTC: time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC),
		DurationSeconds: 7200, Description: "Implement ledger export", Revision: 3,
	}}
}

func testWeekDays() []worklogs.ContextDay {
	days := make([]worklogs.ContextDay, 0, 7)
	for offset := 0; offset < 7; offset++ {
		date := time.Date(2026, 5, 18+offset, 0, 0, 0, 0, time.UTC)
		day := worklogs.ContextDay{Date: date.Format("2006-01-02")}
		switch offset {
		case 3:
			day.BookedSeconds = 2 * 3600
			day.Worklogs = []worklogs.LocalWorklog{{
				ID: "thu", IssueKey: "WL-1234", StartedAtUTC: date.Add(9 * time.Hour), DurationSeconds: 2 * 3600, Description: "Implement week view",
			}}
		case 4:
			day.BookedSeconds = 8 * 3600
			day.Worklogs = []worklogs.LocalWorklog{{
				ID: "fri", IssueKey: "WL-1256", StartedAtUTC: date.Add(8 * time.Hour), DurationSeconds: 8 * 3600, Description: "Verify weekly view",
			}}
			day.Collisions = []worklogs.ContextCollision{{Start: date.Add(9 * time.Hour), End: date.Add(10 * time.Hour)}}
		default:
			if offset < 5 {
				day.BookedSeconds = 7*3600 + 30*60
			}
		}
		days = append(days, day)
	}
	return days
}

func textKey(value string) tea.KeyPressMsg {
	runes := []rune(value)
	return tea.KeyPressMsg(tea.Key{Text: value, Code: runes[0]})
}

func specialKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

func ctrlKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: tea.ModCtrl})
}

func mouseClick(x, y int, button tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: button}
}

func resolvePreview(t *testing.T, m model, scheduled tea.Cmd) model {
	t.Helper()
	if scheduled == nil {
		t.Fatal("expected scheduled preview")
	}
	message := scheduled()
	ready, ok := message.(previewReadyMsg)
	if !ok {
		t.Fatalf("scheduled preview message = %T", message)
	}
	next, cmd := m.Update(ready)
	m = next.(model)
	if cmd == nil {
		t.Fatal("preview command was not started")
	}
	message = cmd()
	result, ok := message.(previewResultMsg)
	if !ok {
		t.Fatalf("preview result message = %T", message)
	}
	next, _ = m.Update(result)
	return next.(model)
}

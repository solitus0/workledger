package reconcile

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/reconcile/model"
)

type snapshotClockifyClient struct {
	fakeClockifyClient
	writes     atomic.Int64
	beforeTags func()
}

func (f *snapshotClockifyClient) ListTags(ctx context.Context, ws string) (map[string]clockify.Tag, error) {
	if f.beforeTags != nil {
		hook := f.beforeTags
		f.beforeTags = nil
		hook()
	}
	return f.fakeClockifyClient.ListTags(ctx, ws)
}
func (f *snapshotClockifyClient) CreateTag(ctx context.Context, ws, name string) (clockify.Tag, error) {
	f.writes.Add(1)
	return f.fakeClockifyClient.CreateTag(ctx, ws, name)
}
func (f *snapshotClockifyClient) CreateTimeEntry(ctx context.Context, ws string, row model.Row, project string, tags []string) (clockify.TimeEntry, error) {
	f.writes.Add(1)
	return f.fakeClockifyClient.CreateTimeEntry(ctx, ws, row, project, tags)
}
func (f *snapshotClockifyClient) DeleteTimeEntry(ctx context.Context, ws, id string) error {
	f.writes.Add(1)
	return f.fakeClockifyClient.DeleteTimeEntry(ctx, ws, id)
}

func TestStalePushCannotMutateIncludingTagPreparation(t *testing.T) {
	for _, stage := range []string{"before apply", "during preparation", "historical plan", "failed retry", "uncertain retry"} {
		t.Run(stage, func(t *testing.T) {
			store := newTestStore(t)
			defer store.Close()
			seedWorklogRow(t, store, "selected", "AAPP-1", "2026-05-01T09:00:00Z", 3600, "Investigate retry")
			seedWorklogRow(t, store, "unassigned", "", "2026-05-01T12:00:00Z", 1800, "Waiting for ticket")
			service := NewService(store)
			client := &snapshotClockifyClient{fakeClockifyClient: fakeClockifyClient{projects: []clockify.Project{{ID: "app", Name: "App"}}, tagsByID: map[string]clockify.Tag{}}}
			service.newClockifyClient = func(config.ClockifyConfig) clockifyClient { return client }
			cfg := testClockifyConfig(true)
			plan, err := service.CreateClockifyPushPlan(t.Context(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 1 || plan.Items[0].IssueKey == "" || plan.SkippedUnassignedCount != 1 || plan.SkippedUnassignedSeconds != 1800 {
				t.Fatalf("plan %#v", plan)
			}
			clear := func() {
				if _, err := store.DB().Exec(`UPDATE worklogs SET issue_key='',revision=revision+1 WHERE id='selected'`); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "during preparation" {
				client.beforeTags = clear
			} else if stage == "historical plan" {
				inspection := plan.Items[0].InspectionSummary
				inspection.SourceSnapshot = nil
				data, _ := json.Marshal(inspection)
				if _, err := store.DB().Exec(`UPDATE saved_plan_items SET inspection_summary_json=? WHERE id=?`, string(data), plan.Items[0].ID); err != nil {
					t.Fatal(err)
				}
			} else {
				clear()
			}
			var result ApplyResult
			if strings.Contains(stage, "retry") {
				state := "failed"
				if stage == "uncertain retry" {
					state = "uncertain"
				}
				if err := service.recordDeliveryAttempt(plan.ID, plan.Items[0].ID, state, "simulated outcome"); err != nil {
					t.Fatal(err)
				}
				result, err = service.RetryPlan(t.Context(), cfg, plan.ID, state)
			} else {
				result, err = service.ApplyPlan(t.Context(), cfg, plan.ID)
			}
			if err != nil || result.FailedCount != 1 || client.writes.Load() != 0 {
				t.Fatalf("result=%#v err=%v writes=%d", result, err, client.writes.Load())
			}
		})
	}
}

func TestUncertainStalePushCanVerifySuccessReadOnly(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "selected", "AAPP-1", "2026-05-01T09:00:00Z", 3600, "Investigate retry")
	service := NewService(store)
	client := &snapshotClockifyClient{fakeClockifyClient: fakeClockifyClient{projects: []clockify.Project{{ID: "app", Name: "App"}}, tagsByID: map[string]clockify.Tag{"tag": {ID: "tag", Name: "AAPP-1"}}}}
	service.newClockifyClient = func(config.ClockifyConfig) clockifyClient { return client }
	cfg := testClockifyConfig(true)
	plan, err := service.CreateClockifyPushPlan(t.Context(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	client.entries = []clockify.TimeEntry{{ID: "remote", ProjectID: "app", Description: "Investigate retry", TagIDs: []string{"tag"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T09:00:00Z", End: "2026-05-01T10:00:00Z"}}}
	store.DB().Exec(`UPDATE worklogs SET issue_key='',revision=revision+1 WHERE id='selected'`)
	if err := service.recordDeliveryAttempt(plan.ID, plan.Items[0].ID, "uncertain", "outcome unknown"); err != nil {
		t.Fatal(err)
	}
	result, err := service.RetryPlan(t.Context(), cfg, plan.ID, "uncertain")
	if err != nil || result.AppliedCount != 1 || client.writes.Load() != 0 {
		t.Fatalf("result=%#v err=%v writes=%d", result, err, client.writes.Load())
	}
}

func TestStalePushDoesNotStopIndependentTarget(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "first", "AAPP-1", "2026-05-01T09:00:00Z", 3600, "First")
	seedWorklogRow(t, store, "second", "BAPP-1", "2026-05-01T11:00:00Z", 3600, "Second")
	service := NewService(store)
	client := &snapshotClockifyClient{fakeClockifyClient: fakeClockifyClient{projects: []clockify.Project{{ID: "app", Name: "App"}}, tagsByID: map[string]clockify.Tag{}}}
	service.newClockifyClient = func(config.ClockifyConfig) clockifyClient { return client }
	cfg := testClockifyConfig(true)
	plan, err := service.CreateClockifyPushPlan(t.Context(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE worklogs SET issue_key='',revision=revision+1 WHERE id='first'`); err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyPlan(t.Context(), cfg, plan.ID)
	if err != nil || result.AppliedCount != 1 || result.FailedCount != 1 || client.writes.Load() != 2 {
		t.Fatalf("result=%#v err=%v writes=%d", result, err, client.writes.Load())
	}
	if len(client.entries) != 1 || client.tagsByID["created-BAPP-1"].Name != "BAPP-1" {
		t.Fatalf("unexpected writes %#v", client)
	}
}

func TestJiraReportingSnapshotsDetectNewIssueMembership(t *testing.T) {
	for _, family := range []string{"jira-cloud", "jira-data-center"} {
		t.Run(family, func(t *testing.T) {
			store := newTestStore(t)
			defer store.Close()
			service := NewService(store)
			seedWorklogRow(t, store, "unassigned", "", "2026-05-01T11:00:00Z", 1800, "Waiting")
			from, to := mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z")
			var plan Plan
			var err error
			if family == "jira-cloud" {
				service.newJiraCloudClient = func(config.JiraCloudInstance) jiraCloudClient {
					return &fakeJiraCloudClient{user: jiracloud.User{AccountID: "u1"}, worklogsByIssue: map[string][]jiracloud.Worklog{"REPORT-1": {}}}
				}
				plan, err = service.CreateJiraCloudPushPlan(t.Context(), testJiraCloudConfig(), "reporting", from, to, false)
			} else {
				service.newJiraDataClient = func(config.JiraDataCenterInstance) jiraDataClient {
					return &fakeJiraDataClient{user: jiradatacenter.User{AccountID: "u1"}, worklogsByIssue: map[string][]jiradatacenter.Worklog{"REPORT-1": {}}}
				}
				plan, err = service.CreateJiraDataPushPlan(t.Context(), testJiraDataConfig(), "reporting", from, to, false)
			}
			if err != nil || len(plan.Items) != 1 {
				t.Fatalf("plan=%#v err=%v", plan, err)
			}
			if err := service.validatePushSources(t.Context(), plan.Items); err != nil {
				t.Fatal(err)
			}
			seedWorklogRow(t, store, "new-source", "AAPP-99", "2026-05-01T09:00:00Z", 3600, "New issue")
			if err := service.validatePushSources(t.Context(), plan.Items); err == nil {
				t.Fatal("new source issue did not invalidate initially empty reporting scope")
			}
		})
	}
}

func TestPullStillKeepsOverlappingUnassignedWork(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "unassigned", "", "2026-05-01T09:00:00Z", 3600, "Same activity")
	service := NewService(store)
	cfg := testClockifyConfig(true)
	plan, err := service.buildClockifyPullPlanFromRows(cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), []model.Row{{IssueKey: "AAPP-1", StartedAtUTC: mustTime("2026-05-01T09:00:00Z"), DurationSeconds: 3600, Description: "Same activity"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.normalizePullPlan(t.Context(), &plan); err != nil {
		t.Fatal(err)
	}
	if err := service.insertPlan(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyPlan(t.Context(), cfg, plan.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM worklogs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rows=%d err=%v", count, err)
	}
}

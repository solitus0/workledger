package reconcile

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/reconcile/model"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

func TestCreateClockifyPushPlanClassifiesScopes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Sync docs")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-3", "CAPP-1", "2026-05-01T12:00:00Z", 3600, "Wrong project")
	seedWorklogRow(t, store, "row-4", "DAPP-1", "2026-05-01T14:00:00Z", 3600, "Need tag")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}, {ID: "proj-other", Name: "Other"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
			"tag-b": {ID: "tag-b", Name: "BAPP-1"},
			"tag-c": {ID: "tag-c", Name: "CAPP-1"},
		},
		entries: []clockify.TimeEntry{
			{ID: "entry-a", ProjectID: "proj-app", Description: "Sync docs", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
			{ID: "entry-c", ProjectID: "proj-other", Description: "Wrong project", TagIDs: []string{"tag-c"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T12:00:00Z", End: "2026-05-01T13:00:00Z"}},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(false), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}

	got := map[string]PlanItem{}
	for _, item := range plan.Items {
		got[item.IssueKey] = item
	}

	assertPlanItem(t, got["AAPP-1"], "skipped", "none")
	assertPlanItem(t, got["BAPP-1"], "ready", "create")
	assertPlanItem(t, got["CAPP-1"], "ready", "replace")
	assertPlanItem(t, got["DAPP-1"], "blocked", "none")
	if got["AAPP-1"].TargetAdapterInstance != config.ClockifyInstanceName {
		t.Fatalf("expected clockify target instance, got %#v", got["AAPP-1"])
	}
}

func TestCreateClockifyPushPlanDefaultsMissingTagCreationToTrue(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "DAPP-1", "2026-05-01T14:00:00Z", 3600, "Need tag")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	cfg := testClockifyConfig(true)
	cfg.File.Clockify.ProjectMapping.CreateIssueTagIfMissing = nil

	plan, err := service.CreateClockifyPushPlan(context.Background(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "create")
	if !item.InspectionSummary.RequiresTagCreate {
		t.Fatalf("expected omitted create_issue_tag_if_missing to default to tag creation, got %#v", item)
	}
}

func TestCreateClockifyPushPlanOnlyDeletedCreatesDeleteItem(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
		},
		entries: []clockify.TimeEntry{
			{ID: "entry-a", ProjectID: "proj-app", Description: "Remote row", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), true)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one delete item, got %d", len(plan.Items))
	}
	assertPlanItem(t, plan.Items[0], "ready", "delete")
	if plan.Items[0].LocalRowCount != 0 || plan.Items[0].InspectionSummary.LocalRowCount != 0 {
		t.Fatalf("expected tombstone delete scope to report zero local rows, got %#v", plan.Items[0])
	}
}

func TestCreateClockifyPushPlanIncludesTombstoneDeletesWithoutOnlyDeleted(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
		},
		entries: []clockify.TimeEntry{
			{ID: "entry-a", ProjectID: "proj-app", Description: "Remote row", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one delete item, got %d", len(plan.Items))
	}
	assertPlanItem(t, plan.Items[0], "ready", "delete")
	if plan.Items[0].LocalRowCount != 0 || plan.Items[0].InspectionSummary.LocalTotalSeconds != 0 {
		t.Fatalf("expected tombstone delete scope to keep local metrics empty, got %#v", plan.Items[0])
	}
}

func TestCreateClockifyPushPlanDeleteOnlyEmptyRemoteIsMatch(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one delete-only item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "skipped", "none")
	if item.ComparisonStatus != "match" || item.ReasonCode != "exact_match" {
		t.Fatalf("expected empty delete scope to be exact match, got %#v", item)
	}
	if item.LocalRowCount != 0 || item.LocalTotal != 0 {
		t.Fatalf("expected delete-only empty remote scope to report zero local metrics, got %#v", item)
	}
}

func TestCreateClockifyPushPlanDeleteOnlyFallsBackToTimeIntervalWhenTagDeleted(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		// AAPP-1 tag absent from tagsByID — simulates the tag having been deleted in Clockify
		tagsByID: map[string]clockify.Tag{},
		entries: []clockify.TimeEntry{
			{ID: "entry-orphan", ProjectID: "proj-app", Description: "Remote row", TagIDs: []string{"deleted-tag-id"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(plan.Items))
	}
	assertPlanItem(t, plan.Items[0], "ready", "delete")
	if plan.Items[0].RemoteRowCount != 1 {
		t.Fatalf("expected remote row count 1 from time-interval fallback, got %d", plan.Items[0].RemoteRowCount)
	}
	if plan.Items[0].RemoteTotal != 3600 {
		t.Fatalf("expected remote total 3600 from time-interval fallback, got %d", plan.Items[0].RemoteTotal)
	}
}

func TestApplyPlanPushMixedResult(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Sync docs")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 3600, "Build feature")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
			"tag-b": {ID: "tag-b", Name: "BAPP-1"},
		},
		createTimeEntryErrByIssue: map[string]error{
			"BAPP-1": errors.New("create failed"),
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	cfg := testClockifyConfig(true)
	plan, err := service.CreateClockifyPushPlan(context.Background(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 1 || !result.MixedResult {
		t.Fatalf("unexpected apply result %#v", result)
	}

	appliedPlan, err := service.LoadPlan(plan.ID)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	states := map[string]string{}
	for _, item := range appliedPlan.Items {
		states[item.IssueKey] = item.AppliedState
	}
	if states["AAPP-1"] != "succeeded" || states["BAPP-1"] != "failed" {
		t.Fatalf("unexpected applied states %#v", states)
	}

	var attempts int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM delivery_attempts`).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 4 {
		t.Fatalf("expected 4 delivery attempt events, got %d", attempts)
	}
}

func TestApplyPlanSkipsNonNotAttemptedReadyItems(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Create me")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 3600, "Already failed")
	seedWorklogRow(t, store, "row-3", "CAPP-1", "2026-05-01T12:00:00Z", 3600, "Pending")
	seedWorklogRow(t, store, "row-4", "DAPP-1", "2026-05-01T14:00:00Z", 3600, "Stale pending")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
	}
	service := NewService(store)
	service.now = func() time.Time { return mustTime("2026-05-02T12:00:00Z") }
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}

	itemsByIssue := map[string]PlanItem{}
	for _, item := range plan.Items {
		itemsByIssue[item.IssueKey] = item
	}
	seedDeliveryAttempt(t, store, plan.ID, itemsByIssue["BAPP-1"].ID, "failed", "failed before", "2026-05-02T10:00:00Z")
	seedDeliveryAttempt(t, store, plan.ID, itemsByIssue["CAPP-1"].ID, "pending", "pending now", "2026-05-02T11:50:00Z")
	seedDeliveryAttempt(t, store, plan.ID, itemsByIssue["DAPP-1"].ID, "pending", "stale", "2026-05-02T11:00:00Z")

	result, err := service.ApplyPlan(testClockifyConfig(true), plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.SkippedCount != 3 || result.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", result)
	}

	loaded, err := service.LoadPlan(plan.ID)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	states := map[string]string{}
	for _, item := range loaded.Items {
		states[item.IssueKey] = item.ExecutionState
	}
	if states["AAPP-1"] != "succeeded" || states["BAPP-1"] != "failed" || states["CAPP-1"] != "pending" || states["DAPP-1"] != "uncertain" {
		t.Fatalf("unexpected execution states %#v", states)
	}
}

func TestRetryPlanFailedReexecutesOnlyFailedReadyItems(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Retry me")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 3600, "Already done")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	itemsByIssue := map[string]PlanItem{}
	for _, item := range plan.Items {
		itemsByIssue[item.IssueKey] = item
	}
	seedDeliveryAttempt(t, store, plan.ID, itemsByIssue["AAPP-1"].ID, "failed", "first failed", "2026-05-02T10:00:00Z")
	seedDeliveryAttempt(t, store, plan.ID, itemsByIssue["BAPP-1"].ID, "succeeded", "done", "2026-05-02T10:00:00Z")

	result, err := service.RetryPlan(testClockifyConfig(true), plan.ID, "failed")
	if err != nil {
		t.Fatalf("RetryPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.SkippedCount != 1 || result.FailedCount != 0 || result.RetryScope != "failed" {
		t.Fatalf("unexpected retry result %#v", result)
	}
	if len(client.entries) != 1 {
		t.Fatalf("expected one retried remote create, got %#v", client.entries)
	}
}

func TestRetryPlanUncertainClockifyHandlesSuccessReplayAndAmbiguous(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Already there")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 3600, "Replay me")
	seedWorklogRow(t, store, "row-3", "CAPP-1", "2026-05-01T12:00:00Z", 3600, "Ambiguous")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
			"tag-b": {ID: "tag-b", Name: "BAPP-1"},
			"tag-c": {ID: "tag-c", Name: "CAPP-1"},
		},
		entries: []clockify.TimeEntry{
			{ID: "entry-c", ProjectID: "proj-app", Description: "Remote drift", TagIDs: []string{"tag-c"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T12:00:00Z", End: "2026-05-01T13:00:00Z"}},
		},
	}
	service := NewService(store)
	service.now = func() time.Time { return mustTime("2026-05-02T12:00:00Z") }
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	client.entries = append(client.entries, clockify.TimeEntry{
		ID:          "entry-a",
		ProjectID:   "proj-app",
		Description: "Already there",
		TagIDs:      []string{"tag-a"},
		TimeInterval: clockify.TimeInterval{
			Start: "2026-05-01T08:00:00Z",
			End:   "2026-05-01T09:00:00Z",
		},
	})
	itemsByIssue := map[string]PlanItem{}
	for _, item := range plan.Items {
		itemsByIssue[item.IssueKey] = item
		seedDeliveryAttempt(t, store, plan.ID, item.ID, "pending", "timed out", "2026-05-02T11:00:00Z")
	}

	result, err := service.RetryPlan(testClockifyConfig(true), plan.ID, "uncertain")
	if err != nil {
		t.Fatalf("RetryPlan failed: %v", err)
	}
	if result.AppliedCount != 2 || result.FailedCount != 1 || !result.MixedResult {
		t.Fatalf("unexpected uncertain retry result %#v", result)
	}

	loaded, err := service.LoadPlan(plan.ID)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	states := map[string]string{}
	for _, item := range loaded.Items {
		states[item.IssueKey] = item.ExecutionState
	}
	if states["AAPP-1"] != "succeeded" || states["BAPP-1"] != "succeeded" || states["CAPP-1"] != "uncertain" {
		t.Fatalf("unexpected retry execution states %#v", states)
	}
	if countClockifyEntriesByTag(client.entries, "tag-b") != 1 {
		t.Fatalf("expected replay to create one BAPP-1 row, got %#v", client.entries)
	}
	if countClockifyEntriesByTag(client.entries, "tag-c") != 1 {
		t.Fatalf("ambiguous scope should not create duplicate CAPP-1 rows, got %#v", client.entries)
	}
}

func TestRetryPlanRejectsFingerprintMismatchAndNoEligibleReturnsNoOp(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Done")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	plan, err := service.CreateClockifyPushPlan(context.Background(), testClockifyConfig(true), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	seedDeliveryAttempt(t, store, plan.ID, plan.Items[0].ID, "succeeded", "done", "2026-05-02T10:00:00Z")

	if _, err := service.RetryPlan(testClockifyConfig(false), plan.ID, "failed"); err == nil || err.Error() != "saved plan config fingerprint does not match current config; run 'workledger plan reconcile' to generate a new plan" {
		t.Fatalf("expected fingerprint mismatch, got %v", err)
	}

	result, err := service.RetryPlan(testClockifyConfig(true), plan.ID, "failed")
	if err != nil {
		t.Fatalf("RetryPlan failed: %v", err)
	}
	if !result.NoOp || result.AppliedCount != 0 || result.SkippedCount != 1 {
		t.Fatalf("expected noop retry result, got %#v", result)
	}
}

func TestApplyPlanPushRunsDistinctTargetGroupsConcurrently(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "First")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 3600, "Second")

	client := &blockingClockifyClient{
		fakeClockifyClient: fakeClockifyClient{
			projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		},
		createStarted: make(chan string, 2),
		release:       make(chan struct{}),
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	cfg := testClockifyConfig(true)
	plan, err := service.CreateClockifyPushPlan(context.Background(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.ApplyPlan(cfg, plan.ID)
		done <- err
	}()

	started := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for len(started) < 2 {
		select {
		case issue := <-client.createStarted:
			started[issue] = true
		case <-timeout:
			t.Fatalf("expected concurrent create calls for distinct target groups, got %#v", started)
		}
	}
	close(client.release)

	if err := <-done; err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
}

func TestCreateJiraDataPullPlanAndPushPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"AAPP-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
				"REPORT-1": {
					{ID: "foreign", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "other", Author: jiradatacenter.WorklogUser{AccountID: "u2"}},
				},
			},
		}
	}

	pullPlan, err := service.CreateJiraDataPullPlan(context.Background(), testJiraDataConfig(), "internal", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraDataPullPlan failed: %v", err)
	}
	if len(pullPlan.Items) != 1 || pullPlan.Items[0].PlanStatus != "skipped" {
		t.Fatalf("unexpected pull plan %#v", pullPlan.Items)
	}

	pushPlan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(pushPlan.Items) != 1 {
		t.Fatalf("expected one push item, got %d", len(pushPlan.Items))
	}
	if pushPlan.Items[0].PlanStatus != "blocked" || pushPlan.Items[0].PlannedAction != "none" {
		t.Fatalf("unexpected push item %#v", pushPlan.Items[0])
	}
	if !pushPlan.Items[0].InspectionSummary.ForeignAuthorPresent {
		t.Fatalf("expected foreign author marker in inspection summary")
	}
}

func TestCreateJiraDataPullPlanImplicitlyExcludesReportingTargets(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}, {Key: "REPORT-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"AAPP-1":   {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}}},
				"REPORT-1": {{ID: "w2", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "reporting", Author: jiradatacenter.WorklogUser{AccountID: "u1"}}},
			},
		}
	}

	cfg := testJiraDataConfig()
	cfg.File.JiraData.Instances["internal"] = config.JiraDataCenterInstance{
		BaseURL: "https://jira.example.com",
		Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
		Routing: cfg.File.JiraData.Instances["internal"].Routing,
	}

	plan, err := service.CreateJiraDataPullPlan(context.Background(), cfg, "internal", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraDataPullPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].IssueKey != "AAPP-1" {
		t.Fatalf("expected reporting target to be excluded, got %#v", plan.Items)
	}
}

func TestCreateJiraDataPushPlanDeleteOnlyMetricsAndEmptyRemoteMatch(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one delete-only item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "skipped", "none")
	if item.ComparisonStatus != "match" || item.ReasonCode != "exact_match" {
		t.Fatalf("expected empty delete scope to be exact match, got %#v", item)
	}
	if item.LocalRowCount != 0 || item.InspectionSummary.LocalRowCount != 0 || item.InspectionSummary.PerSourceTotals["AAPP-1"] != 0 {
		t.Fatalf("expected tombstone-backed Jira scope to report zero local metrics, got %#v", item)
	}
}

func TestCreateJiraDataPushPlanDeleteOnlyRemotePresentStaysDelete(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "remote", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one delete-only item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "delete")
	if item.LocalRowCount != 0 || item.InspectionSummary.LocalTotalSeconds != 0 {
		t.Fatalf("expected tombstone-backed Jira delete scope to report zero local metrics, got %#v", item)
	}
}

func TestCreateJiraDataPushPlanRequiresRouting(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	_, err := service.CreateJiraDataPushPlan(
		context.Background(),
		config.EffectiveConfig{
			SQLitePath:             "/tmp/workledger.db",
			MinimumDurationSeconds: 900,
			Location:               time.UTC,
			File: config.FileConfig{
				JiraData: &config.JiraDataCenterConfig{
					Instances: map[string]config.JiraDataCenterInstance{
						"internal": {
							BaseURL: "https://jira.example.com",
							Auth: config.JiraDataCenterAuthWrap{
								Bearer: config.JiraDataCenterBearer{Token: "token"},
							},
						},
					},
				},
			},
		},
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err == nil || err.Error() != "jira_data_center routing is required for push" {
		t.Fatalf("expected routing validation error, got %v", err)
	}
}

func TestCreateJiraCloudPullPushAndApplyPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:         jiracloud.User{AccountID: "u1"},
			searchIssues: []jiracloud.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"AAPP-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
				"REPORT-1": {},
			},
		}
	}

	pullPlan, err := service.CreateJiraCloudPullPlan(context.Background(), testJiraCloudConfig(), "product", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraCloudPullPlan failed: %v", err)
	}
	if len(pullPlan.Items) != 1 || pullPlan.Items[0].PlanStatus != "skipped" {
		t.Fatalf("unexpected pull plan %#v", pullPlan.Items)
	}

	pushPlan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(pushPlan.Items) != 1 {
		t.Fatalf("expected one push item, got %d", len(pushPlan.Items))
	}
	item := pushPlan.Items[0]
	if item.PlanStatus != "ready" || item.PlannedAction != "create" {
		t.Fatalf("unexpected push item %#v", item)
	}
	if item.InspectionSummary.ForeignAuthorPresent {
		t.Fatalf("expected no foreign author marker in inspection summary")
	}
	if item.Payload[0].Description != "AAPP-1 | Build feature" {
		t.Fatalf("expected reporting description prefix, got %#v", item.Payload)
	}

	result, err := service.ApplyPlan(testJiraCloudConfig(), pushPlan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", result)
	}

	appliedPlan, err := service.LoadPlan(pushPlan.ID)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if appliedPlan.Items[0].ApplyMessage != "applied saved push payload to jira-cloud" {
		t.Fatalf("unexpected apply message %q", appliedPlan.Items[0].ApplyMessage)
	}
}

func TestCreateJiraCloudPullPlanImplicitlyExcludesReportingTargets(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:         jiracloud.User{AccountID: "u1"},
			searchIssues: []jiracloud.IssueBrief{{Key: "AAPP-1"}, {Key: "REPORT-1"}},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"AAPP-1":   {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}}},
				"REPORT-1": {{ID: "w2", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "reporting", Author: jiracloud.WorklogUser{AccountID: "u1"}}},
			},
		}
	}

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{
		BaseURL: "https://example.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
		Routing: cfg.File.JiraCloud.Instances["product"].Routing,
	}

	plan, err := service.CreateJiraCloudPullPlan(context.Background(), cfg, "product", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraCloudPullPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].IssueKey != "AAPP-1" {
		t.Fatalf("expected reporting target to be excluded, got %#v", plan.Items)
	}
}

func TestCreateJiraCloudPushPlanDeleteOnlyMetricsAndValidation(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedTombstoneRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "2026-05-02T08:00:00Z")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{"REPORT-1": {}},
		}
	}

	plan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one delete-only item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "skipped", "none")
	if item.LocalRowCount != 0 || item.InspectionSummary.LocalRowCount != 0 || item.InspectionSummary.PerSourceTotals["AAPP-1"] != 0 {
		t.Fatalf("expected tombstone-backed Jira Cloud scope to report zero local metrics, got %#v", item)
	}

	_, err = service.CreateJiraCloudPushPlan(context.Background(), config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"product": {
						BaseURL: "https://example.atlassian.net",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "token"},
					},
				},
			},
		},
	}, "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err == nil || err.Error() != "jira_cloud routing is required for push" {
		t.Fatalf("expected routing validation error, got %v", err)
	}
}

func TestCreateJiraCloudPullPlanResolvesIssueIDOnlySearchResults(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:         jiracloud.User{AccountID: "u1"},
			searchIssues: []jiracloud.IssueBrief{{ID: "10001"}},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"10001": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
			},
			issuesByRef: map[string]jiracloud.IssueBrief{
				"10001": {ID: "10001", Key: "AAPP-1"},
			},
		}
	}

	plan, err := service.CreateJiraCloudPullPlan(context.Background(), testJiraCloudConfig(), "product", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraCloudPullPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one pull item, got %d", len(plan.Items))
	}
	if plan.Items[0].IssueKey != "AAPP-1" || plan.Items[0].TargetIssue != "AAPP-1" {
		t.Fatalf("expected resolved issue key in plan item, got %#v", plan.Items[0])
	}
	if plan.Items[0].RemoteRowCount != 1 {
		t.Fatalf("expected one remote row, got %#v", plan.Items[0])
	}
}

func TestReconcileJiraDataPushPlanReturnsAlreadyInSyncNoPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user: jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}}},
			},
		}
	}

	result, err := service.ReconcileJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraDataPushPlan failed: %v", err)
	}
	if result.Plan != nil || result.NoPlan == nil || result.NoPlan.Reason != "already_in_sync" {
		t.Fatalf("unexpected reconcile result %#v", result)
	}
	if result.NoPlan.MatchedScopeCount != 1 || result.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("unexpected no-plan summary %#v", result.NoPlan)
	}
	plans, err := service.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("expected no saved plans, got %#v", plans)
	}
}

func TestReconcileJiraDataPushPlanAggregatesSharedReportingTargets(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 1800, "Review PR")

	service := NewService(store)
	client := &fakeJiraDataClient{
		user:            jiradatacenter.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiradatacenter.Worklog{"REPORT-1": {}},
	}
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient { return client }

	result, err := service.ReconcileJiraDataPushPlan(context.Background(), testAggregatedJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraDataPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if len(result.Plan.Items) != 1 {
		t.Fatalf("expected one aggregated reporting item, got %#v", result.Plan.Items)
	}
	item := result.Plan.Items[0]
	assertPlanItem(t, item, "ready", "create")
	if item.IssueKey != "REPORT-1" || item.TargetIssue != "REPORT-1" {
		t.Fatalf("expected aggregated target issue scope, got %#v", item)
	}
	if len(item.Payload) != 2 || len(item.InspectionSummary.SourceIssueKeys) != 2 {
		t.Fatalf("expected aggregated payload and provenance, got %#v", item)
	}
	if item.InspectionSummary.PerSourceTotals["AAPP-1"] != 3600 || item.InspectionSummary.PerSourceTotals["BAPP-1"] != 1800 {
		t.Fatalf("unexpected per-source totals %#v", item.InspectionSummary.PerSourceTotals)
	}

	applyResult, err := service.ApplyPlan(testAggregatedJiraDataConfig(), result.Plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if applyResult.AppliedCount != 1 || applyResult.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", applyResult)
	}

	second, err := service.ReconcileJiraDataPushPlan(context.Background(), testAggregatedJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("second ReconcileJiraDataPushPlan failed: %v", err)
	}
	if second.Plan != nil || second.NoPlan == nil || second.NoPlan.Reason != "already_in_sync" {
		t.Fatalf("expected already_in_sync no-plan, got %#v", second)
	}
	if second.NoPlan.MatchedScopeCount != 1 || second.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("unexpected second no-plan summary %#v", second.NoPlan)
	}
}

func TestCreateJiraDataPushPlanAggregatesMixedReportingActiveAndTombstoneState(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedTombstoneRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 1800, "2026-05-02T08:00:00Z")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user: jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
					{ID: "w2", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "BAPP-1 | Old row", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testAggregatedJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one aggregated reporting item, got %#v", plan.Items)
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "replace")
	if item.IssueKey != "REPORT-1" || len(item.InspectionSummary.SourceIssueKeys) != 2 {
		t.Fatalf("unexpected aggregated item %#v", item)
	}
	if item.InspectionSummary.PerSourceTotals["AAPP-1"] != 3600 || item.InspectionSummary.PerSourceTotals["BAPP-1"] != 0 {
		t.Fatalf("unexpected mixed-state per-source totals %#v", item.InspectionSummary.PerSourceTotals)
	}
}

func TestReconcileJiraCloudPushPlanReturnsNoMatchingRoutesNoPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "ZAPP-1", "2026-05-01T08:00:00Z", 3600, "Unmapped")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{},
		}
	}

	result, err := service.ReconcileJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if result.Plan != nil || result.NoPlan == nil || result.NoPlan.Reason != "no_matching_routes" {
		t.Fatalf("unexpected reconcile result %#v", result)
	}
	if result.NoPlan.MatchedScopeCount != 0 || result.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("unexpected no-plan summary %#v", result.NoPlan)
	}
}

func TestReconcileJiraCloudPushPlanAggregatesSharedReportingTargets(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 1800, "Review PR")

	service := NewService(store)
	client := &fakeJiraCloudClient{
		user:            jiracloud.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiracloud.Worklog{"REPORT-1": {}},
	}
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient { return client }

	result, err := service.ReconcileJiraCloudPushPlan(context.Background(), testAggregatedJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if len(result.Plan.Items) != 1 {
		t.Fatalf("expected one aggregated reporting item, got %#v", result.Plan.Items)
	}
	item := result.Plan.Items[0]
	assertPlanItem(t, item, "ready", "create")
	if item.IssueKey != "REPORT-1" || item.TargetIssue != "REPORT-1" {
		t.Fatalf("expected aggregated target issue scope, got %#v", item)
	}
	if len(item.Payload) != 2 || len(item.InspectionSummary.SourceIssueKeys) != 2 {
		t.Fatalf("expected aggregated payload and provenance, got %#v", item)
	}
	if item.InspectionSummary.PerSourceTotals["AAPP-1"] != 3600 || item.InspectionSummary.PerSourceTotals["BAPP-1"] != 1800 {
		t.Fatalf("unexpected per-source totals %#v", item.InspectionSummary.PerSourceTotals)
	}

	applyResult, err := service.ApplyPlan(testAggregatedJiraCloudConfig(), result.Plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if applyResult.AppliedCount != 1 || applyResult.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", applyResult)
	}

	second, err := service.ReconcileJiraCloudPushPlan(context.Background(), testAggregatedJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("second ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if second.Plan != nil || second.NoPlan == nil || second.NoPlan.Reason != "already_in_sync" {
		t.Fatalf("expected already_in_sync no-plan, got %#v", second)
	}
	if second.NoPlan.MatchedScopeCount != 1 || second.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("unexpected second no-plan summary %#v", second.NoPlan)
	}
}

func TestReconcileJiraCloudPushPlanPersistsBlockedReportingPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {{ID: "foreign", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "other", Author: jiracloud.WorklogUser{AccountID: "u2"}}},
			},
		}
	}

	result, err := service.ReconcileJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if len(result.Plan.Items) != 1 || result.Plan.Items[0].PlanStatus != "blocked" {
		t.Fatalf("unexpected plan items %#v", result.Plan.Items)
	}
	plans, err := service.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one saved plan, got %#v", plans)
	}
}

func TestReconcileJiraCloudPushPlanPartialCurrentUserFailurePersistsPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 1800, "Review PR")

	cfg := testAggregatedJiraCloudConfig()
	cfg.File.JiraCloud.Instances = map[string]config.JiraCloudInstance{
		"alpha": {
			BaseURL: "https://alpha.example.atlassian.net",
			Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
			Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{"default": {IssuePrefixes: []string{"AAPP"}}}},
		},
		"beta": {
			BaseURL: "https://beta.example.atlassian.net",
			Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t2"},
			Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{"default": {IssuePrefixes: []string{"BAPP"}}}},
		},
	}

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		switch cfg.BaseURL {
		case "https://alpha.example.atlassian.net":
			return &fakeJiraCloudClient{user: jiracloud.User{AccountID: "u1"}, worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-1": {}}}
		case "https://beta.example.atlassian.net":
			return &fakeJiraCloudClient{currentUserErr: &jiracloud.RequestError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"}}
		default:
			t.Fatalf("unexpected base url %s", cfg.BaseURL)
			return nil
		}
	}

	result, err := service.ReconcileJiraCloudPushPlan(context.Background(), cfg, "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "check_failed" || len(result.Plan.Items) != 2 {
		t.Fatalf("unexpected partial plan %#v", result.Plan)
	}

	var readyItem, failedItem *PlanItem
	for i := range result.Plan.Items {
		item := &result.Plan.Items[i]
		switch item.IssueKey {
		case "AAPP-1":
			readyItem = item
		case "BAPP-1":
			failedItem = item
		}
	}
	if readyItem == nil || failedItem == nil {
		t.Fatalf("expected both scopes in plan %#v", result.Plan.Items)
	}
	assertPlanItem(t, *readyItem, "ready", "create")
	if failedItem.PlanStatus != "check_failed" || failedItem.PlannedAction != "none" || failedItem.ComparisonStatus != "check_failed" {
		t.Fatalf("unexpected failed item %#v", failedItem)
	}
	if failedItem.ReasonCode != "auth_error" || failedItem.ReasonDetail != "jira cloud authentication failed" {
		t.Fatalf("unexpected failed item reason %#v", failedItem)
	}
	if failedItem.TargetAdapterInstance != "beta" || failedItem.TargetIssue != "BAPP-1" || failedItem.LocalRowCount != 1 || failedItem.LocalTotal != 1800 {
		t.Fatalf("expected preserved failed-scope metadata %#v", failedItem)
	}

	plans, err := service.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one saved plan, got %#v", plans)
	}
}

func TestReconcileJiraCloudPushPlanPartialWorklogFailureMarksSingleScope(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "AAPP-2", "2026-05-01T10:00:00Z", 1800, "Review PR")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-1": {}},
			worklogErrByKey: map[string]error{"AAPP-2": &jiracloud.RequestError{StatusCode: http.StatusNotFound, Status: "404 Not Found"}},
		}
	}

	result, err := service.ReconcileJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "check_failed" || len(result.Plan.Items) != 2 {
		t.Fatalf("unexpected partial plan %#v", result.Plan)
	}

	for _, item := range result.Plan.Items {
		switch item.IssueKey {
		case "AAPP-1":
			assertPlanItem(t, item, "ready", "create")
		case "AAPP-2":
			if item.PlanStatus != "check_failed" || item.ReasonCode != "not_found" || item.ReasonDetail != "jira cloud resource not found" {
				t.Fatalf("unexpected failed item %#v", item)
			}
		default:
			t.Fatalf("unexpected item %#v", item)
		}
	}
}

func TestReconcileJiraCloudReportingExactMatchPlusFailedReadStillPersistsPlan(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 1800, "Review PR")

	cfg := testAggregatedJiraCloudConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{
		BaseURL: "https://example.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
		Pull:    config.JiraPullConfig{ExcludeIssues: []string{"REPORT-1", "REPORT-2"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default": {IssuePrefixes: []string{"AAPP", "BAPP"}},
				"reporting": {
					ReportingTargets: map[string]string{
						"AAPP": "REPORT-1",
						"BAPP": "REPORT-2",
					},
				},
			},
		},
	}

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}}},
			},
			worklogErrByKey: map[string]error{"REPORT-2": &jiracloud.RequestError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"}},
		}
	}

	result, err := service.ReconcileJiraCloudPushPlan(context.Background(), cfg, "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraCloudPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan instead of no-plan result, got %#v", result)
	}
	if result.Plan.AggregateStatus != "check_failed" || len(result.Plan.Items) != 2 {
		t.Fatalf("unexpected reporting plan %#v", result.Plan)
	}

	for _, item := range result.Plan.Items {
		switch item.TargetIssue {
		case "REPORT-1":
			if item.PlanStatus != "skipped" || item.ReasonCode != "exact_match" {
				t.Fatalf("unexpected exact-match item %#v", item)
			}
		case "REPORT-2":
			if item.PlanStatus != "check_failed" || item.ReasonCode != "auth_error" {
				t.Fatalf("unexpected failed item %#v", item)
			}
		default:
			t.Fatalf("unexpected item %#v", item)
		}
	}
}

func TestReconcileJiraDataPushPlanPartialWorklogFailureMarksSingleScope(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "AAPP-2", "2026-05-01T10:00:00Z", 1800, "Review PR")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:            jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{"AAPP-1": {}},
			worklogErrByKey: map[string]error{"AAPP-2": &jiradatacenter.RequestError{StatusCode: http.StatusNotFound, Status: "404 Not Found"}},
		}
	}

	result, err := service.ReconcileJiraDataPushPlan(context.Background(), testJiraDataConfig(), "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraDataPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "check_failed" || len(result.Plan.Items) != 2 {
		t.Fatalf("unexpected partial plan %#v", result.Plan)
	}

	for _, item := range result.Plan.Items {
		switch item.IssueKey {
		case "AAPP-1":
			assertPlanItem(t, item, "ready", "create")
		case "AAPP-2":
			if item.PlanStatus != "check_failed" || item.ReasonCode != "not_found" || item.ReasonDetail != "jira data center resource not found" {
				t.Fatalf("unexpected failed item %#v", item)
			}
		default:
			t.Fatalf("unexpected item %#v", item)
		}
	}
}

func TestReconcileJiraDataPushPlanCurrentUserFailureMarksAllScopes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "AAPP-2", "2026-05-01T10:00:00Z", 1800, "Review PR")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			currentUserErr: &jiradatacenter.RequestError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"},
		}
	}

	result, err := service.ReconcileJiraDataPushPlan(context.Background(), testJiraDataConfig(), "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraDataPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "check_failed" || len(result.Plan.Items) != 2 {
		t.Fatalf("unexpected plan %#v", result.Plan)
	}
	for _, item := range result.Plan.Items {
		if item.PlanStatus != "check_failed" || item.ReasonCode != "auth_error" {
			t.Fatalf("expected check_failed item, got %#v", item)
		}
	}
}

func TestReconcileJiraDataPushPlanSupportsCrossFamilyCanonicalReportingPrefix(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "APPS-10", "2026-05-01T08:00:00Z", 3600, "Mirror me")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:            jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{"ACIU-123": {}},
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"maxima_lt_jira": {
						BaseURL: "https://cloud.example.com",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"APPS"}},
						}},
					},
				},
			},
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"ito_jira": {
						BaseURL: "https://jira.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"aciu_reporting": {ReportingTargets: map[string]string{"APPS": "ACIU-123"}},
						}},
					},
				},
			},
		},
	}

	result, err := service.ReconcileJiraDataPushPlan(context.Background(), cfg, "aciu_reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("ReconcileJiraDataPushPlan failed: %v", err)
	}
	if result.Plan == nil || result.NoPlan != nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	item := result.Plan.Items[0]
	if item.IssueKey != "ACIU-123" || item.TargetAdapterInstance != "ito_jira" || item.TargetIssue != "ACIU-123" {
		t.Fatalf("unexpected cross-family reporting item %#v", item)
	}
	if len(item.InspectionSummary.SourceIssueKeys) != 1 || item.InspectionSummary.SourceIssueKeys[0] != "APPS-10" {
		t.Fatalf("expected source provenance on aggregated reporting item, got %#v", item.InspectionSummary)
	}
}

func TestReconcileMultiPushPlanBuildsSinglePlanAcrossAdapters(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-10", "2026-05-01T08:00:00Z", 3600, "Cloud scope")
	seedWorklogRow(t, store, "row-2", "BAPP-20", "2026-05-01T10:00:00Z", 1800, "Data scope")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-10": {}},
		}
	}
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:            jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{"BAPP-20": {}},
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"maxima_lt_jira": {
						BaseURL: "https://cloud.example.com",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"AAPP"}},
						}},
					},
				},
			},
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"ito_jira": {
						BaseURL: "https://jira.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"BAPP"}},
						}},
					},
				},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		ReconcileScope{AdapterFamilies: []string{"jira-cloud", "jira-data-center"}},
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || result.NoPlan != nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	if len(result.Plan.Items) != 4 {
		t.Fatalf("expected four plan items, got %#v", result.Plan.Items)
	}
	if result.Plan.AdapterFamily != "multiple" {
		t.Fatalf("expected multiple adapter summary, got %#v", result.Plan)
	}
	if got := strings.Join(result.Plan.AdapterFamilies, ","); got != "jira-cloud,jira-data-center" {
		t.Fatalf("unexpected adapter families %q", got)
	}
	if got := strings.Join(result.Plan.TargetInstances, ","); got != "ito_jira,maxima_lt_jira" {
		t.Fatalf("unexpected target instances %q", got)
	}
	ready := 0
	for _, item := range result.Plan.Items {
		if item.PlanStatus == "ready" {
			ready++
		}
	}
	if ready != 2 {
		t.Fatalf("expected two actionable scopes, got %#v", result.Plan.Items)
	}

	if plans, err := service.ListPlans(); err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	} else if len(plans) != 1 {
		t.Fatalf("expected one saved plan, got %d", len(plans))
	}
}

func TestCreateMultiPullPlanInfersClockifyTargetInstance(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient {
		return &fakeClockifyClient{
			tagsByID: map[string]clockify.Tag{
				"tag-a": {ID: "tag-a", Name: "AAPP-1"},
			},
			entries: []clockify.TimeEntry{
				{ID: "entry-a", ProjectID: "proj-app", Description: "Sync docs", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
			},
		}
	}

	plan, err := service.CreateMultiPullPlan(
		context.Background(),
		testClockifyConfig(true),
		ReconcileScope{AdapterFamilies: []string{"clockify"}, Instances: []string{config.ClockifyInstanceName}},
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
	)
	if err != nil {
		t.Fatalf("CreateMultiPullPlan failed: %v", err)
	}
	if got := strings.Join(plan.TargetInstances, ","); got != config.ClockifyInstanceName {
		t.Fatalf("unexpected target instances %q", got)
	}
	if len(plan.Items) != 1 || plan.Items[0].TargetAdapterInstance != config.ClockifyInstanceName {
		t.Fatalf("expected clockify plan item instance, got %#v", plan.Items)
	}
}

func TestReconcileMultiPushPlanSkipsClockifyWhenAllowlistExcludesIt(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-10", "2026-05-01T08:00:00Z", 3600, "Cloud scope")

	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient {
		t.Fatal("clockify should not be selected")
		return nil
	}
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-10": {}},
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			Clockify: testClockifyConfig(true).File.Clockify,
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"maxima_lt_jira": {
						BaseURL: "https://cloud.example.com",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"AAPP"}},
						}},
					},
				},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		ReconcileScope{AdapterFamilies: []string{"clockify", "jira-cloud"}, Instances: []string{"maxima_lt_jira"}},
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || result.NoPlan != nil {
		t.Fatalf("expected saved plan, got %#v", result)
	}
	for _, item := range result.Plan.Items {
		if item.TargetAdapterFamily == "clockify" {
			t.Fatalf("did not expect clockify item, got %#v", result.Plan.Items)
		}
	}
}

func TestReconcileJiraCloudPushPlanRejectsReportingTargetOwnedByOtherFamily(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "APPS-10", "2026-05-01T08:00:00Z", 3600, "Mirror me")

	service := NewService(store)

	cfg := config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"maxima_lt_jira": {
						BaseURL: "https://cloud.example.com",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default":   {IssuePrefixes: []string{"APPS"}},
							"reporting": {ReportingTargets: map[string]string{"APPS": "ACIU-4393"}},
						}},
					},
				},
			},
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"ito_jira": {
						BaseURL: "https://jira.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"ACIU"}},
						}},
					},
				},
			},
		},
	}

	_, err := service.ReconcileJiraCloudPushPlan(context.Background(), cfg, "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err == nil {
		t.Fatal("expected reporting target ownership error")
	}
	if !strings.Contains(err.Error(), "ACIU-4393") || !strings.Contains(err.Error(), "ito_jira") || !strings.Contains(err.Error(), "--adapter=jira-data-center") {
		t.Fatalf("unexpected error %q", err)
	}
}

type fakeClockifyClient struct {
	entries                   []clockify.TimeEntry
	tagsByID                  map[string]clockify.Tag
	projects                  []clockify.Project
	createTimeEntryErrByIssue map[string]error
}

type blockingClockifyClient struct {
	fakeClockifyClient
	mu            sync.Mutex
	createStarted chan string
	release       chan struct{}
}

type fakeJiraDataClient struct {
	user            jiradatacenter.User
	searchIssues    []jiradatacenter.IssueBrief
	worklogsByIssue map[string][]jiradatacenter.Worklog
	currentUserErr  error
	worklogErrByKey map[string]error
}

type fakeJiraCloudClient struct {
	user            jiracloud.User
	searchIssues    []jiracloud.IssueBrief
	worklogsByIssue map[string][]jiracloud.Worklog
	issuesByRef     map[string]jiracloud.IssueBrief
	currentUserErr  error
	worklogErrByKey map[string]error
}

func (f *fakeJiraDataClient) CurrentUser(ctx context.Context) (jiradatacenter.User, error) {
	if f.currentUserErr != nil {
		return jiradatacenter.User{}, f.currentUserErr
	}
	return f.user, nil
}

func (f *fakeJiraDataClient) SearchIssues(ctx context.Context, jql string, fields []string) ([]jiradatacenter.IssueBrief, error) {
	return append([]jiradatacenter.IssueBrief(nil), f.searchIssues...), nil
}

func (f *fakeJiraDataClient) ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiradatacenter.Worklog, error) {
	if err := f.worklogErrByKey[issueKey]; err != nil {
		return nil, err
	}
	return append([]jiradatacenter.Worklog(nil), f.worklogsByIssue[issueKey]...), nil
}

func (f *fakeJiraDataClient) GetIssue(ctx context.Context, issueKey string, fields []string) (jiradatacenter.IssueBrief, error) {
	return jiradatacenter.IssueBrief{Key: issueKey}, nil
}

func (f *fakeJiraDataClient) CreateWorklog(ctx context.Context, issueKey string, row model.Row) (jiradatacenter.Worklog, error) {
	entry := jiradatacenter.Worklog{ID: "created", Started: row.StartedAtUTC.Format("2006-01-02T15:04:05.000-0700"), TimeSpentSeconds: row.DurationSeconds, Comment: row.Description, Author: jiradatacenter.WorklogUser{AccountID: f.user.AccountID}}
	f.worklogsByIssue[issueKey] = append(f.worklogsByIssue[issueKey], entry)
	return entry, nil
}

func (f *fakeJiraDataClient) DeleteWorklog(ctx context.Context, issueKey, worklogID string) error {
	items := f.worklogsByIssue[issueKey]
	filtered := make([]jiradatacenter.Worklog, 0, len(items))
	for _, item := range items {
		if item.ID != worklogID {
			filtered = append(filtered, item)
		}
	}
	f.worklogsByIssue[issueKey] = filtered
	return nil
}

func (f *fakeJiraCloudClient) CurrentUser(ctx context.Context) (jiracloud.User, error) {
	if f.currentUserErr != nil {
		return jiracloud.User{}, f.currentUserErr
	}
	return f.user, nil
}

func (f *fakeJiraCloudClient) SearchIssues(ctx context.Context, jql string, fields []string) ([]jiracloud.IssueBrief, error) {
	return append([]jiracloud.IssueBrief(nil), f.searchIssues...), nil
}

func (f *fakeJiraCloudClient) ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiracloud.Worklog, error) {
	if err := f.worklogErrByKey[issueKey]; err != nil {
		return nil, err
	}
	return append([]jiracloud.Worklog(nil), f.worklogsByIssue[issueKey]...), nil
}

func (f *fakeJiraCloudClient) GetIssue(ctx context.Context, issueKey string, fields []string) (jiracloud.IssueBrief, error) {
	if item, ok := f.issuesByRef[issueKey]; ok {
		return item, nil
	}
	return jiracloud.IssueBrief{Key: issueKey}, nil
}

func (f *fakeJiraCloudClient) CreateWorklog(ctx context.Context, issueKey string, row model.Row) (jiracloud.Worklog, error) {
	entry := jiracloud.Worklog{ID: "created", Started: row.StartedAtUTC.Format("2006-01-02T15:04:05.000-0700"), TimeSpentSeconds: row.DurationSeconds, Comment: row.Description, Author: jiracloud.WorklogUser{AccountID: f.user.AccountID}}
	f.worklogsByIssue[issueKey] = append(f.worklogsByIssue[issueKey], entry)
	return entry, nil
}

func (f *fakeJiraCloudClient) DeleteWorklog(ctx context.Context, issueKey, worklogID string) error {
	items := f.worklogsByIssue[issueKey]
	filtered := make([]jiracloud.Worklog, 0, len(items))
	for _, item := range items {
		if item.ID != worklogID {
			filtered = append(filtered, item)
		}
	}
	f.worklogsByIssue[issueKey] = filtered
	return nil
}

func (f *fakeClockifyClient) ListUserTimeEntries(ctx context.Context, workspaceID, userID string, start, end time.Time) ([]clockify.TimeEntry, error) {
	return append([]clockify.TimeEntry(nil), f.entries...), nil
}

func (f *fakeClockifyClient) ListTags(ctx context.Context, workspaceID string) (map[string]clockify.Tag, error) {
	items := map[string]clockify.Tag{}
	for id, tag := range f.tagsByID {
		items[id] = tag
	}
	return items, nil
}

func (f *fakeClockifyClient) ListProjects(ctx context.Context, workspaceID string) ([]clockify.Project, error) {
	return append([]clockify.Project(nil), f.projects...), nil
}

func (f *fakeClockifyClient) CreateTag(ctx context.Context, workspaceID, name string) (clockify.Tag, error) {
	tag := clockify.Tag{ID: "created-" + name, Name: name}
	if f.tagsByID == nil {
		f.tagsByID = map[string]clockify.Tag{}
	}
	f.tagsByID[tag.ID] = tag
	return tag, nil
}

func (f *fakeClockifyClient) CreateTimeEntry(ctx context.Context, workspaceID string, row clockify.CandidateRow, projectID string, tagIDs []string) (clockify.TimeEntry, error) {
	if err := f.createTimeEntryErrByIssue[row.IssueKey]; err != nil {
		return clockify.TimeEntry{}, err
	}
	entry := clockify.TimeEntry{
		ID:          "created-" + row.IssueKey + "-" + row.StartedAtUTC.Format(time.RFC3339),
		ProjectID:   projectID,
		Description: row.Description,
		TagIDs:      append([]string(nil), tagIDs...),
		TimeInterval: clockify.TimeInterval{
			Start: row.StartedAtUTC.Format(time.RFC3339),
			End:   row.StartedAtUTC.Add(time.Duration(row.DurationSeconds) * time.Second).Format(time.RFC3339),
		},
	}
	f.entries = append(f.entries, entry)
	return entry, nil
}

func (f *blockingClockifyClient) CreateTimeEntry(ctx context.Context, workspaceID string, row clockify.CandidateRow, projectID string, tagIDs []string) (clockify.TimeEntry, error) {
	f.createStarted <- row.IssueKey
	<-f.release

	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fakeClockifyClient.CreateTimeEntry(ctx, workspaceID, row, projectID, tagIDs)
}

func (f *fakeClockifyClient) DeleteTimeEntry(ctx context.Context, workspaceID, entryID string) error {
	filtered := make([]clockify.TimeEntry, 0, len(f.entries))
	for _, entry := range f.entries {
		if entry.ID != entryID {
			filtered = append(filtered, entry)
		}
	}
	f.entries = filtered
	return nil
}

func newTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "worklogs.db"))
	if err != nil {
		t.Fatalf("bootstrap store: %v", err)
	}
	return store
}

func seedWorklogRow(t *testing.T, store *sqlitestore.Store, id, issueKey, startedAt string, durationSeconds int, description string) {
	t.Helper()
	if _, err := store.DB().Exec(
		`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		id, issueKey, startedAt, durationSeconds, description, startedAt, startedAt,
	); err != nil {
		t.Fatalf("seed worklog: %v", err)
	}
}

func seedTombstoneRow(t *testing.T, store *sqlitestore.Store, id, issueKey, startedAt string, durationSeconds int, deletedAt string) {
	t.Helper()
	if _, err := store.DB().Exec(
		`INSERT INTO worklog_tombstones(worklog_id, issue_key, started_at_utc, duration_seconds, deleted_at) VALUES(?, ?, ?, ?, ?)`,
		id, issueKey, startedAt, durationSeconds, deletedAt,
	); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
}

func seedDeliveryAttempt(t *testing.T, store *sqlitestore.Store, planID, planItemID, state, message, createdAt string) {
	t.Helper()
	if _, err := store.DB().Exec(
		`INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		planItemID+"-"+state+"-"+createdAt, planID, planItemID, state, message, createdAt,
	); err != nil {
		t.Fatalf("seed delivery attempt: %v", err)
	}
}

func countClockifyEntriesByTag(entries []clockify.TimeEntry, tagID string) int {
	total := 0
	for _, entry := range entries {
		for _, candidate := range entry.TagIDs {
			if candidate == tagID {
				total++
				break
			}
		}
	}
	return total
}

func testClockifyConfig(createMissingTag bool) config.EffectiveConfig {
	return config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			Clockify: &config.ClockifyConfig{
				WorkspaceID: "ws-1",
				UserID:      "user-1",
				Auth:        config.ClockifyAuthConfig{APIKey: "key-1"},
				ProjectMapping: &config.ClockifyProjectConfig{
					IssuePrefixes:           map[string]string{"AAPP": "App", "BAPP": "App", "CAPP": "App", "DAPP": "App"},
					DefaultProject:          "App",
					CreateIssueTagIfMissing: boolPtr(createMissingTag),
				},
			},
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func testJiraDataConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"internal": {
						BaseURL: "https://jira.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
						Pull:    config.JiraPullConfig{ExcludeIssues: []string{"REPORT-1"}},
						Routing: &config.JiraInstanceRoutes{
							Profiles: map[string]config.JiraRouteProfile{
								"default": {IssuePrefixes: []string{"AAPP"}},
								"reporting": {
									ReportingTargets: map[string]string{
										"AAPP": "REPORT-1",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func testAggregatedJiraDataConfig() config.EffectiveConfig {
	cfg := testJiraDataConfig()
	cfg.File.JiraData.Instances["internal"] = config.JiraDataCenterInstance{
		BaseURL: "https://jira.example.com",
		Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
		Pull:    config.JiraPullConfig{ExcludeIssues: []string{"REPORT-1"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default": {IssuePrefixes: []string{"AAPP", "BAPP"}},
				"reporting": {
					ReportingTargets: map[string]string{
						"AAPP": "REPORT-1",
						"BAPP": "REPORT-1",
					},
				},
			},
		},
	}
	return cfg
}

func testJiraCloudConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"product": {
						BaseURL: "https://example.atlassian.net",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
						Pull:    config.JiraPullConfig{ExcludeIssues: []string{"REPORT-1"}},
						Routing: &config.JiraInstanceRoutes{
							Profiles: map[string]config.JiraRouteProfile{
								"default": {IssuePrefixes: []string{"AAPP"}},
								"reporting": {
									ReportingTargets: map[string]string{
										"AAPP": "REPORT-1",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func testAggregatedJiraCloudConfig() config.EffectiveConfig {
	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{
		BaseURL: "https://example.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
		Pull:    config.JiraPullConfig{ExcludeIssues: []string{"REPORT-1"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default": {IssuePrefixes: []string{"AAPP", "BAPP"}},
				"reporting": {
					ReportingTargets: map[string]string{
						"AAPP": "REPORT-1",
						"BAPP": "REPORT-1",
					},
				},
			},
		},
	}
	return cfg
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func assertPlanItem(t *testing.T, item PlanItem, wantStatus, wantAction string) {
	t.Helper()
	if item.PlanStatus != wantStatus || item.PlannedAction != wantAction {
		t.Fatalf("unexpected plan item for %s: status=%s action=%s", item.IssueKey, item.PlanStatus, item.PlannedAction)
	}
}

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
	"github.com/solitus0/workledger/internal/worklogs"
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

func TestSummarizeRowDiffs(t *testing.T) {
	base := func(issueKey, startedAt, description string, durationSeconds int) model.Row {
		return model.Row{
			IssueKey:        issueKey,
			StartedAtUTC:    mustTime(startedAt),
			DurationSeconds: durationSeconds,
			Description:     description,
		}
	}

	tests := []struct {
		name    string
		desired []model.Row
		current []model.Row
		want    rowDiffCounts
	}{
		{
			name: "same counts different content",
			desired: []model.Row{
				base("APPS-993", "2026-06-22T08:00:00Z", "Keep one", 1800),
				base("APPS-993", "2026-06-22T09:00:00Z", "Keep two", 1800),
				base("APPS-993", "2026-06-22T10:00:00Z", "Create one", 1800),
				base("APPS-993", "2026-06-22T11:00:00Z", "Create two", 1800),
			},
			current: []model.Row{
				base("APPS-993", "2026-06-22T08:00:00Z", "Keep one", 1800),
				base("APPS-993", "2026-06-22T09:00:00Z", "Keep two", 1800),
				base("APPS-993", "2026-06-22T12:00:00Z", "Delete one", 1800),
				base("APPS-993", "2026-06-22T13:00:00Z", "Delete two", 1800),
			},
			want: rowDiffCounts{Matched: 2, Create: 2, Delete: 2},
		},
		{
			name: "create only",
			desired: []model.Row{
				base("APPS-993", "2026-06-22T08:00:00Z", "Create one", 1800),
				base("APPS-993", "2026-06-22T09:00:00Z", "Create two", 1800),
			},
			want: rowDiffCounts{Create: 2},
		},
		{
			name: "delete only",
			current: []model.Row{
				base("APPS-993", "2026-06-22T08:00:00Z", "Delete one", 1800),
				base("APPS-993", "2026-06-22T09:00:00Z", "Delete two", 1800),
			},
			want: rowDiffCounts{Delete: 2},
		},
		{
			name: "exact match",
			desired: []model.Row{
				base("APPS-993", "2026-06-22T08:00:00Z", "Keep one", 1800),
			},
			current: []model.Row{
				base("APPS-993", "2026-06-22T08:00:00Z", "Keep one", 1800),
			},
			want: rowDiffCounts{Matched: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeRowDiffs(tt.desired, tt.current)
			if got != tt.want {
				t.Fatalf("summarizeRowDiffs() = %#v, want %#v", got, tt.want)
			}
		})
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

func TestCreateClockifyPushPlanRemoteOwnedOrphanCreatesCleanupItem(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

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
		t.Fatalf("expected one cleanup item, got %d", len(plan.Items))
	}
	assertPlanItem(t, plan.Items[0], "ready", "replace")
	if plan.Items[0].LocalRowCount != 0 || plan.Items[0].InspectionSummary.LocalRowCount != 0 {
		t.Fatalf("expected orphan cleanup scope to report zero local rows, got %#v", plan.Items[0])
	}
}

func TestApplyPlanClockifyRemoteOwnedOrphanArchivesTrash(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

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
		t.Fatalf("expected one cleanup item, got %d", len(plan.Items))
	}
	if plan.Items[0].LocalRowCount != 0 || plan.Items[0].RemoteRowCount != 1 {
		t.Fatalf("expected remote-only cleanup scope, got %#v", plan.Items[0])
	}

	result, err := service.ApplyPlan(testClockifyConfig(true), plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", result)
	}
	if result.TrashArchivedCount != 1 {
		t.Fatalf("expected one archived trash row, got %#v", result)
	}
	if got := countTrashRecords(t, store); got != 1 {
		t.Fatalf("expected one trashed remote row, got %d", got)
	}
	record := listTrashRecords(t, store)[0]
	if record.StorageScope != worklogs.TrashScopeRemote || record.SourceWorklogID != nil || record.ReasonCode != pushTrashReasonCode {
		t.Fatalf("unexpected trash record %#v", record)
	}
}

func TestApplyPlanPullMergeArchivesRemovedLocalRowsAndPreservesIDs(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Keep me")
	seedWorklogRow(t, store, "row-2", "AAPP-1", "2026-05-01T10:00:00Z", 1800, "Remove me")

	service := NewService(store)
	cfg := testClockifyConfig(true)
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		t.Fatalf("fingerprint config: %v", err)
	}

	plan := Plan{
		ID:                "plan-pull-trash",
		Direction:         "pull",
		AdapterFamily:     "clockify",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     mustTime("2026-05-01T00:00:00Z"),
		WindowToUTC:       mustTime("2026-05-01T23:59:59Z"),
		CreatedAt:         mustTime("2026-05-02T00:00:00Z"),
		AggregateStatus:   "ready",
		Items: []PlanItem{
			{
				ID:               "item-pull-trash",
				PlanID:           "plan-pull-trash",
				IssueKey:         "AAPP-1",
				PlanDirection:    "pull",
				WindowFromUTC:    mustTime("2026-05-01T00:00:00Z"),
				WindowToUTC:      mustTime("2026-05-01T23:59:59Z"),
				PlanStatus:       "ready",
				PlannedAction:    "merge",
				ComparisonStatus: "merge_needed",
				ReasonCode:       "remote_diff",
				ReasonDetail:     "seeded",
				Payload: []model.Row{
					{IssueKey: "AAPP-1", StartedAtUTC: mustTime("2026-05-01T08:00:00Z"), DurationSeconds: 3600, Description: "Keep me"},
					{IssueKey: "AAPP-1", StartedAtUTC: mustTime("2026-05-01T09:00:00Z"), DurationSeconds: 900, Description: "Add me"},
				},
				LocalRowCount:  2,
				LocalTotal:     5400,
				RemoteRowCount: 2,
				RemoteTotal:    4500,
				AppliedState:   "not_attempted",
			},
		},
	}
	if err := service.insertPlan(plan); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 || result.TrashArchivedCount != 1 {
		t.Fatalf("unexpected apply result %#v", result)
	}
	if len(result.ScopeResults) != 1 || result.ScopeResults[0].TrashArchivedCount != 1 {
		t.Fatalf("expected scope trash summary, got %#v", result.ScopeResults)
	}

	scopeRows, err := service.listLocalScope("AAPP-1", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("list local scope: %v", err)
	}
	if len(scopeRows) != 2 {
		t.Fatalf("expected two active rows after merge, got %#v", scopeRows)
	}
	foundPreserved := false
	foundInserted := false
	for _, row := range scopeRows {
		if row.ID == "row-1" && row.Description == "Keep me" {
			foundPreserved = true
		}
		if row.ID != "row-1" && row.Description == "Add me" {
			foundInserted = true
		}
		if row.ID == "row-2" {
			t.Fatalf("removed row should not remain active: %#v", scopeRows)
		}
	}
	if !foundPreserved || !foundInserted {
		t.Fatalf("expected preserved and inserted rows, got %#v", scopeRows)
	}

	records := listTrashRecords(t, store)
	if len(records) != 1 {
		t.Fatalf("expected one trash row, got %#v", records)
	}
	record := records[0]
	if record.StorageScope != worklogs.TrashScopeLocal || record.SourceWorklogID == nil || *record.SourceWorklogID != "row-2" || record.ReasonCode != pullTrashReasonCode {
		t.Fatalf("unexpected pull trash record %#v", record)
	}
}

func TestApplyPlanClockifyReplaceFailureStillArchivesRemoteTrash(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Local desired")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
		},
		entries: []clockify.TimeEntry{
			{ID: "entry-a", ProjectID: "proj-app", Description: "Remote old", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
		},
		createTimeEntryErrByIssue: map[string]error{
			"AAPP-1": errors.New("create failed"),
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	cfg := testClockifyConfig(true)
	plan, err := service.CreateClockifyPushPlan(context.Background(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].PlannedAction != "replace" {
		t.Fatalf("expected one replace item, got %#v", plan.Items)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 0 || result.FailedCount != 1 || result.TrashArchivedCount != 1 {
		t.Fatalf("unexpected apply result %#v", result)
	}
	if got := countTrashRecords(t, store); got != 1 {
		t.Fatalf("expected archived remote delete despite create failure, got %d", got)
	}
}

func TestApplyPlanClockifyReplaceDeletesOnlyConflictingRemoteRows(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Keep me")
	seedWorklogRow(t, store, "row-2", "AAPP-1", "2026-05-01T10:00:00Z", 3600, "Make me exact")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
		},
		entries: []clockify.TimeEntry{
			{ID: "entry-keep", ProjectID: "proj-app", Description: "Keep me", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T08:00:00Z", End: "2026-05-01T09:00:00Z"}},
			{ID: "entry-conflict", ProjectID: "proj-app", Description: "Make me exact", TagIDs: []string{"tag-a"}, TimeInterval: clockify.TimeInterval{Start: "2026-05-01T10:00:00Z", End: "2026-05-01T10:30:00Z"}},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	cfg := testClockifyConfig(true)
	plan, err := service.CreateClockifyPushPlan(context.Background(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].PlannedAction != "replace" {
		t.Fatalf("expected one replace item, got %#v", plan.Items)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 || result.TrashArchivedCount != 1 {
		t.Fatalf("unexpected apply result %#v", result)
	}

	if len(client.entries) != 2 {
		t.Fatalf("expected two remote entries after minimal diff apply, got %#v", client.entries)
	}
	byID := map[string]clockify.TimeEntry{}
	for _, entry := range client.entries {
		byID[entry.ID] = entry
	}
	if _, ok := byID["entry-keep"]; !ok {
		t.Fatalf("expected exact-match remote entry to remain, got %#v", client.entries)
	}
	if _, ok := byID["entry-conflict"]; ok {
		t.Fatalf("expected conflicting remote entry to be deleted, got %#v", client.entries)
	}
	created, ok := byID["created-AAPP-1-2026-05-01T10:00:00Z"]
	if !ok || created.TimeInterval.End != "2026-05-01T11:00:00Z" {
		t.Fatalf("expected only missing desired row to be created, got %#v", client.entries)
	}

	records := listTrashRecords(t, store)
	if len(records) != 1 || records[0].DurationSeconds != 1800 || records[0].Description != "Make me exact" {
		t.Fatalf("expected only conflicting remote row to be archived, got %#v", records)
	}
}

func TestApplyPlanJiraDataReplaceDeletesOnlyConflictingRemoteRows(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Keep me")
	seedWorklogRow(t, store, "row-2", "AAPP-1", "2026-05-01T10:00:00Z", 3600, "Make me exact")

	client := &fakeJiraDataClient{
		user: jiradatacenter.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiradatacenter.Worklog{
			"AAPP-1": {
				{ID: "w-keep", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Keep me", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				{ID: "w-conflict", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "Make me exact", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
			},
		},
	}
	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient { return client }

	cfg := testJiraDataConfig()
	plan, err := service.CreateJiraDataPushPlan(context.Background(), cfg, "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].PlannedAction != "replace" {
		t.Fatalf("expected one replace item, got %#v", plan.Items)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 || result.TrashArchivedCount != 1 {
		t.Fatalf("unexpected apply result %#v", result)
	}

	remote := client.worklogsByIssue["AAPP-1"]
	if len(remote) != 2 {
		t.Fatalf("expected two remote worklogs after minimal diff apply, got %#v", remote)
	}
	foundKeep := false
	foundCreated := false
	for _, item := range remote {
		if item.ID == "w-keep" {
			foundKeep = true
		}
		if item.ID == "w-conflict" {
			t.Fatalf("expected conflicting Jira Data worklog to be deleted, got %#v", remote)
		}
		if item.ID == "created" && item.TimeSpentSeconds == 3600 && item.Comment == "Make me exact" {
			foundCreated = true
		}
	}
	if !foundKeep || !foundCreated {
		t.Fatalf("expected exact match to remain and one replacement to be created, got %#v", remote)
	}

	records := listTrashRecords(t, store)
	if len(records) != 1 || records[0].DurationSeconds != 1800 || records[0].Description != "Make me exact" {
		t.Fatalf("expected only conflicting Jira Data row to be archived, got %#v", records)
	}
}

func TestApplyPlanJiraCloudReplaceDeletesOnlyConflictingRemoteRows(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Keep me")
	seedWorklogRow(t, store, "row-2", "AAPP-1", "2026-05-01T10:00:00Z", 3600, "Make me exact")

	client := &fakeJiraCloudClient{
		user: jiracloud.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiracloud.Worklog{
			"AAPP-1": {
				{ID: "w-keep", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Keep me", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				{ID: "w-conflict", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "Make me exact", Author: jiracloud.WorklogUser{AccountID: "u1"}},
			},
		},
	}
	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient { return client }

	cfg := testJiraCloudConfig()
	plan, err := service.CreateJiraCloudPushPlan(context.Background(), cfg, "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].PlannedAction != "replace" {
		t.Fatalf("expected one replace item, got %#v", plan.Items)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 || result.TrashArchivedCount != 1 {
		t.Fatalf("unexpected apply result %#v", result)
	}

	remote := client.worklogsByIssue["AAPP-1"]
	if len(remote) != 2 {
		t.Fatalf("expected two remote worklogs after minimal diff apply, got %#v", remote)
	}
	foundKeep := false
	foundCreated := false
	for _, item := range remote {
		if item.ID == "w-keep" {
			foundKeep = true
		}
		if item.ID == "w-conflict" {
			t.Fatalf("expected conflicting Jira Cloud worklog to be deleted, got %#v", remote)
		}
		if item.ID == "created" && item.TimeSpentSeconds == 3600 && item.Comment == "Make me exact" {
			foundCreated = true
		}
	}
	if !foundKeep || !foundCreated {
		t.Fatalf("expected exact match to remain and one replacement to be created, got %#v", remote)
	}

	records := listTrashRecords(t, store)
	if len(records) != 1 || records[0].DurationSeconds != 1800 || records[0].Description != "Make me exact" {
		t.Fatalf("expected only conflicting Jira Cloud row to be archived, got %#v", records)
	}
}

func TestApplyPlanJiraCloudReportingReplaceDeletesOnlyConflictingRemoteRows(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Keep me")
	seedWorklogRow(t, store, "row-2", "AAPP-1", "2026-05-01T10:00:00Z", 3600, "Make me exact")

	client := &fakeJiraCloudClient{
		user: jiracloud.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiracloud.Worklog{
			"REPORT-1": {
				{ID: "w-keep", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Keep me", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				{ID: "w-conflict", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "AAPP-1 | Make me exact", Author: jiracloud.WorklogUser{AccountID: "u1"}},
			},
		},
	}
	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient { return client }

	cfg := testJiraCloudConfig()
	plan, err := service.CreateJiraCloudPushPlan(context.Background(), cfg, "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].PlannedAction != "replace" {
		t.Fatalf("expected one reporting replace item, got %#v", plan.Items)
	}

	result, err := service.ApplyPlan(cfg, plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 || result.TrashArchivedCount != 1 {
		t.Fatalf("unexpected apply result %#v", result)
	}

	remote := client.worklogsByIssue["REPORT-1"]
	if len(remote) != 2 {
		t.Fatalf("expected two reporting worklogs after minimal diff apply, got %#v", remote)
	}
	foundKeep := false
	foundCreated := false
	for _, item := range remote {
		if item.ID == "w-keep" {
			foundKeep = true
		}
		if item.ID == "w-conflict" {
			t.Fatalf("expected conflicting reporting worklog to be deleted, got %#v", remote)
		}
		if item.ID == "created" && item.TimeSpentSeconds == 3600 && item.Comment == "AAPP-1 | Make me exact" {
			foundCreated = true
		}
	}
	if !foundKeep || !foundCreated {
		t.Fatalf("expected reporting exact match to remain and one replacement to be created, got %#v", remote)
	}

	records := listTrashRecords(t, store)
	if len(records) != 1 || records[0].DurationSeconds != 1800 || records[0].Description != "AAPP-1 | Make me exact" {
		t.Fatalf("expected only conflicting reporting row to be archived, got %#v", records)
	}
}

func TestApplyPlanJiraCloudAutoReportingSharedTargetPreservesUnion(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "OPS-1", "2026-05-01T10:00:00Z", 1800, "Ops work")

	client := &fakeJiraCloudClient{
		user:            jiracloud.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiracloud.Worklog{"REPORT-1": {}},
	}
	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient { return client }

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{
		BaseURL: "https://example.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"reporting-a": {ReportingTargets: map[string]string{"AAPP": "REPORT-1"}},
				"reporting-b": {ReportingTargets: map[string]string{"OPS": "REPORT-1"}},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || len(result.Plan.Items) != 2 {
		t.Fatalf("expected two saved items for shared reporting target, got %#v", result)
	}

	applyResult, err := service.ApplyPlan(cfg, result.Plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if applyResult.AppliedCount != 2 || applyResult.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", applyResult)
	}

	remote := client.worklogsByIssue["REPORT-1"]
	if len(remote) != 2 {
		t.Fatalf("expected shared reporting target to keep both rows, got %#v", remote)
	}
	comments := map[string]struct{}{}
	for _, item := range remote {
		comment, _ := item.Comment.(string)
		comments[comment] = struct{}{}
	}
	if _, ok := comments["AAPP-1 | Build feature"]; !ok {
		t.Fatalf("expected AAPP reporting row, got %#v", remote)
	}
	if _, ok := comments["OPS-1 | Ops work"]; !ok {
		t.Fatalf("expected OPS reporting row, got %#v", remote)
	}
}

func TestApplyPlanJiraDataAutoReportingSharedTargetPreservesUnion(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "OPS-1", "2026-05-01T10:00:00Z", 1800, "Ops work")

	client := &fakeJiraDataClient{
		user:            jiradatacenter.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiradatacenter.Worklog{"REPORT-1": {}},
	}
	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient { return client }

	cfg := testJiraDataConfig()
	cfg.File.JiraData.Instances["internal"] = config.JiraDataCenterInstance{
		BaseURL: "https://jira.example.com",
		Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"reporting-a": {ReportingTargets: map[string]string{"AAPP": "REPORT-1"}},
				"reporting-b": {ReportingTargets: map[string]string{"OPS": "REPORT-1"}},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraDataTarget("internal"), jiraDataTarget("ops")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || len(result.Plan.Items) != 2 {
		t.Fatalf("expected two saved items for shared reporting target, got %#v", result)
	}

	applyResult, err := service.ApplyPlan(cfg, result.Plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if applyResult.AppliedCount != 2 || applyResult.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", applyResult)
	}

	remote := client.worklogsByIssue["REPORT-1"]
	if len(remote) != 2 {
		t.Fatalf("expected shared reporting target to keep both rows, got %#v", remote)
	}
	comments := map[string]struct{}{}
	for _, item := range remote {
		comment, _ := item.Comment.(string)
		comments[comment] = struct{}{}
	}
	if _, ok := comments["AAPP-1 | Build feature"]; !ok {
		t.Fatalf("expected AAPP reporting row, got %#v", remote)
	}
	if _, ok := comments["OPS-1 | Ops work"]; !ok {
		t.Fatalf("expected OPS reporting row, got %#v", remote)
	}
}

func TestReconcileMultiJiraDataAutoExcludesReportingTargetsFromDefaultRemoteOwnedDiscovery(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	client := &fakeJiraDataClient{
		user:         jiradatacenter.User{AccountID: "u1"},
		searchIssues: []jiradatacenter.IssueBrief{{Key: "ACIU-4403"}},
		worklogsByIssue: map[string][]jiradatacenter.Worklog{
			"ACIU-4403": {
				{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
			},
		},
	}
	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient { return client }

	cfg := testJiraDataConfig()
	cfg.File.JiraData.Instances["internal"] = config.JiraDataCenterInstance{
		BaseURL: "https://jira.example.com",
		Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"ACIU"}},
				"reporting": {ReportingTargets: map[string]string{"AAPP": "ACIU-4403"}},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraDataTarget("internal"), jiraDataTarget("ops")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan != nil {
		t.Fatalf("expected no saved plan for exact reporting match, got %#v", result.Plan.Items)
	}
	if result.NoPlan == nil || result.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("expected aggregate no-plan result, got %#v", result)
	}
	for _, summary := range result.ProfileSummaries {
		if summary.RouteProfile == "default" && summary.ActionableScopeCount != 0 {
			t.Fatalf("default profile should not treat reporting target as actionable, got %#v", summary)
		}
	}
}

func TestReconcileMultiJiraCloudAutoExcludesReportingTargetsFromDefaultRemoteOwnedDiscovery(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	client := &fakeJiraCloudClient{
		user:         jiracloud.User{AccountID: "u1"},
		searchIssues: []jiracloud.IssueBrief{{Key: "ACIU-4403"}},
		worklogsByIssue: map[string][]jiracloud.Worklog{
			"ACIU-4403": {
				{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}},
			},
		},
	}
	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient { return client }

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{
		BaseURL: "https://example.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"ACIU"}},
				"reporting": {ReportingTargets: map[string]string{"AAPP": "ACIU-4403"}},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product"), jiraCloudTarget("ops")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan != nil {
		t.Fatalf("expected no saved plan for exact reporting match, got %#v", result.Plan.Items)
	}
	if result.NoPlan == nil || result.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("expected aggregate no-plan result, got %#v", result)
	}
	for _, summary := range result.ProfileSummaries {
		if summary.RouteProfile == "default" && summary.ActionableScopeCount != 0 {
			t.Fatalf("default profile should not treat reporting target as actionable, got %#v", summary)
		}
	}
}

func TestReconcileMultiJiraDataAutoReportingDoesNotLoopAfterApply(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	client := &fakeJiraDataClient{
		user:            jiradatacenter.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiradatacenter.Worklog{"ACIU-4403": {}},
	}
	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient { return client }

	cfg := testJiraDataConfig()
	cfg.File.JiraData.Instances["internal"] = config.JiraDataCenterInstance{
		BaseURL: "https://jira.example.com",
		Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"ACIU"}},
				"reporting": {ReportingTargets: map[string]string{"AAPP": "ACIU-4403"}},
			},
		},
	}

	first, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraDataTarget("internal")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("first ReconcileMultiPushPlan failed: %v", err)
	}
	if first.Plan == nil || len(first.Plan.Items) != 1 || first.Plan.Items[0].RouteProfile != "reporting" || first.Plan.Items[0].PlannedAction != "create" {
		t.Fatalf("expected first reconcile to create reporting row only, got %#v", first)
	}
	if _, err := service.ApplyPlan(cfg, first.Plan.ID); err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}

	client.searchIssues = []jiradatacenter.IssueBrief{{Key: "ACIU-4403"}}
	second, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraDataTarget("internal")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("second ReconcileMultiPushPlan failed: %v", err)
	}
	if second.Plan != nil {
		t.Fatalf("expected no follow-up cleanup plan, got %#v", second.Plan.Items)
	}
	if second.NoPlan == nil || second.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("expected second reconcile to be non-actionable, got %#v", second)
	}
}

func TestFinalizeSuccessfulPushGroupAttributesTrashToMatchingScope(t *testing.T) {
	items := []PlanItem{
		{
			ID:            "item-ops",
			PlanDirection: "push",
			PlannedAction: "replace",
			IssueKey:      "REPORT-1",
			TargetIssue:   "REPORT-1",
			InspectionSummary: InspectionSummary{
				SourceIssueKeys: []string{"OPS-1"},
			},
			Payload: []model.Row{{
				IssueKey:        "REPORT-1",
				StartedAtUTC:    mustTime("2026-05-01T10:00:00Z"),
				DurationSeconds: 1800,
				Description:     "OPS-1 | Ops work",
			}},
		},
		{
			ID:            "item-aapp",
			PlanDirection: "push",
			PlannedAction: "replace",
			IssueKey:      "REPORT-1",
			TargetIssue:   "REPORT-1",
			InspectionSummary: InspectionSummary{
				SourceIssueKeys: []string{"AAPP-1"},
			},
			Payload: []model.Row{{
				IssueKey:        "REPORT-1",
				StartedAtUTC:    mustTime("2026-05-01T08:00:00Z"),
				DurationSeconds: 3600,
				Description:     "AAPP-1 | Build feature",
			}},
		},
	}

	outcomes := finalizeSuccessfulPushGroup(items, "jira-cloud", pushApplyResult{
		trashArchivedCount: 1,
		deletedRows: []model.Row{{
			IssueKey:        "REPORT-1",
			StartedAtUTC:    mustTime("2026-05-01T08:00:00Z"),
			DurationSeconds: 1800,
			Description:     "AAPP-1 | Old feature",
		}},
	})

	if len(outcomes) != 2 {
		t.Fatalf("expected two outcomes, got %#v", outcomes)
	}

	outcomesByID := map[string]pushExecutionOutcome{}
	for _, outcome := range outcomes {
		outcomesByID[outcome.item.ID] = outcome
	}

	if outcomesByID["item-ops"].trashArchivedCount != 0 {
		t.Fatalf("expected item-ops to have no trash attribution, got %#v", outcomesByID["item-ops"])
	}
	if outcomesByID["item-aapp"].trashArchivedCount != 1 {
		t.Fatalf("expected item-aapp to receive the grouped trash attribution, got %#v", outcomesByID["item-aapp"])
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

func TestApplyPlanClockifyPreflightFailureDoesNotCreatePendingAttempt(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Create me")

	client := &fakeClockifyClient{
		projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
		tagsByID: map[string]clockify.Tag{
			"tag-a": {ID: "tag-a", Name: "AAPP-1"},
		},
	}
	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient { return client }

	cfg := testClockifyConfig(false)
	plan, err := service.CreateClockifyPushPlan(context.Background(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateClockifyPushPlan failed: %v", err)
	}
	client.tagsByID = map[string]clockify.Tag{}

	if _, err := service.ApplyPlan(cfg, plan.ID); err == nil || err.Error() != `issue tag "AAPP-1" is missing and config forbids creating it` {
		t.Fatalf("expected clockify preflight failure, got %v", err)
	}

	var attempts int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM delivery_attempts`).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("expected no delivery attempts after preflight failure, got %d", attempts)
	}

	loaded, err := service.LoadPlan(plan.ID)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if len(loaded.Items) != 1 || loaded.Items[0].ExecutionState != "not_attempted" {
		t.Fatalf("expected plan item to remain not_attempted, got %#v", loaded.Items)
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

func TestRetryPlanFailedSharedPushScopeUsesFullGroupReconcileContext(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "BAPP-1", "2026-05-01T10:00:00Z", 1800, "Review PR")

	cfg := config.EffectiveConfig{
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
								"reporting-a": {ReportingTargets: map[string]string{"AAPP": "REPORT-1"}},
								"reporting-b": {ReportingTargets: map[string]string{"BAPP": "REPORT-1"}},
							},
						},
					},
				},
			},
		},
	}

	client := &fakeJiraCloudClient{
		user:            jiracloud.User{AccountID: "u1"},
		worklogsByIssue: map[string][]jiracloud.Worklog{"REPORT-1": {}},
	}
	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient { return client }

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product"), jiraCloudTarget("ops")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || len(result.Plan.Items) != 2 {
		t.Fatalf("expected two saved reporting items, got %#v", result.Plan)
	}

	itemsByProfile := map[string]PlanItem{}
	for _, item := range result.Plan.Items {
		itemsByProfile[item.RouteProfile] = item
	}
	itemA := itemsByProfile["reporting-a"]
	itemB := itemsByProfile["reporting-b"]
	if itemA.ID == "" || itemB.ID == "" {
		t.Fatalf("expected reporting items by profile, got %#v", result.Plan.Items)
	}

	client.worklogsByIssue["REPORT-1"] = []jiracloud.Worklog{
		{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}},
		{ID: "w2", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "BAPP-1 | Review PR", Author: jiracloud.WorklogUser{AccountID: "u1"}},
	}

	seedDeliveryAttempt(t, store, result.Plan.ID, itemA.ID, "succeeded", "done", "2026-05-02T10:00:00Z")
	seedDeliveryAttempt(t, store, result.Plan.ID, itemB.ID, "failed", "cleanup failed", "2026-05-02T10:00:00Z")

	retryResult, err := service.RetryPlan(cfg, result.Plan.ID, "failed")
	if err != nil {
		t.Fatalf("RetryPlan failed: %v", err)
	}
	if retryResult.AppliedCount != 1 || retryResult.FailedCount != 0 || retryResult.SkippedCount != 1 {
		t.Fatalf("unexpected retry result %#v", retryResult)
	}

	loaded, err := service.LoadPlan(result.Plan.ID)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	statesByProfile := map[string]string{}
	for _, item := range loaded.Items {
		statesByProfile[item.RouteProfile] = item.ExecutionState
	}
	if statesByProfile["reporting-a"] != "succeeded" || statesByProfile["reporting-b"] != "succeeded" {
		t.Fatalf("expected both grouped scopes to be succeeded after retry, got %#v", statesByProfile)
	}

	comments := map[string]struct{}{}
	for _, item := range client.worklogsByIssue["REPORT-1"] {
		comment, ok := item.Comment.(string)
		if !ok {
			t.Fatalf("expected string comment, got %#v", item.Comment)
		}
		comments[comment] = struct{}{}
	}
	if len(comments) != 2 {
		t.Fatalf("expected grouped retry to preserve both remote rows, got %#v", client.worklogsByIssue["REPORT-1"])
	}
	if _, ok := comments["AAPP-1 | Build feature"]; !ok {
		t.Fatalf("expected AAPP reporting row to remain, got %#v", client.worklogsByIssue["REPORT-1"])
	}
	if _, ok := comments["BAPP-1 | Review PR"]; !ok {
		t.Fatalf("expected BAPP reporting row to remain, got %#v", client.worklogsByIssue["REPORT-1"])
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
	if pushPlan.Items[0].PlanStatus != "ready" || pushPlan.Items[0].PlannedAction != "create" {
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

func TestCreateJiraDataPullPlanExcludesIssuesOutsideRoutedPrefixes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}, {Key: "IDVSTL-5"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"AAPP-1":   {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}}},
				"IDVSTL-5": {{ID: "w2", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "foreign", Author: jiradatacenter.WorklogUser{AccountID: "u1"}}},
			},
		}
	}

	plan, err := service.CreateJiraDataPullPlan(context.Background(), testJiraDataConfig(), "internal", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraDataPullPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].IssueKey != "AAPP-1" {
		t.Fatalf("expected foreign prefix to be excluded, got %#v", plan.Items)
	}
}

func TestCreateJiraDataPushPlanReportingRemoteOwnedGroupCreatesDeleteItem(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | remote", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one reporting cleanup item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "delete")
	if item.LocalRowCount != 0 || item.InspectionSummary.LocalTotalSeconds != 0 || item.RemoteRowCount != 1 {
		t.Fatalf("expected remote-owned Jira cleanup scope to report zero local metrics, got %#v", item)
	}
}

func TestCreateJiraDataPushPlanReportingCleanupUsesDeletableRemoteRowsForMetrics(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one reporting cleanup item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "delete")
	if item.ComparisonStatus != "remote_present" || item.ReasonCode != "remote_present" {
		t.Fatalf("expected remote_present cleanup classification, got %#v", item)
	}
	if item.RemoteRowCount != 1 || item.RemoteTotal != 3600 || item.InspectionSummary.DeleteRowCount != 1 {
		t.Fatalf("expected deletable remote rows to drive metrics, got %#v", item)
	}
}

func TestCreateJiraDataPushPlanReportingCleanupIgnoresForeignRowsWithEmptyAccountIDs(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{Name: "ernestas@ito.lt", Key: "JIRAUSER21702"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "foreign", Author: jiradatacenter.WorklogUser{Name: "vilius@ito.lt", Key: "JIRAUSER27201"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one reporting item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	if item.PlanStatus != "skipped" || item.PlannedAction != "none" || item.ReasonCode != "exact_match" {
		t.Fatalf("expected foreign-only reporting scope to match empty local state, got %#v", item)
	}
	if item.RemoteRowCount != 0 || item.InspectionSummary.ForeignAuthorPresent != true {
		t.Fatalf("expected no owned remote metrics and foreign marker, got %#v", item)
	}
}

func TestApplyPlanJiraDataRemoteOwnedCleanupArchivesTrash(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user:         jiradatacenter.User{AccountID: "u1"},
			searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | remote", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}

	result, err := service.ApplyPlan(testJiraDataConfig(), plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", result)
	}
	if result.TrashArchivedCount != 1 {
		t.Fatalf("expected one archived trash row, got %#v", result)
	}
	if got := countTrashRecords(t, store); got != 1 {
		t.Fatalf("expected one remote trash row, got %d", got)
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

func TestCreateJiraCloudPullPlanExcludesIssuesOutsideRoutedPrefixes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:         jiracloud.User{AccountID: "u1"},
			searchIssues: []jiracloud.IssueBrief{{Key: "AAPP-1"}, {Key: "IDVSTL-5"}},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"AAPP-1":   {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}}},
				"IDVSTL-5": {{ID: "w2", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "foreign", Author: jiracloud.WorklogUser{AccountID: "u1"}}},
			},
		}
	}

	plan, err := service.CreateJiraCloudPullPlan(context.Background(), testJiraCloudConfig(), "product", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
	if err != nil {
		t.Fatalf("CreateJiraCloudPullPlan failed: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].IssueKey != "AAPP-1" {
		t.Fatalf("expected foreign prefix to be excluded, got %#v", plan.Items)
	}
}

func TestCreateJiraCloudPushPlanReportingRemoteOwnedGroupCreatesDeleteItem(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | remote", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one reporting cleanup item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "delete")
	if item.LocalRowCount != 0 || item.InspectionSummary.LocalRowCount != 0 || item.RemoteRowCount != 1 {
		t.Fatalf("expected remote-owned Jira Cloud cleanup scope to report zero local metrics, got %#v", item)
	}
}

func TestCreateJiraCloudPushPlanReportingCleanupUsesDeletableRemoteRowsForMetrics(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one reporting cleanup item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "delete")
	if item.ComparisonStatus != "remote_present" || item.ReasonCode != "remote_present" {
		t.Fatalf("expected remote_present cleanup classification, got %#v", item)
	}
	if item.RemoteRowCount != 1 || item.RemoteTotal != 3600 || item.InspectionSummary.DeleteRowCount != 1 {
		t.Fatalf("expected deletable remote rows to drive metrics, got %#v", item)
	}
}

func TestCreateJiraCloudPushPlanReportingCleanupIgnoresForeignRowsWithEmptyAccountIDs(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{Name: "ernestas@ito.lt", Key: "JIRAUSER21702"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "foreign", Author: jiracloud.WorklogUser{Name: "vilius@ito.lt", Key: "JIRAUSER27201"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one reporting item, got %d", len(plan.Items))
	}
	item := plan.Items[0]
	if item.PlanStatus != "skipped" || item.PlannedAction != "none" || item.ReasonCode != "exact_match" {
		t.Fatalf("expected foreign-only reporting scope to match empty local state, got %#v", item)
	}
	if item.RemoteRowCount != 0 || item.InspectionSummary.ForeignAuthorPresent != true {
		t.Fatalf("expected no owned remote metrics and foreign marker, got %#v", item)
	}
}

func TestCreateJiraCloudPushPlanRequiresRouting(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	_, err := service.CreateJiraCloudPushPlan(context.Background(), config.EffectiveConfig{
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

func TestApplyPlanJiraCloudRemoteOwnedCleanupArchivesTrash(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | remote", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}

	result, err := service.ApplyPlan(testJiraCloudConfig(), plan.ID)
	if err != nil {
		t.Fatalf("ApplyPlan failed: %v", err)
	}
	if result.AppliedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("unexpected apply result %#v", result)
	}
	if result.TrashArchivedCount != 1 {
		t.Fatalf("expected one archived trash row, got %#v", result)
	}
	if got := countTrashRecords(t, store); got != 1 {
		t.Fatalf("expected one remote trash row, got %d", got)
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

func TestReconcileJiraDataPushPlanReturnsExactMatchNoPlan(t *testing.T) {
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
	if result.Plan != nil || result.NoPlan == nil || result.NoPlan.Reason != "exact_match" {
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
	if second.Plan != nil || second.NoPlan == nil || second.NoPlan.Reason != "exact_match" {
		t.Fatalf("expected exact_match no-plan, got %#v", second)
	}
	if second.NoPlan.MatchedScopeCount != 1 || second.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("unexpected second no-plan summary %#v", second.NoPlan)
	}
}

func TestCreateJiraDataPushPlanTreatsZeroNormalizedRemoteRowsAsMissing(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user: jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraDataPushPlan(context.Background(), testJiraDataConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraDataPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one item, got %#v", plan.Items)
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "create")
	if item.ComparisonStatus != "remote_missing" || item.ReasonCode != "remote_missing" {
		t.Fatalf("expected remote_missing classification, got %#v", item)
	}
	if item.RemoteRowCount != 0 || item.RemoteTotal != 0 {
		t.Fatalf("expected zero normalized remote rows, got %#v", item)
	}
}

func TestReconcileJiraCloudPushPlanReturnsBlockedPlanWhenRoutesDoNotMatch(t *testing.T) {
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
	if result.Plan == nil || result.NoPlan != nil {
		t.Fatalf("unexpected reconcile result %#v", result)
	}
	if result.Plan.AggregateStatus != "blocked" || len(result.Plan.Items) != 2 {
		t.Fatalf("expected blocked reporting plan with preserved target scope, got %#v", result.Plan)
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
	if second.Plan != nil || second.NoPlan == nil || second.NoPlan.Reason != "exact_match" {
		t.Fatalf("expected exact_match no-plan, got %#v", second)
	}
	if second.NoPlan.MatchedScopeCount != 1 || second.NoPlan.ActionableScopeCount != 0 {
		t.Fatalf("unexpected second no-plan summary %#v", second.NoPlan)
	}
}

func TestCreateJiraCloudPushPlanTreatsZeroNormalizedRemoteRowsAsMissing(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"REPORT-1": {
					{ID: "w1", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Comment: "", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	plan, err := service.CreateJiraCloudPushPlan(context.Background(), testJiraCloudConfig(), "reporting", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatalf("CreateJiraCloudPushPlan failed: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("expected one item, got %#v", plan.Items)
	}
	item := plan.Items[0]
	assertPlanItem(t, item, "ready", "create")
	if item.ComparisonStatus != "remote_missing" || item.ReasonCode != "remote_missing" {
		t.Fatalf("expected remote_missing classification, got %#v", item)
	}
	if item.RemoteRowCount != 0 || item.RemoteTotal != 0 {
		t.Fatalf("expected zero normalized remote rows, got %#v", item)
	}
}

func TestReconcileJiraCloudPushPlanPersistsReportingPlanWithForeignRows(t *testing.T) {
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
	if len(result.Plan.Items) != 1 || result.Plan.Items[0].PlanStatus != "ready" || result.Plan.Items[0].PlannedAction != "create" {
		t.Fatalf("unexpected plan items %#v", result.Plan.Items)
	}
	if !result.Plan.Items[0].InspectionSummary.ForeignAuthorPresent {
		t.Fatalf("expected foreign author marker in inspection summary")
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
		targetScope(jiraCloudTarget("maxima_lt_jira"), jiraDataTarget("ito_jira")),
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
	if len(result.Plan.Items) != 2 {
		t.Fatalf("expected two plan items, got %#v", result.Plan.Items)
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
	if len(result.ProfileSummaries) != 2 {
		t.Fatalf("expected two profile summaries, got %#v", result.ProfileSummaries)
	}
	if got := strings.Join(result.ProfileSummaries[0].ResolvedTargetInstances, ","); got != "maxima_lt_jira" {
		t.Fatalf("unexpected jira-cloud summary target instances %q", got)
	}
	if got := strings.Join(result.ProfileSummaries[1].ResolvedTargetInstances, ","); got != "ito_jira" {
		t.Fatalf("unexpected jira-data-center summary target instances %q", got)
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

func TestReconcileMultiPushPlanAutoIncludesReportingProfiles(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-1": {}, "REPORT-1": {}},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		testJiraCloudConfig(),
		targetScope(jiraCloudTarget("product"), jiraCloudTarget("ops")),
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
	if len(result.Plan.Items) != 2 {
		t.Fatalf("expected default and reporting items, got %#v", result.Plan.Items)
	}
	if len(result.ProfileSummaries) != 2 {
		t.Fatalf("expected two profile summaries, got %#v", result.ProfileSummaries)
	}
	if result.ProfileSummaries[0].RouteProfile != "default" || result.ProfileSummaries[1].RouteProfile != "reporting" {
		t.Fatalf("unexpected profile order %#v", result.ProfileSummaries)
	}
	if result.ProfileSummaries[0].PlanCreated != true || result.ProfileSummaries[1].PlanCreated != true {
		t.Fatalf("expected plan-created summaries, got %#v", result.ProfileSummaries)
	}
	if got := strings.Join(result.ProfileSummaries[0].ResolvedTargetInstances, ","); got != "product" {
		t.Fatalf("unexpected default summary target instances %q", got)
	}
	if got := strings.Join(result.ProfileSummaries[1].ResolvedTargetInstances, ","); got != "product" {
		t.Fatalf("unexpected reporting summary target instances %q", got)
	}
	if result.Plan.Items[0].RouteProfile != "default" || result.Plan.Items[1].RouteProfile != "reporting" {
		t.Fatalf("expected persisted route profiles, got %#v", result.Plan.Items)
	}
}

func TestReconcileMultiPushPlanAutoPersistsExactMatchReportingProfilesJiraCloud(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"AAPP-1": {
					{ID: "w-default", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
				"REPORT-1": {
					{ID: "w-reporting", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiracloud.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		testJiraCloudConfig(),
		targetScope(jiraCloudTarget("product")),
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
	if len(result.Plan.Items) != 2 {
		t.Fatalf("expected default and reporting exact-match items, got %#v", result.Plan.Items)
	}
	for _, item := range result.Plan.Items {
		if item.PlanStatus != "skipped" || item.ReasonCode != "exact_match" {
			t.Fatalf("expected exact-match skipped item, got %#v", item)
		}
	}
	if result.ProfileSummaries[1].RouteProfile != "reporting" || !result.ProfileSummaries[1].PlanCreated || result.ProfileSummaries[1].Reason != "exact_match" {
		t.Fatalf("expected persisted exact-match reporting summary, got %#v", result.ProfileSummaries)
	}
}

func TestReconcileMultiPushPlanAutoPersistsExactMatchReportingProfilesJiraData(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user: jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"AAPP-1": {
					{ID: "w-default", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
				"REPORT-1": {
					{ID: "w-reporting", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "AAPP-1 | Build feature", Author: jiradatacenter.WorklogUser{AccountID: "u1"}},
				},
			},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		testJiraDataConfig(),
		targetScope(jiraDataTarget("internal"), jiraDataTarget("ops")),
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
	if len(result.Plan.Items) != 2 {
		t.Fatalf("expected default and reporting exact-match items, got %#v", result.Plan.Items)
	}
	for _, item := range result.Plan.Items {
		if item.PlanStatus != "skipped" || item.ReasonCode != "exact_match" {
			t.Fatalf("expected exact-match skipped item, got %#v", item)
		}
	}
	if result.ProfileSummaries[1].RouteProfile != "reporting" || !result.ProfileSummaries[1].PlanCreated || result.ProfileSummaries[1].Reason != "exact_match" {
		t.Fatalf("expected persisted exact-match reporting summary, got %#v", result.ProfileSummaries)
	}
}

func TestReconcileMultiPushPlanAutoIgnoresSameNamedNonReportingProfileJiraCloud(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "OPS-1", "2026-05-01T10:00:00Z", 1800, "Ops work")

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["ops"] = config.JiraCloudInstance{
		BaseURL: "https://ops.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t2"},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"OPS"}},
				"reporting": {IssuePrefixes: []string{"OPS"}},
			},
		},
	}

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"AAPP-1":   {},
				"OPS-1":    {},
				"REPORT-1": {},
			},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product"), jiraCloudTarget("ops")),
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
	if len(result.Plan.Items) != 3 {
		t.Fatalf("expected product default, product reporting, and ops default items, got %#v", result.Plan.Items)
	}

	reportingItems := 0
	for _, item := range result.Plan.Items {
		if item.RouteProfile != "reporting" {
			continue
		}
		reportingItems++
		if item.TargetAdapterInstance != "product" || item.TargetIssue != "REPORT-1" {
			t.Fatalf("unexpected auto reporting item %#v", item)
		}
	}
	if reportingItems != 1 {
		t.Fatalf("expected one reporting item, got %#v", result.Plan.Items)
	}
}

func TestReconcileMultiPushPlanAutoIgnoresSameNamedNonReportingProfileJiraData(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "OPS-1", "2026-05-01T10:00:00Z", 1800, "Ops work")

	cfg := testJiraDataConfig()
	cfg.File.JiraData.Instances["ops"] = config.JiraDataCenterInstance{
		BaseURL: "https://jira.ops.example.com",
		Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t2"}},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"OPS"}},
				"reporting": {IssuePrefixes: []string{"OPS"}},
			},
		},
	}

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataClient{
			user: jiradatacenter.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiradatacenter.Worklog{
				"AAPP-1":   {},
				"OPS-1":    {},
				"REPORT-1": {},
			},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraDataTarget("internal"), jiraDataTarget("ops")),
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
	if len(result.Plan.Items) != 3 {
		t.Fatalf("expected internal default, internal reporting, and ops default items, got %#v", result.Plan.Items)
	}

	reportingItems := 0
	for _, item := range result.Plan.Items {
		if item.RouteProfile != "reporting" {
			continue
		}
		reportingItems++
		if item.TargetAdapterInstance != "internal" || item.TargetIssue != "REPORT-1" {
			t.Fatalf("unexpected auto reporting item %#v", item)
		}
	}
	if reportingItems != 1 {
		t.Fatalf("expected one reporting item, got %#v", result.Plan.Items)
	}
}

func TestReconcileMultiPushPlanExplicitRouteProfileKeepsNameScopedAcrossInstances(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")
	seedWorklogRow(t, store, "row-2", "OPS-1", "2026-05-01T10:00:00Z", 1800, "Ops work")

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["ops"] = config.JiraCloudInstance{
		BaseURL: "https://ops.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t2"},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"OPS"}},
				"reporting": {IssuePrefixes: []string{"OPS"}},
			},
		},
	}

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user: jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{
				"OPS-1":    {},
				"REPORT-1": {},
			},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product"), jiraCloudTarget("ops")),
		"reporting",
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
	if len(result.Plan.Items) != 2 {
		t.Fatalf("expected both selected reporting-name routes, got %#v", result.Plan.Items)
	}

	targets := map[string]string{}
	for _, item := range result.Plan.Items {
		if item.RouteProfile != "reporting" {
			t.Fatalf("expected explicit route profile to persist, got %#v", item)
		}
		targets[item.TargetAdapterInstance] = item.TargetIssue
	}
	if targets["product"] != "REPORT-1" || targets["ops"] != "OPS-1" {
		t.Fatalf("unexpected explicit route-profile targets %#v", targets)
	}
}

func TestReconcileMultiPushPlanExplicitRouteProfileKeepsMissingRoutesAcrossInstances(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "ZAPP-1", "2026-05-01T08:00:00Z", 3600, "Unrouted work")

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["ops"] = config.JiraCloudInstance{
		BaseURL: "https://ops.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t2"},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default": {IssuePrefixes: []string{"OPS"}},
			},
		},
	}

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product"), jiraCloudTarget("ops")),
		"default",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || result.NoPlan != nil {
		t.Fatalf("expected saved blocked plan, got %#v", result)
	}
	if len(result.Plan.Items) != 1 {
		t.Fatalf("expected one missing-route item, got %#v", result.Plan.Items)
	}
	item := result.Plan.Items[0]
	if item.IssueKey != "ZAPP-1" || item.PlanStatus != "blocked" || item.ReasonCode != "missing_route" {
		t.Fatalf("expected explicit profile to preserve missing route, got %#v", item)
	}
}

func TestReconcileMultiPushPlanRejectsAmbiguousAutomaticReportingProfiles(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	cfg := testJiraCloudConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{
		BaseURL: "https://example.atlassian.net",
		Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
		Routing: &config.JiraInstanceRoutes{
			Profiles: map[string]config.JiraRouteProfile{
				"default":     {IssuePrefixes: []string{"AAPP"}},
				"reporting-a": {ReportingTargets: map[string]string{"AAPP": "REPORT-1"}},
				"reporting-b": {ReportingTargets: map[string]string{"AAPP": "REPORT-2"}},
			},
		},
	}

	service := NewService(store)
	_, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("product")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err == nil {
		t.Fatal("expected ambiguous automatic reporting error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--route-profile <name>") {
		t.Fatalf("expected rerun guidance, got %v", err)
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
		targetScope(clockifyTarget()),
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
		targetScope(jiraCloudTarget("maxima_lt_jira")),
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

func TestReconcileMultiPushPlanIncludesClockifySummary(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient {
		return &fakeClockifyClient{
			projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
			tagsByID: map[string]clockify.Tag{},
		}
	}
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			user:            jiracloud.User{AccountID: "u1"},
			worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-1": {}},
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             filepath.Join(t.TempDir(), "worklogs.db"),
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
		targetScope(clockifyTarget(), jiraCloudTarget("maxima_lt_jira")),
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
	if len(result.ProfileSummaries) != 2 {
		t.Fatalf("expected two profile summaries, got %#v", result.ProfileSummaries)
	}
	clockifySummary := result.ProfileSummaries[0]
	if clockifySummary.AdapterFamily != "clockify" || clockifySummary.RouteProfile != "" {
		t.Fatalf("unexpected clockify summary %#v", clockifySummary)
	}
	if got := strings.Join(clockifySummary.ResolvedTargetInstances, ","); got != config.ClockifyInstanceName {
		t.Fatalf("unexpected clockify summary target instances %q", got)
	}
	if clockifySummary.ScopeCount != 1 || clockifySummary.ActionableScopeCount != 1 || !clockifySummary.PlanCreated {
		t.Fatalf("unexpected clockify summary counts %#v", clockifySummary)
	}
	if clockifySummary.Reason != "" {
		t.Fatalf("expected actionable clockify summary reason to stay empty, got %#v", clockifySummary)
	}
	jiraSummary := result.ProfileSummaries[1]
	if jiraSummary.AdapterFamily != "jira-cloud" {
		t.Fatalf("unexpected jira summary %#v", jiraSummary)
	}
	if jiraSummary.Reason != "" {
		t.Fatalf("expected actionable jira summary reason to stay empty, got %#v", jiraSummary)
	}
}

func TestReconcileMultiPushPlanPersistsPartialPlanWhenJiraCloudSearchFails(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient {
		return &fakeClockifyClient{
			projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
			tagsByID: map[string]clockify.Tag{},
		}
	}
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudClient{
			searchIssuesErr: &jiracloud.RequestError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"},
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             filepath.Join(t.TempDir(), "worklogs.db"),
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			Clockify: testClockifyConfig(true).File.Clockify,
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"product": {
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
		targetScope(clockifyTarget(), jiraCloudTarget("product")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.Plan == nil || result.NoPlan != nil {
		t.Fatalf("expected saved partial plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "check_failed" {
		t.Fatalf("expected check_failed aggregate status, got %#v", result.Plan)
	}

	foundClockify := false
	foundJiraFailure := false
	for _, item := range result.Plan.Items {
		switch item.TargetAdapterFamily {
		case "clockify":
			foundClockify = true
			if item.PlanStatus != "ready" {
				t.Fatalf("expected ready clockify item, got %#v", item)
			}
		case "jira-cloud":
			foundJiraFailure = true
			if item.PlanStatus != "check_failed" || item.ReasonCode != "auth_error" || item.TargetAdapterInstance != "product" {
				t.Fatalf("unexpected jira-cloud failed item %#v", item)
			}
		}
	}
	if !foundClockify || !foundJiraFailure {
		t.Fatalf("expected both clockify and jira-cloud items, got %#v", result.Plan.Items)
	}
	if len(result.ProfileSummaries) != 2 {
		t.Fatalf("expected two profile summaries, got %#v", result.ProfileSummaries)
	}
	jiraSummary := result.ProfileSummaries[1]
	if jiraSummary.AdapterFamily != "jira-cloud" || jiraSummary.ActionableScopeCount != 0 || jiraSummary.Reason != "auth_error" {
		t.Fatalf("expected jira-cloud auth failure summary, got %#v", jiraSummary)
	}
}

func TestSummarizePlanProfileReportsMixedCheckFailedReasons(t *testing.T) {
	plan := Plan{
		TargetInstances: []string{"product"},
		Items: []PlanItem{
			{PlanStatus: "check_failed", ReasonCode: "auth_error"},
			{PlanStatus: "check_failed", ReasonCode: "remote_error"},
			{PlanStatus: "skipped", ReasonCode: "exact_match"},
		},
	}

	summary := summarizePlanProfile("jira-cloud", "default", plan)
	if summary.ActionableScopeCount != 0 || !summary.PlanCreated || summary.Reason != "mixed" {
		t.Fatalf("expected mixed check-failed summary, got %#v", summary)
	}
}

func TestReconcileMultiPushPlanAutoSkipsUnreachableJiraCloudInstanceWithoutMatchingRoutes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		switch cfg.BaseURL {
		case "https://lt.example.atlassian.net":
			return &fakeJiraCloudClient{
				user:            jiracloud.User{AccountID: "u1"},
				worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-1": {}},
			}
		case "https://ee.example.atlassian.net":
			return &fakeJiraCloudClient{
				currentUserErr: errors.New("dial tcp: no route to host"),
			}
		default:
			t.Fatalf("unexpected base url %s", cfg.BaseURL)
			return nil
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             filepath.Join(t.TempDir(), "worklogs.db"),
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"maxima_lt_jira": {
						BaseURL: "https://lt.example.atlassian.net",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t1"},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"AAPP"}},
						}},
					},
					"maxima_ee_jira": {
						BaseURL: "https://ee.example.atlassian.net",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t2"},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"EAPP"}},
						}},
					},
				},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraCloudTarget("maxima_lt_jira"), jiraCloudTarget("maxima_ee_jira")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved jira-cloud plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "ready" || len(result.Plan.Items) != 1 {
		t.Fatalf("expected only matched reachable scope, got %#v", result.Plan)
	}
	if len(result.ProfileSummaries) != 2 {
		t.Fatalf("expected two profile summaries, got %#v", result.ProfileSummaries)
	}
	if result.ProfileSummaries[0].ResolvedTargetInstances[0] != "maxima_ee_jira" || result.ProfileSummaries[0].ScopeCount != 0 || result.ProfileSummaries[0].PlanCreated || result.ProfileSummaries[0].Reason != "no_matching_routes" {
		t.Fatalf("unexpected ee summary %#v", result.ProfileSummaries[0])
	}
	if result.ProfileSummaries[1].ResolvedTargetInstances[0] != "maxima_lt_jira" || result.ProfileSummaries[1].ScopeCount != 1 || !result.ProfileSummaries[1].PlanCreated {
		t.Fatalf("unexpected lt summary %#v", result.ProfileSummaries[1])
	}
}

func TestReconcileMultiPushPlanAutoSkipsUnreachableJiraDataInstanceWithoutMatchingRoutes(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row-1", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Build feature")

	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		switch cfg.BaseURL {
		case "https://lt.example.com":
			return &fakeJiraDataClient{
				user:            jiradatacenter.User{AccountID: "u1"},
				worklogsByIssue: map[string][]jiradatacenter.Worklog{"AAPP-1": {}},
			}
		case "https://ee.example.com":
			return &fakeJiraDataClient{
				currentUserErr: errors.New("dial tcp: no route to host"),
			}
		default:
			t.Fatalf("unexpected base url %s", cfg.BaseURL)
			return nil
		}
	}

	cfg := config.EffectiveConfig{
		SQLitePath:             filepath.Join(t.TempDir(), "worklogs.db"),
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"ito_jira": {
						BaseURL: "https://lt.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t1"}},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"AAPP"}},
						}},
					},
					"vpn_only_jira": {
						BaseURL: "https://ee.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t2"}},
						Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
							"default": {IssuePrefixes: []string{"EAPP"}},
						}},
					},
				},
			},
		},
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		cfg,
		targetScope(jiraDataTarget("ito_jira"), jiraDataTarget("vpn_only_jira")),
		"",
		mustTime("2026-05-01T00:00:00Z"),
		mustTime("2026-05-01T23:59:59Z"),
		false,
	)
	if err != nil {
		t.Fatalf("ReconcileMultiPushPlan failed: %v", err)
	}
	if result.NoPlan != nil || result.Plan == nil {
		t.Fatalf("expected saved jira-data-center plan, got %#v", result)
	}
	if result.Plan.AggregateStatus != "ready" || len(result.Plan.Items) != 1 {
		t.Fatalf("expected only matched reachable scope, got %#v", result.Plan)
	}
	if len(result.ProfileSummaries) != 2 {
		t.Fatalf("expected two profile summaries, got %#v", result.ProfileSummaries)
	}
	if result.ProfileSummaries[0].ResolvedTargetInstances[0] != "ito_jira" || result.ProfileSummaries[0].ScopeCount != 1 || !result.ProfileSummaries[0].PlanCreated {
		t.Fatalf("unexpected reachable summary %#v", result.ProfileSummaries[0])
	}
	if result.ProfileSummaries[1].ResolvedTargetInstances[0] != "vpn_only_jira" || result.ProfileSummaries[1].ScopeCount != 0 || result.ProfileSummaries[1].PlanCreated || result.ProfileSummaries[1].Reason != "no_matching_routes" {
		t.Fatalf("unexpected vpn-only summary %#v", result.ProfileSummaries[1])
	}
}

func TestReconcileMultiPushPlanClockifyOnlyEmptyPlanStillIncludesSummary(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient {
		return &fakeClockifyClient{
			projects: []clockify.Project{{ID: "proj-app", Name: "App"}},
			tagsByID: map[string]clockify.Tag{},
		}
	}

	result, err := service.ReconcileMultiPushPlan(
		context.Background(),
		testClockifyConfig(true),
		targetScope(clockifyTarget()),
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
	if got := strings.Join(result.Plan.TargetInstances, ","); got != config.ClockifyInstanceName {
		t.Fatalf("unexpected clockify target instances %q", got)
	}
	if len(result.ProfileSummaries) != 1 {
		t.Fatalf("expected one clockify summary, got %#v", result.ProfileSummaries)
	}
	summary := result.ProfileSummaries[0]
	if summary.AdapterFamily != "clockify" || summary.RouteProfile != "" {
		t.Fatalf("unexpected clockify summary %#v", summary)
	}
	if got := strings.Join(summary.ResolvedTargetInstances, ","); got != config.ClockifyInstanceName {
		t.Fatalf("unexpected clockify summary target instances %q", got)
	}
	if summary.ScopeCount != 0 || summary.ActionableScopeCount != 0 || !summary.PlanCreated {
		t.Fatalf("unexpected empty clockify summary %#v", summary)
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
	deleteTimeEntryErr        error
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
	searchIssuesErr error
	worklogsByIssue map[string][]jiradatacenter.Worklog
	currentUserErr  error
	worklogErrByKey map[string]error
}

type fakeJiraCloudClient struct {
	user            jiracloud.User
	searchIssues    []jiracloud.IssueBrief
	searchIssuesErr error
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
	if f.searchIssuesErr != nil {
		return nil, f.searchIssuesErr
	}
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
	if f.searchIssuesErr != nil {
		return nil, f.searchIssuesErr
	}
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
	if f.deleteTimeEntryErr != nil {
		return f.deleteTimeEntryErr
	}
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

func seedDeliveryAttempt(t *testing.T, store *sqlitestore.Store, planID, planItemID, state, message, createdAt string) {
	t.Helper()
	if _, err := store.DB().Exec(
		`INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		planItemID+"-"+state+"-"+createdAt, planID, planItemID, state, message, createdAt,
	); err != nil {
		t.Fatalf("seed delivery attempt: %v", err)
	}
}

func countTrashRecords(t *testing.T, store *sqlitestore.Store) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM trashed_worklogs`).Scan(&count); err != nil {
		t.Fatalf("count trash records: %v", err)
	}
	return count
}

func listTrashRecords(t *testing.T, store *sqlitestore.Store) []worklogs.TrashRecord {
	t.Helper()
	service := worklogs.NewService(store)
	items, _, err := service.ListTrash(config.EffectiveConfig{Location: time.UTC}, worklogs.ListFilters{
		From: "2026-05-01",
		To:   "2026-05-31",
	})
	if err != nil {
		t.Fatalf("list trash records: %v", err)
	}
	return items
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

func targetScope(targets ...ReconcileTarget) ReconcileScope {
	return ReconcileScope{Targets: targets}
}

func clockifyTarget() ReconcileTarget {
	return ReconcileTarget{AdapterFamily: "clockify", Instance: config.ClockifyInstanceName}
}

func jiraCloudTarget(instance string) ReconcileTarget {
	return ReconcileTarget{AdapterFamily: "jira-cloud", Instance: instance}
}

func jiraDataTarget(instance string) ReconcileTarget {
	return ReconcileTarget{AdapterFamily: "jira-data-center", Instance: instance}
}

func assertPlanItem(t *testing.T, item PlanItem, wantStatus, wantAction string) {
	t.Helper()
	if item.PlanStatus != wantStatus || item.PlannedAction != wantAction {
		t.Fatalf("unexpected plan item for %s: status=%s action=%s", item.IssueKey, item.PlanStatus, item.PlannedAction)
	}
}

package reconcile

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
)

func TestApplyAndRetryIsolateClockifyPreparationFromJira(t *testing.T) {
	for _, resource := range []string{"projects", "tags"} {
		t.Run(resource, func(t *testing.T) {
			store := newTestStore(t)
			defer store.Close()
			seedWorklogRow(t, store, "row", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Sync me")
			cfg := testJiraCloudConfig()
			delete(cfg.File.JiraCloud.Instances["product"].Routing.Profiles, "reporting")
			cfg.File.Clockify = testClockifyConfig(true).File.Clockify
			cc := &fakeClockifyClient{projects: []clockify.Project{{ID: "project", Name: "App"}}}
			jc := &fakeJiraCloudClient{user: jiracloud.User{AccountID: "u1"}, worklogsByIssue: map[string][]jiracloud.Worklog{}}
			s := NewService(store)
			s.newClockifyClient = func(config.ClockifyConfig) clockifyClient { return cc }
			s.newJiraCloudClient = func(config.JiraCloudInstance) jiraCloudClient { return jc }
			planned, err := s.ReconcileMultiPushPlan(t.Context(), cfg, targetScope(clockifyTarget(), jiraCloudTarget("product")), "", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
			if err != nil || planned.Plan == nil {
				t.Fatalf("plan: %+v, %v", planned, err)
			}
			failure := &clockify.RequestError{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests"}
			if resource == "projects" {
				cc.listProjectsErr = failure
			} else {
				cc.listTagsErr = failure
			}
			result, err := s.ApplyPlan(t.Context(), cfg, planned.Plan.ID)
			if err != nil || result.AppliedCount != 1 || result.FailedCount != 1 || !result.MixedResult || result.UnattemptedCount != 0 {
				t.Fatalf("mixed execution: %+v, %v", result, err)
			}
			loaded, err := s.LoadPlan(planned.Plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range loaded.Items {
				if item.TargetAdapterFamily == "clockify" && (item.ExecutionState != "failed" || !strings.Contains(item.LatestAttempt.Message, "list Clockify "+resource) || !strings.Contains(item.LatestAttempt.Message, "429")) {
					t.Fatalf("Clockify result: %+v", item)
				}
				if item.TargetAdapterFamily == "jira-cloud" && item.ExecutionState != "succeeded" {
					t.Fatalf("Jira blocked by Clockify: %+v", item)
				}
			}
			if len(jc.worklogsByIssue["AAPP-1"]) != 1 || len(cc.entries) != 0 || loaded.AppliedAt != nil {
				t.Fatal("incorrect remote effects or premature plan completion")
			}
			cc.listProjectsErr, cc.listTagsErr = nil, nil
			retried, err := s.RetryPlan(t.Context(), cfg, planned.Plan.ID, "failed")
			if err != nil || retried.AppliedCount != 1 || retried.FailedCount != 0 || len(cc.entries) != 1 || len(jc.worklogsByIssue["AAPP-1"]) != 1 {
				t.Fatalf("retry replayed healthy scope or lost failed scope: %+v, %v", retried, err)
			}
		})
	}
}

type rateLimitAfterWriteClient struct {
	*fakeClockifyClient
	writes int
}

func (c *rateLimitAfterWriteClient) CreateTimeEntry(ctx context.Context, workspace string, row clockify.CandidateRow, project string, tags []string) (clockify.TimeEntry, error) {
	if c.writes > 0 {
		return clockify.TimeEntry{}, &clockify.RequestError{StatusCode: 429, Status: "429 Too Many Requests"}
	}
	c.writes++
	return c.fakeClockifyClient.CreateTimeEntry(ctx, workspace, row, project, tags)
}

func TestRateLimitAfterPartialClockifyWriteRequiresReconciliation(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "one", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "First")
	seedWorklogRow(t, store, "two", "AAPP-1", "2026-05-01T10:00:00Z", 3600, "Second")
	client := &rateLimitAfterWriteClient{fakeClockifyClient: &fakeClockifyClient{projects: []clockify.Project{{ID: "project", Name: "App"}}}}
	s := NewService(store)
	s.newClockifyClient = func(config.ClockifyConfig) clockifyClient { return client }
	cfg := testClockifyConfig(true)
	p, err := s.CreateClockifyPushPlan(t.Context(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ApplyPlan(t.Context(), cfg, p.ID)
	if err != nil || r.FailedCount != 1 || len(r.ScopeResults) != 1 || r.ScopeResults[0].ExecutionState != "uncertain" || !strings.Contains(r.ScopeResults[0].Message, "429") || len(client.entries) != 1 {
		t.Fatalf("partial write was not protected: %+v, %v", r, err)
	}
	loaded, err := s.LoadPlan(p.ID)
	if err != nil || loaded.Items[0].ExecutionState != "uncertain" {
		t.Fatalf("uncertainty not saved: %+v, %v", loaded, err)
	}
	r, err = s.RetryPlan(t.Context(), cfg, p.ID, "uncertain")
	if err != nil || r.FailedCount != 1 || r.ScopeResults[0].ExecutionState != "uncertain" || len(client.entries) != 1 {
		t.Fatalf("partial writes were replayed: %+v, %v", r, err)
	}
}

func TestCompletedRemoteOutcomeRetainedWhenPersistenceFails(t *testing.T) {
	store := newTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	s := NewService(store)
	var summary pushExecutionSummary
	err := s.recordPushExecutionOutcome(&summary, pushExecutionOutcome{
		item:     PlanItem{ID: "scope", PlanID: "plan", TargetAdapterFamily: "jira-cloud", TargetAdapterInstance: "company"},
		executed: true, finalState: "succeeded", applyMessage: "remote write confirmed",
	}, progress.NewNoopReporter(), 1, 1)
	if err == nil || summary.appliedCount != 1 || len(summary.scopeResults) != 1 || len(summary.scopeResults[0].Warnings) != 1 {
		t.Fatalf("known remote result lost after persistence failure: %+v, %v", summary, err)
	}
}

func TestClockifyPreparationCancellationLeavesScopeUnattempted(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	seedWorklogRow(t, store, "row", "AAPP-1", "2026-05-01T08:00:00Z", 3600, "Sync me")
	cc := &fakeClockifyClient{projects: []clockify.Project{{ID: "project", Name: "App"}}}
	s := NewService(store)
	s.newClockifyClient = func(config.ClockifyConfig) clockifyClient { return cc }
	cfg := testClockifyConfig(true)
	p, err := s.CreateClockifyPushPlan(t.Context(), cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ApplyPlan(ctx, cfg, p.ID); err != context.Canceled {
		t.Fatalf("cancel error: %v", err)
	}
	loaded, err := s.LoadPlan(p.ID)
	if err != nil || loaded.Items[0].ExecutionState != "not_attempted" {
		t.Fatalf("cancellation manufactured a failure: %+v, %v", loaded, err)
	}
}

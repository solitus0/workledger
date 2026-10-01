package reconcile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/reconcile/model"
)

func TestJiraPullMinutePayloadAndRawObservations(t *testing.T) {
	for _, family := range []string{"jira-cloud", "jira-data-center"} {
		t.Run(family, func(t *testing.T) {
			store := newTestStore(t)
			defer store.Close()
			service := NewService(store)
			observed := jiradatacenter.Worklog{ID: "raw", Started: "2026-05-01T08:00:40.000+0000", TimeSpentSeconds: 931, Comment: "legacy", Author: jiradatacenter.WorklogUser{AccountID: "u1"}}
			var plan Plan
			var err error
			var cfg config.EffectiveConfig
			if family == "jira-cloud" {
				cfg = testJiraCloudConfig()
				service.newJiraCloudClient = func(config.JiraCloudInstance) jiraCloudClient {
					return &fakeJiraCloudClient{user: jiracloud.User{AccountID: "u1"}, searchIssues: []jiracloud.IssueBrief{{Key: "AAPP-1"}}, worklogsByIssue: map[string][]jiracloud.Worklog{"AAPP-1": {observed}}}
				}
				plan, err = service.CreateJiraCloudPullPlan(context.Background(), cfg, "product", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
			} else {
				cfg = testJiraDataConfig()
				service.newJiraDataClient = func(config.JiraDataCenterInstance) jiraDataClient {
					return &fakeJiraDataClient{user: jiradatacenter.User{AccountID: "u1"}, searchIssues: []jiradatacenter.IssueBrief{{Key: "AAPP-1"}}, worklogsByIssue: map[string][]jiradatacenter.Worklog{"AAPP-1": {observed}}}
				}
				plan, err = service.CreateJiraDataPullPlan(context.Background(), cfg, "internal", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 1 || plan.Items[0].RemoteTotal != 931 || plan.Items[0].InspectionSummary.RemoteTotalSeconds != 931 || plan.Items[0].Payload[0].DurationSeconds != 960 || !plan.Items[0].Payload[0].StartedAtUTC.Equal(mustTime("2026-05-01T08:01:00Z")) {
				t.Fatalf("plan=%#v", plan)
			}
			if _, err := service.ApplyPlan(context.Background(), cfg, plan.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMultiPullChecksOriginalTimingTogether(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(map[bool]string{false: "introduced overlap", true: "existing overlap"}[overlap], func(t *testing.T) {
			store := newTestStore(t)
			defer store.Close()
			service := NewService(store)
			cfg := testJiraCloudConfig()
			cfg.File.JiraCloud.Instances["second"] = config.JiraCloudInstance{BaseURL: "https://second.atlassian.net", Auth: config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t2"}, Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{"default": {IssuePrefixes: []string{"BAPP"}}}}}
			service.newJiraCloudClient = func(instance config.JiraCloudInstance) jiraCloudClient {
				key, start, seconds := "AAPP-1", "2026-05-01T08:00:30.000+0000", 930
				if instance.BaseURL == "https://second.atlassian.net" {
					key, start, seconds = "BAPP-1", "2026-05-01T08:16:00.000+0000", 900
					if overlap {
						start = "2026-05-01T08:10:00.000+0000"
					}
				}
				return &fakeJiraCloudClient{user: jiracloud.User{AccountID: "u1"}, searchIssues: []jiracloud.IssueBrief{{Key: key}}, worklogsByIssue: map[string][]jiracloud.Worklog{key: {{ID: key, Started: start, TimeSpentSeconds: seconds, Comment: key, Author: jiracloud.WorklogUser{AccountID: "u1"}}}}}
			}
			scope := ReconcileScope{Targets: []ReconcileTarget{{AdapterFamily: "jira-cloud", Instance: "product"}, {AdapterFamily: "jira-cloud", Instance: "second"}}}
			plan, err := service.CreateMultiPullPlan(context.Background(), cfg, scope, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 2 {
				t.Fatalf("items=%#v", plan.Items)
			}
			want := "blocked"
			if overlap {
				want = "ready"
			}
			for _, item := range plan.Items {
				if item.PlanStatus != want {
					t.Fatalf("item=%#v", item)
				}
			}
		})
	}
}

func TestPullRepairsMatchingLegacyTimingAndPreservesTrash(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	var guard string
	if err := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name='trg_worklogs_minutes_insert'`).Scan(&guard); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER trg_worklogs_minutes_insert`); err != nil {
		t.Fatal(err)
	}
	seedWorklogRow(t, store, "legacy", "AAPP-1", "2026-05-01T08:00:40Z", 931, "legacy")
	if _, err := store.DB().Exec(guard); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	cfg := testClockifyConfig(false)
	row := model.Row{IssueKey: "AAPP-1", StartedAtUTC: mustTime("2026-05-01T08:00:40Z"), DurationSeconds: 931, Description: "legacy"}
	plan, err := service.buildClockifyPullPlanFromRows(cfg, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), []model.Row{row}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.normalizePullPlan(context.Background(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].PlanStatus != "ready" {
		t.Fatalf("item=%#v", plan.Items[0])
	}
	if err := service.insertPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyPlan(context.Background(), cfg, plan.ID); err != nil {
		t.Fatal(err)
	}
	var start string
	var seconds int
	if err := store.DB().QueryRow(`SELECT started_at_utc,duration_seconds FROM trashed_worklogs WHERE source_worklog_id='legacy'`).Scan(&start, &seconds); err != nil {
		t.Fatal(err)
	}
	if start != "2026-05-01T08:00:40Z" || seconds != 931 {
		t.Fatalf("audit timing=%s/%d", start, seconds)
	}
}

func TestPullMinuteCandidates(t *testing.T) {
	from, to := mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z")
	row := func(start string, seconds int) model.Row {
		return model.Row{IssueKey: "AAPP-1", SourceRowID: start, StartedAtUTC: mustTime(start), DurationSeconds: seconds, Description: "remote"}
	}
	for _, tc := range []struct {
		name     string
		rows     []model.Row
		status   string
		findings int
	}{
		{"round", []model.Row{row("2026-05-01T08:00:30Z", 931)}, "ready", 0},
		{"short import", []model.Row{row("2026-05-01T08:00:40Z", 96)}, "ready", 0},
		{"zero", []model.Row{row("2026-05-01T08:00:00Z", 29)}, "invalid", 1},
		{"outside window", []model.Row{row("2026-05-01T23:59:30Z", 900)}, "invalid", 1},
		{"duplicate", []model.Row{row("2026-05-01T08:00:10Z", 901), row("2026-05-01T08:00:20Z", 903)}, "blocked", 0},
		{"overlap", []model.Row{row("2026-05-01T08:00:30Z", 930), row("2026-05-01T08:16:00Z", 900)}, "blocked", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			defer store.Close()
			service := NewService(store)
			plan, err := service.buildClockifyPullPlanFromRows(testClockifyConfig(false), from, to, tc.rows, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.normalizePullPlan(context.Background(), &plan); err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 1 || plan.Items[0].PlanStatus != tc.status || len(plan.Findings) != tc.findings {
				t.Fatalf("plan = %#v", plan)
			}
			if plan.Items[0].RemoteTotal != sumRows(tc.rows) {
				t.Fatal("observed remote total was rounded")
			}
			if tc.status == "ready" {
				if err := service.insertPlan(context.Background(), plan); err != nil {
					t.Fatal(err)
				}
				if _, err := service.ApplyPlan(context.Background(), testClockifyConfig(false), plan.ID); err != nil {
					t.Fatal(err)
				}
				local, err := service.listLocalScope("AAPP-1", from, to)
				if err != nil || len(local) != 1 || local[0].DurationSeconds%60 != 0 || local[0].StartedAtUTC.Second() != 0 {
					t.Fatalf("import = %#v, %v", local, err)
				}
				if !strings.Contains(tc.rows[0].StartedAtUTC.Format(time.RFC3339), "08:00:") {
					t.Fatal("raw observation mutated")
				}
			}
		})
	}
}

func TestPullMinuteCollisionAcrossIssuesAndExistingLocal(t *testing.T) {
	for _, existing := range []bool{false, true} {
		store := newTestStore(t)
		service := NewService(store)
		rows := []model.Row{{IssueKey: "AAPP-1", StartedAtUTC: mustTime("2026-05-01T08:00:30Z"), DurationSeconds: 930, Description: "round"}}
		if existing {
			seedWorklogRow(t, store, "neighbor", "BAPP-1", "2026-05-01T08:16:00Z", 900, "neighbor")
		} else {
			rows = append(rows, model.Row{IssueKey: "BAPP-1", StartedAtUTC: mustTime("2026-05-01T08:16:00Z"), DurationSeconds: 900, Description: "neighbor"})
		}
		plan, err := service.buildClockifyPullPlanFromRows(testClockifyConfig(false), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"), rows, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.normalizePullPlan(context.Background(), &plan); err != nil {
			t.Fatal(err)
		}
		for _, item := range plan.Items {
			if item.PlanStatus != "blocked" {
				t.Fatalf("existing=%t, item=%#v", existing, item)
			}
		}
		store.Close()
	}
}

func TestSavedMinutePayloadPreflightBeforeAnyMutation(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	service := NewService(store)
	ctx := context.Background()
	cfg := testClockifyConfig(false)
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{ID: "legacy-precision", Direction: "pull", AdapterFamily: "clockify", ConfigFingerprint: fingerprint, WindowFromUTC: mustTime("2026-05-01T00:00:00Z"), WindowToUTC: mustTime("2026-05-01T23:59:59Z"), CreatedAt: mustTime("2026-05-02T00:00:00Z"), PlanningStatus: "ready"}
	for i, seconds := range []int{900, 901} {
		key := []string{"AAPP-1", "BAPP-1"}[i]
		item := newPlanItem(plan, key, []model.Row{{IssueKey: key, StartedAtUTC: mustTime("2026-05-01T08:00:00Z"), DurationSeconds: seconds, Description: "snapshot"}})
		item.PlanStatus, item.PlannedAction = "ready", "merge"
		plan.Items = append(plan.Items, item)
	}
	if err := service.insertPlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyPlan(ctx, cfg, plan.ID); err == nil || !strings.Contains(err.Error(), "generate a new plan") {
		t.Fatalf("apply error=%v", err)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM delivery_attempts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("attempts=%d, %v", count, err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM worklogs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("local writes=%d, %v", count, err)
	}
	if err := service.recordDeliveryAttempt(plan.ID, plan.Items[1].ID, "failed", "old failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RetryPlan(ctx, cfg, plan.ID, "failed"); err == nil {
		t.Fatal("retry accepted incompatible snapshot")
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM delivery_attempts`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry attempts=%d, %v", count, err)
	}
}

func TestJiraMinutePersistenceAndConvergence(t *testing.T) {
	for _, family := range []string{"jira-cloud", "jira-data-center"} {
		for _, mismatch := range []bool{false, true} {
			t.Run(family+map[bool]string{false: "/match", true: "/mismatch"}[mismatch], func(t *testing.T) {
				posts := 0
				entries := []map[string]any{}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/myself"):
						json.NewEncoder(w).Encode(map[string]any{"accountId": "u1"})
					case strings.HasSuffix(r.URL.Path, "/search/jql"):
						json.NewEncoder(w).Encode(map[string]any{"isLast": true, "issues": []map[string]string{{"key": "AAPP-1"}}})
					case strings.HasSuffix(r.URL.Path, "/search"):
						json.NewEncoder(w).Encode(map[string]any{"startAt": 0, "total": 1, "issues": []map[string]string{{"key": "AAPP-1"}}})
					case r.Method == http.MethodPost:
						var entry map[string]any
						if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
							t.Error(err)
							return
						}
						posts++
						entry["id"] = "remote-created"
						entry["author"] = map[string]any{"accountId": "u1"}
						if mismatch {
							entry["timeSpentSeconds"] = entry["timeSpentSeconds"].(float64) - 60
						}
						entries = append(entries, entry)
						json.NewEncoder(w).Encode(entry)
					case strings.HasSuffix(r.URL.Path, "/worklog"):
						json.NewEncoder(w).Encode(map[string]any{"total": len(entries), "worklogs": entries})
					default:
						t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					}
				}))
				defer server.Close()
				store := newTestStore(t)
				defer store.Close()
				service := NewService(store)
				cfg, instance := testJiraCloudConfig(), "product"
				if family == "jira-cloud" {
					value := cfg.File.JiraCloud.Instances[instance]
					value.BaseURL = server.URL
					cfg.File.JiraCloud.Instances[instance] = value
				} else {
					cfg, instance = testJiraDataConfig(), "internal"
					value := cfg.File.JiraData.Instances[instance]
					value.BaseURL = server.URL
					cfg.File.JiraData.Instances[instance] = value
				}
				seedWorklogRow(t, store, "one", "AAPP-1", "2026-05-01T08:00:00Z", 900, "first")
				seedWorklogRow(t, store, "two", "AAPP-1", "2026-05-01T09:00:00Z", 900, "second")
				from, to := mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z")
				build := func() (Plan, error) {
					if family == "jira-cloud" {
						return service.CreateJiraCloudPushPlan(context.Background(), cfg, "default", from, to, false)
					}
					return service.CreateJiraDataPushPlan(context.Background(), cfg, "default", from, to, false)
				}
				plan, err := build()
				if err != nil {
					t.Fatal(err)
				}
				result, err := service.ApplyPlan(context.Background(), cfg, plan.ID)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := service.LoadPlan(plan.ID)
				if err != nil {
					t.Fatal(err)
				}
				if mismatch {
					if result.FailedCount != 1 || result.AppliedCount != 0 || posts != 1 || loaded.ExecutionState != "failed" {
						t.Fatalf("result=%#v posts=%d state=%s", result, posts, loaded.ExecutionState)
					}
					byItem, err := service.loadDeliveryAttempts(plan.ID)
					attempts := byItem[plan.Items[0].ID]
					if err != nil || len(attempts) == 0 || !strings.Contains(attempts[len(attempts)-1].Message, "remote-created") {
						t.Fatalf("attempts=%#v err=%v", attempts, err)
					}
				} else {
					if result.FailedCount != 0 || result.AppliedCount != 1 || posts != 2 {
						t.Fatalf("result=%#v posts=%d", result, posts)
					}
					again, err := build()
					if err != nil {
						t.Fatal(err)
					}
					for _, item := range again.Items {
						if item.PlanStatus != "skipped" {
							t.Fatalf("non-converging item=%#v", item)
						}
					}
				}
			})
		}
	}
}

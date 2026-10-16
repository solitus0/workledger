package issues

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestPullPreservesOverridesAndCommitsSuccessesForUnusedIssues(t *testing.T) {
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "issues.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	local := worklogs.NewService(store)
	title, epic, manual, cached := "Manual title", "October", int64(7200), int64(3600)
	if err := local.ApplyIssueMetadata(t.Context(), []worklogs.IssueMetadataPatch{
		{IssueKey: "APP-1", TitleSet: true, Title: &title, MaxEstimateSet: true, MaxEstimateSeconds: &manual, EpicSet: true, Epic: &epic},
		{IssueKey: "APP-2"}, {IssueKey: "APP-3"},
	}, false); err != nil {
		t.Fatal(err)
	}
	before, _ := local.LookupIssueMetadata(t.Context(), []string{"APP-1"})
	if err := local.UpsertIssueMetadataBatch(t.Context(), []worklogs.IssueMetadata{
		{IssueKey: "APP-2", Title: "Previous title", MaxEstimateSeconds: &cached, SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "product", RefreshedAt: time.Now()},
		{IssueKey: "APP-3", Title: "Failed issue cache", MaxEstimateSeconds: &cached, SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "product", RefreshedAt: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	service := NewServiceWith(local, Dependencies{JiraCloudClient: func(config.JiraCloudInstance) IssueClient {
		return fakeIssueClient{estimates: map[string]*int64{"APP-1": &cached}, errAt: "APP-3"}
	}})
	result := service.Pull(t.Context(), issueTestConfig(), []string{"APP-1", "APP-2", "APP-3", "APP-1"})
	if len(result.Items) != 2 || len(result.Failures) != 1 || result.Failures[0].IssueKey != "APP-3" {
		t.Fatalf("pull: %+v", result)
	}
	items, err := local.LookupIssueMetadata(t.Context(), []string{"APP-1", "APP-2", "APP-3"})
	if err != nil {
		t.Fatal(err)
	}
	one := items["APP-1"]
	if one.Title != title || one.ManualTitle != title || one.CachedTitle != "Jira APP-1" || *one.MaxEstimateSeconds != manual || *one.CachedMaxEstimateSeconds != cached || one.Epic != epic || one.CreatedAt != before["APP-1"].CreatedAt || one.UpdatedAt != before["APP-1"].UpdatedAt {
		t.Fatalf("overrides/identity changed: %+v", one)
	}
	if items["APP-2"].Title != "Jira APP-2" || items["APP-2"].MaxEstimateSeconds != nil || items["APP-2"].RemainingSeconds != nil {
		t.Fatalf("absent estimate: %+v", items["APP-2"])
	}
	if items["APP-3"].Title != "Failed issue cache" || *items["APP-3"].MaxEstimateSeconds != cached {
		t.Fatalf("failure replaced cache: %+v", items["APP-3"])
	}
	used, _, err := local.ListUsedIssues(t.Context(), worklogs.IssueFindByTitle, "Jira APP-2", 25, 0)
	if err != nil || len(used) != 1 || used[0].IssueKey != "APP-2" || used[0].Title != "Jira APP-2" {
		t.Fatalf("autocomplete ignored cached title: %+v %v", used, err)
	}
	used, _, err = local.ListUsedIssues(t.Context(), worklogs.IssueFindByTitle, "Jira APP-1", 25, 0)
	if err != nil || len(used) != 0 {
		t.Fatalf("autocomplete ignored manual override: %+v %v", used, err)
	}
	if err := local.ApplyIssueMetadata(t.Context(), []worklogs.IssueMetadataPatch{{IssueKey: "APP-1", TitleSet: true, MaxEstimateSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	one, err = local.ShowIssueMetadata(t.Context(), "APP-1")
	if err != nil || one.Title != "Jira APP-1" || *one.MaxEstimateSeconds != cached || one.Epic != epic {
		t.Fatalf("clear did not reveal cache: %+v %v", one, err)
	}
}

func TestPullRoutesCloudAndDataCenterAndRequestsBothFields(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Query().Get("fields") != "summary,timetracking" {
			t.Errorf("fields = %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"fields":{"summary":"Jira title","timetracking":{"originalEstimateSeconds":5400}}}`)
	}))
	defer server.Close()
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "issues.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	route := func(prefix string) *config.JiraInstanceRoutes {
		return &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{"main": {IssuePrefixes: []string{prefix}}}}
	}
	cfg := issueTestConfig()
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{BaseURL: server.URL, Auth: config.JiraCloudAuthBlock{Email: "user@example.test", Token: "token"}, Routing: route("APP")}
	cfg.File.JiraData = &config.JiraDataCenterConfig{Instances: map[string]config.JiraDataCenterInstance{"dc": {BaseURL: server.URL, Auth: config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "token"}}, Routing: route("OPS")}}}
	result := NewService(worklogs.NewService(store)).Pull(t.Context(), cfg, []string{"APP-1", "OPS-2", "AP-3"})
	if len(result.Items) != 2 || len(result.Failures) != 1 || result.Failures[0].IssueKey != "AP-3" {
		t.Fatalf("result: %+v", result)
	}
	if !reflect.DeepEqual(calls, []string{"/rest/api/3/issue/APP-1", "/rest/api/2/issue/OPS-2"}) {
		t.Fatalf("requests: %v", calls)
	}
	for _, item := range result.Items {
		if item.Title != "Jira title" || item.MaxEstimateSeconds == nil || *item.MaxEstimateSeconds != 5400 {
			t.Fatalf("decoded: %+v", item)
		}
	}
	// A reporting-only prefix must not fetch its mapped target or original key.
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{"main": {ReportingTargets: map[string]string{"APP": "BILL-1"}}}}}
	result = NewService(worklogs.NewService(store)).Pull(t.Context(), cfg, []string{"APP-1"})
	if len(result.Failures) != 1 || len(calls) != 2 {
		t.Fatalf("reporting route fetched: %+v %v", result, calls)
	}
	cfg.File.JiraCloud.Instances["product"] = config.JiraCloudInstance{Routing: route("APP")}
	// Duplicate owners fail without any request; reporting routes never supply an owner.
	cfg.File.JiraData.Instances["dc"] = config.JiraDataCenterInstance{Routing: route("APP")}
	result = NewService(worklogs.NewService(store)).Pull(t.Context(), cfg, []string{"APP-1"})
	if len(result.Failures) != 1 || len(calls) != 2 {
		t.Fatalf("ambiguous route fetched: %+v %v", result, calls)
	}
}

func TestPullCancellationRetainsCachedValues(t *testing.T) {
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "issues.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(t.Context())
	estimate := int64(3600)
	client := &cancelingIssueClient{cancel: cancel, estimate: &estimate}
	local := worklogs.NewService(store)
	service := NewServiceWith(local, Dependencies{JiraCloudClient: func(config.JiraCloudInstance) IssueClient { return client }})
	result := service.Pull(ctx, issueTestConfig(), []string{"APP-1", "APP-2"})
	if len(result.Items) != 0 || len(result.Failures) != 2 || client.calls != 1 {
		t.Fatalf("canceled pull: %+v calls=%d", result, client.calls)
	}
	items, err := local.SearchIssues(context.Background(), "", "", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("canceled cache persisted: %+v %v", items, err)
	}
}

package totals

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	jiracloudadapter "github.com/solitus0/workledger/internal/adapter/jiracloud"
	jiradataadapter "github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

func TestCollectAllRunsTargetsConcurrentlyAndPreservesOrder(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	insertTestWorklog(t, store, "local-zapp", "ZAPP-42", "2026-05-01T08:00:00Z", 3600)
	insertTestWorklog(t, store, "local-ignore", "IGN-1", "2026-05-01T11:00:00Z", 1800)

	clockifyGate := newStartGate(1)
	cloudAlphaGate := newStartGate(1)
	cloudZetaGate := newStartGate(1)
	dataCoreGate := newStartGate(1)

	service := NewService(store)
	service.newClockifyClient = func(cfg config.ClockifyConfig) clockifyClient {
		return &fakeClockifyTotalsClient{
			user:    clockifyadapter.User{ID: "user-1", ActiveWorkspace: "ws-1", DefaultWorkspace: "ws-1"},
			gate:    clockifyGate,
			entries: nil,
		}
	}
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		switch cfg.BaseURL {
		case "https://alpha.example.com":
			return &fakeJiraCloudTotalsClient{
				user:       jiracloudadapter.User{AccountID: "user-1"},
				currentErr: &jiracloudadapter.RequestError{StatusCode: 401, Status: "401 Unauthorized"},
				gate:       cloudAlphaGate,
			}
		case "https://zeta.example.com":
			return &fakeJiraCloudTotalsClient{
				user:   jiracloudadapter.User{AccountID: "user-1"},
				issues: []jiracloudadapter.IssueBrief{{Key: "ZAPP-1"}},
				worklogsByRef: map[string][]jiracloudadapter.Worklog{
					"ZAPP-1": {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Author: jiracloudadapter.WorklogUser{AccountID: "user-1"}, Comment: "Docs"}},
				},
				gate: cloudZetaGate,
			}
		default:
			t.Fatalf("unexpected jira cloud base url %s", cfg.BaseURL)
			return nil
		}
	}
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataTotalsClient{
			user:   jiradataadapter.User{AccountID: "user-1"},
			issues: []jiradataadapter.IssueBrief{{Key: "CAPP-1"}},
			worklogsByIssue: map[string][]jiradataadapter.Worklog{
				"CAPP-1": {{ID: "w2", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 1800, Author: jiradataadapter.WorklogUser{AccountID: "user-1"}, Comment: "Ops"}},
			},
			gate: dataCoreGate,
		}
	}

	done := make(chan struct{})
	var items []CollectionItem
	var code int
	go func() {
		items, code = service.CollectAll(context.Background(), testBareTotalsConfig(), mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
		close(done)
	}()

	waitForStarts(t, clockifyGate.started, []string{"clockify"})
	waitForStarts(t, cloudAlphaGate.started, []string{"jira-cloud"})
	waitForStarts(t, cloudZetaGate.started, []string{"jira-cloud"})
	waitForStarts(t, dataCoreGate.started, []string{"jira-data-center"})

	clockifyGate.release()
	cloudAlphaGate.release()
	cloudZetaGate.release()
	dataCoreGate.release()

	<-done

	if code != 4 {
		t.Fatalf("expected first non-zero exit code 4, got %d", code)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}
	gotOrder := []string{
		items[0].Adapter + "/" + items[0].Instance,
		items[1].Adapter + "/" + items[1].Instance,
		items[2].Adapter + "/" + items[2].Instance,
		items[3].Adapter + "/" + items[3].Instance,
	}
	wantOrder := []string{"clockify/clockify", "jira-cloud/alpha", "jira-cloud/zeta", "jira-data-center/core"}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("unexpected order %v", gotOrder)
	}
	if items[1].Status != "auth_error" {
		t.Fatalf("expected auth error for alpha, got %#v", items[1])
	}
	if items[1].Result != nil || items[1].LocalResult == nil || items[1].LocalResult.LocalTotalSeconds != 3600 {
		t.Fatalf("expected scoped local total for failed alpha, got %#v", items[1])
	}
	if items[2].Result == nil || items[3].Result == nil {
		t.Fatalf("expected unrelated workers to complete, got %#v %#v", items[2], items[3])
	}
}

func TestCollectAllLeavesLocalEmptyWhenScopeValidationFails(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	service := NewService(store)
	items, code := service.CollectAll(context.Background(), config.EffectiveConfig{
		Location:     time.UTC,
		TimezoneName: "UTC",
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"broken": {
						BaseURL: "https://broken.example.com",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t"},
					},
				},
			},
		},
	}, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))

	if code != 2 {
		t.Fatalf("expected validation exit code 2, got %d", code)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %#v", items)
	}
	if items[0].Status != "validation_error" || items[0].LocalResult != nil {
		t.Fatalf("expected validation item without local result, got %#v", items[0])
	}
}

func TestCompareJiraCloudRemoteFetchesIssueWorklogsConcurrently(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	worklogGate := newStartGate(2)
	service := NewService(store)
	service.newJiraCloudClient = func(cfg config.JiraCloudInstance) jiraCloudClient {
		return &fakeJiraCloudTotalsClient{
			user:   jiracloudadapter.User{AccountID: "user-1"},
			issues: []jiracloudadapter.IssueBrief{{Key: "AAPP-1"}, {Key: "AAPP-2"}},
			worklogsByRef: map[string][]jiracloudadapter.Worklog{
				"AAPP-1": {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Author: jiracloudadapter.WorklogUser{AccountID: "user-1"}, Comment: "First"}},
				"AAPP-2": {{ID: "w2", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Author: jiracloudadapter.WorklogUser{AccountID: "user-1"}, Comment: "Second"}},
			},
			worklogGate: worklogGate,
		}
	}

	done := make(chan struct{})
	var result Result
	var err error
	go func() {
		_, result, err = service.CompareJiraCloudRemote(context.Background(), testJiraCloudConfig(), "product", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
		close(done)
	}()

	waitForStarts(t, worklogGate.started, []string{"AAPP-1", "AAPP-2"})
	worklogGate.release()
	worklogGate.release()
	<-done

	if err != nil {
		t.Fatalf("CompareJiraCloudRemote failed: %v", err)
	}
	if result.RemoteTotalSeconds != 5400 {
		t.Fatalf("expected remote total 5400, got %#v", result)
	}
}

func TestCompareJiraDataRemoteFetchesIssueWorklogsConcurrently(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	worklogGate := newStartGate(2)
	service := NewService(store)
	service.newJiraDataClient = func(cfg config.JiraDataCenterInstance) jiraDataClient {
		return &fakeJiraDataTotalsClient{
			user:   jiradataadapter.User{AccountID: "user-1"},
			issues: []jiradataadapter.IssueBrief{{Key: "AAPP-1"}, {Key: "AAPP-2"}},
			worklogsByIssue: map[string][]jiradataadapter.Worklog{
				"AAPP-1": {{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Author: jiradataadapter.WorklogUser{AccountID: "user-1"}, Comment: "First"}},
				"AAPP-2": {{ID: "w2", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 1800, Author: jiradataadapter.WorklogUser{AccountID: "user-1"}, Comment: "Second"}},
			},
			worklogGate: worklogGate,
		}
	}

	done := make(chan struct{})
	var result Result
	var err error
	go func() {
		_, result, err = service.CompareJiraDataRemote(context.Background(), testJiraDataConfig(), "core", mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z"))
		close(done)
	}()

	waitForStarts(t, worklogGate.started, []string{"AAPP-1", "AAPP-2"})
	worklogGate.release()
	worklogGate.release()
	<-done

	if err != nil {
		t.Fatalf("CompareJiraDataRemote failed: %v", err)
	}
	if result.RemoteTotalSeconds != 5400 {
		t.Fatalf("expected remote total 5400, got %#v", result)
	}
}

type fakeClockifyTotalsClient struct {
	user       clockifyadapter.User
	entries    []clockifyadapter.TimeEntry
	currentErr error
	entriesErr error
	gate       *startGate
}

func (f *fakeClockifyTotalsClient) CurrentUser(ctx context.Context) (clockifyadapter.User, error) {
	if f.gate != nil {
		f.gate.block("clockify")
	}
	return f.user, f.currentErr
}

func (f *fakeClockifyTotalsClient) ListUserTimeEntries(ctx context.Context, workspaceID, userID string, start, end time.Time) ([]clockifyadapter.TimeEntry, error) {
	return append([]clockifyadapter.TimeEntry(nil), f.entries...), f.entriesErr
}

type fakeJiraCloudTotalsClient struct {
	user          jiracloudadapter.User
	issues        []jiracloudadapter.IssueBrief
	worklogsByRef map[string][]jiracloudadapter.Worklog
	issuesByRef   map[string]jiracloudadapter.IssueBrief
	currentErr    error
	searchErr     error
	worklogErr    error
	gate          *startGate
	worklogGate   *startGate
}

func (f *fakeJiraCloudTotalsClient) CurrentUser(ctx context.Context) (jiracloudadapter.User, error) {
	if f.gate != nil {
		f.gate.block("jira-cloud")
	}
	return f.user, f.currentErr
}

func (f *fakeJiraCloudTotalsClient) SearchIssues(ctx context.Context, jql string, fields []string) ([]jiracloudadapter.IssueBrief, error) {
	return append([]jiracloudadapter.IssueBrief(nil), f.issues...), f.searchErr
}

func (f *fakeJiraCloudTotalsClient) ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiracloudadapter.Worklog, error) {
	if f.worklogGate != nil {
		f.worklogGate.block(issueKey)
	}
	return append([]jiracloudadapter.Worklog(nil), f.worklogsByRef[issueKey]...), f.worklogErr
}

func (f *fakeJiraCloudTotalsClient) GetIssue(ctx context.Context, issueKey string, fields []string) (jiracloudadapter.IssueBrief, error) {
	if item, ok := f.issuesByRef[issueKey]; ok {
		return item, nil
	}
	return jiracloudadapter.IssueBrief{Key: issueKey}, nil
}

type fakeJiraDataTotalsClient struct {
	user            jiradataadapter.User
	issues          []jiradataadapter.IssueBrief
	worklogsByIssue map[string][]jiradataadapter.Worklog
	currentErr      error
	searchErr       error
	worklogErr      error
	gate            *startGate
	worklogGate     *startGate
}

func (f *fakeJiraDataTotalsClient) CurrentUser(ctx context.Context) (jiradataadapter.User, error) {
	if f.gate != nil {
		f.gate.block("jira-data-center")
	}
	return f.user, f.currentErr
}

func (f *fakeJiraDataTotalsClient) SearchIssues(ctx context.Context, jql string, fields []string) ([]jiradataadapter.IssueBrief, error) {
	return append([]jiradataadapter.IssueBrief(nil), f.issues...), f.searchErr
}

func (f *fakeJiraDataTotalsClient) ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiradataadapter.Worklog, error) {
	if f.worklogGate != nil {
		f.worklogGate.block(issueKey)
	}
	return append([]jiradataadapter.Worklog(nil), f.worklogsByIssue[issueKey]...), f.worklogErr
}

func (f *fakeJiraDataTotalsClient) GetIssue(ctx context.Context, issueKey string, fields []string) (jiradataadapter.IssueBrief, error) {
	return jiradataadapter.IssueBrief{Key: issueKey}, nil
}

type startGate struct {
	started   chan string
	releaseCh chan struct{}
}

func newStartGate(size int) *startGate {
	return &startGate{
		started:   make(chan string, size),
		releaseCh: make(chan struct{}, size),
	}
}

func (g *startGate) block(label string) {
	g.started <- label
	<-g.releaseCh
}

func (g *startGate) release() {
	g.releaseCh <- struct{}{}
}

func waitForStarts(t *testing.T, started <-chan string, want []string) {
	t.Helper()
	got := make([]string, 0, len(want))
	timeout := time.After(2 * time.Second)
	for len(got) < len(want) {
		select {
		case value := <-started:
			got = append(got, value)
		case <-timeout:
			t.Fatalf("timed out waiting for starts, got %v want %v", got, want)
		}
	}
	if !sameElements(got, want) {
		t.Fatalf("unexpected starts %v want %v", got, want)
	}
}

func sameElements(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	slices.Sort(gotCopy)
	slices.Sort(wantCopy)
	return slices.Equal(gotCopy, wantCopy)
}

func newTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "worklogs.db"))
	if err != nil {
		t.Fatalf("bootstrap store: %v", err)
	}
	return store
}

func insertTestWorklog(t *testing.T, store *sqlitestore.Store, id, issueKey, startedAtUTC string, durationSeconds int) {
	t.Helper()
	_, err := store.DB().Exec(
		`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		id,
		issueKey,
		startedAtUTC,
		durationSeconds,
		"test",
		startedAtUTC,
		startedAtUTC,
	)
	if err != nil {
		t.Fatalf("insert worklog: %v", err)
	}
}

func testBareTotalsConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		Location:     time.UTC,
		TimezoneName: "UTC",
		File: config.FileConfig{
			Clockify: &config.ClockifyConfig{
				WorkspaceID: "ws-1",
				UserID:      "user-1",
				Auth:        config.ClockifyAuthConfig{APIKey: "token"},
			},
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"alpha": {BaseURL: "https://alpha.example.com", Auth: config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t"}, Routing: routedProfile("ZAPP")},
					"zeta":  {BaseURL: "https://zeta.example.com", Auth: config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t"}, Routing: routedProfile("ZAPP")},
				},
			},
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"core": {BaseURL: "https://core.example.com", Auth: config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t"}}, Routing: routedProfile("CAPP")},
				},
			},
		},
	}
}

func testJiraCloudConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		Location:     time.UTC,
		TimezoneName: "UTC",
		File: config.FileConfig{
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"product": {BaseURL: "https://product.example.com", Auth: config.JiraCloudAuthBlock{Email: "user@example.com", Token: "t"}, Routing: routedProfile("AAPP")},
				},
			},
		},
	}
}

func testJiraDataConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		Location:     time.UTC,
		TimezoneName: "UTC",
		File: config.FileConfig{
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"core": {BaseURL: "https://core.example.com", Auth: config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "t"}}, Routing: routedProfile("AAPP")},
				},
			},
		},
	}
}

func routedProfile(prefix string) *config.JiraInstanceRoutes {
	return &config.JiraInstanceRoutes{
		Profiles: map[string]config.JiraRouteProfile{
			"default": {IssuePrefixes: []string{prefix}},
		},
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

var _ = errors.New

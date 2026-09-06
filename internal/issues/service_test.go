package issues

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

type fakeEstimateClient struct {
	estimates map[string]*int64
	errAt     string
}

type cancelingEstimateClient struct {
	cancel   context.CancelFunc
	estimate *int64
	calls    int
}

func (f *cancelingEstimateClient) OriginalEstimateSeconds(context.Context, string) (*int64, error) {
	f.calls++
	if f.calls == 1 {
		f.cancel()
	}
	return f.estimate, nil
}

func (f fakeEstimateClient) OriginalEstimateSeconds(_ context.Context, key string) (*int64, error) {
	if key == f.errAt {
		return nil, errors.New("remote failed")
	}
	return f.estimates[key], nil
}

func TestRefreshPersistsResultsAtomically(t *testing.T) {
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "workledger.db"))
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer store.Close()
	seedWorklog(t, store, "one", "APP-1")
	seedWorklog(t, store, "two", "APP-2")
	estimate := int64(3600)
	client := fakeEstimateClient{estimates: map[string]*int64{"APP-1": &estimate, "APP-2": nil}}
	service := NewServiceWith(worklogs.NewService(store), Dependencies{
		JiraCloudClient: func(config.JiraCloudInstance) EstimateClient { return client },
		Now:             func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	})

	result, err := service.Refresh(context.Background(), issueTestConfig(), RefreshInput{
		Adapter: "jira-cloud", Field: "max-estimate", Filters: worklogs.ListFilters{From: "2026-05-01", To: "2026-05-01"},
	})
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if result.Instance != "product" || len(result.Items) != 2 {
		t.Fatalf("unexpected result %#v", result)
	}
	stored, err := worklogs.NewService(store).ListIssueMetadata(context.Background(), []string{"APP-1", "APP-2"})
	if err != nil || len(stored) != 2 {
		t.Fatalf("unexpected persisted metadata %#v err=%v", stored, err)
	}
}

func TestRefreshRemoteFailureDoesNotPartiallyPersist(t *testing.T) {
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "workledger.db"))
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer store.Close()
	seedWorklog(t, store, "one", "APP-1")
	seedWorklog(t, store, "two", "APP-2")
	estimate := int64(3600)
	client := fakeEstimateClient{estimates: map[string]*int64{"APP-1": &estimate}, errAt: "APP-2"}
	service := NewServiceWith(worklogs.NewService(store), Dependencies{
		JiraCloudClient: func(config.JiraCloudInstance) EstimateClient { return client },
	})

	_, err = service.Refresh(context.Background(), issueTestConfig(), RefreshInput{
		Adapter: "jira-cloud", Field: "max-estimate", Filters: worklogs.ListFilters{From: "2026-05-01", To: "2026-05-01"},
	})
	if err == nil {
		t.Fatal("expected remote error")
	}
	stored, lookupErr := worklogs.NewService(store).ListIssueMetadata(context.Background(), []string{"APP-1", "APP-2"})
	if lookupErr != nil || len(stored) != 0 {
		t.Fatalf("metadata should remain unchanged: %#v err=%v", stored, lookupErr)
	}
}

func TestRefreshCancellationDoesNotPartiallyPersist(t *testing.T) {
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "workledger.db"))
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer store.Close()
	seedWorklog(t, store, "one", "APP-1")
	seedWorklog(t, store, "two", "APP-2")
	ctx, cancel := context.WithCancel(context.Background())
	estimate := int64(3600)
	client := &cancelingEstimateClient{cancel: cancel, estimate: &estimate}
	service := NewServiceWith(worklogs.NewService(store), Dependencies{
		JiraCloudClient: func(config.JiraCloudInstance) EstimateClient { return client },
	})

	_, err = service.Refresh(ctx, issueTestConfig(), RefreshInput{
		Adapter: "jira-cloud", Field: "max-estimate", Filters: worklogs.ListFilters{From: "2026-05-01", To: "2026-05-01"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh error = %v, want context.Canceled", err)
	}
	stored, lookupErr := worklogs.NewService(store).ListIssueMetadata(context.Background(), []string{"APP-1", "APP-2"})
	if lookupErr != nil || len(stored) != 0 {
		t.Fatalf("metadata should remain unchanged: %#v err=%v", stored, lookupErr)
	}
}

func seedWorklog(t *testing.T, store *sqlitestore.Store, id, issueKey string) {
	t.Helper()
	_, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, '2026-05-01T09:00:00Z', 900, 'seed', '2026-05-01T09:00:00Z', '2026-05-01T09:00:00Z')`, id, issueKey)
	if err != nil {
		t.Fatalf("seed worklog: %v", err)
	}
}

func issueTestConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		Location: time.UTC,
		File: config.FileConfig{JiraCloud: &config.JiraCloudConfig{Instances: map[string]config.JiraCloudInstance{
			"product": {BaseURL: "https://jira.example.test", Auth: config.JiraCloudAuthBlock{Email: "user@example.test", Token: "token"}},
		}}},
	}
}

package clockify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCurrentUserParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Fatalf("missing api key header")
		}
		_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-1","defaultWorkspace":"ws-2"}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	user, err := client.CurrentUser(context.Background())
	if err != nil {
		t.Fatalf("CurrentUser failed: %v", err)
	}
	if user.ID != "user-1" || user.ActiveWorkspace != "ws-1" || user.DefaultWorkspace != "ws-2" {
		t.Fatalf("unexpected user %#v", user)
	}
}

func TestListUserTimeEntriesPaginatesUntilLastPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Header().Set("Last-Page", "false")
			_, _ = w.Write([]byte(`[{"id":"e1","description":"One","timeInterval":{"start":"2026-04-01T08:00:00Z","end":"2026-04-01T09:00:00Z"}}]`))
		case "2":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[{"id":"e2","description":"Two","timeInterval":{"start":"2026-04-01T09:00:00Z","end":"2026-04-01T10:00:00Z"}}]`))
		default:
			t.Fatalf("unexpected page %s", page)
		}
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	items, err := client.ListUserTimeEntries(context.Background(), "ws-1", "user-1", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListUserTimeEntries failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestListTagsPaginatesUntilLastPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Header().Set("Last-Page", "false")
			_, _ = w.Write([]byte(`[{"id":"t1","name":"ABC-123"},{"id":"t2","name":"misc"}]`))
		case "2":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[{"id":"t3","name":"ABC-124"}]`))
		default:
			t.Fatalf("unexpected page %s", page)
		}
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	items, err := client.ListTags(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("ListTags failed: %v", err)
	}
	if len(items) != 3 || items["t1"].Name != "ABC-123" || items["t3"].Name != "ABC-124" {
		t.Fatalf("unexpected tags %#v", items)
	}
}

func TestListProjectsPaginatesUntilLastPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Header().Set("Last-Page", "false")
			_, _ = w.Write([]byte(`[{"id":"p1","name":"App"}]`))
		case "2":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[{"id":"p2","name":"Ops"}]`))
		default:
			t.Fatalf("unexpected page %s", page)
		}
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	items, err := client.ListProjects(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(items) != 2 || items[1].Name != "Ops" {
		t.Fatalf("unexpected projects %#v", items)
	}
}

func TestCreateTagCreateEntryAndDeleteEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/ws-1/tags":
			_, _ = w.Write([]byte(`{"id":"tag-1","name":"ABC-123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/ws-1/time-entries":
			_, _ = w.Write([]byte(`{"id":"entry-1","projectId":"proj-1","description":"Work","tagIds":["tag-1"],"timeInterval":{"start":"2026-04-01T08:00:00Z","end":"2026-04-01T09:00:00Z"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/ws-1/time-entries/entry-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	tag, err := client.CreateTag(context.Background(), "ws-1", "ABC-123")
	if err != nil || tag.ID != "tag-1" {
		t.Fatalf("CreateTag failed: tag=%#v err=%v", tag, err)
	}
	entry, err := client.CreateTimeEntry(context.Background(), "ws-1", CandidateRow{
		IssueKey:        "ABC-123",
		StartedAtUTC:    time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		DurationSeconds: 3600,
		Description:     "Work",
	}, "proj-1", []string{"tag-1"})
	if err != nil || entry.ProjectID != "proj-1" {
		t.Fatalf("CreateTimeEntry failed: entry=%#v err=%v", entry, err)
	}
	if err := client.DeleteTimeEntry(context.Background(), "ws-1", "entry-1"); err != nil {
		t.Fatalf("DeleteTimeEntry failed: %v", err)
	}
}

func TestCurrentUserReturnsRequestErrorOnUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("bad-key")
	client.baseURL = server.URL

	_, err := client.CurrentUser(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("expected RequestError, got %T", err)
	}
	if requestErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status code %d", requestErr.StatusCode)
	}
}

func TestNormalizeEntriesFiltersRunningEntries(t *testing.T) {
	valid, invalid := NormalizeEntries([]TimeEntry{
		{ID: "running", Description: "Running", Tags: []Tag{{Name: "ABC-123"}}, TimeInterval: TimeInterval{Start: "2026-04-01T08:00:00Z"}},
	}, nil)
	if len(valid) != 0 || len(invalid) != 0 {
		t.Fatalf("expected running entry to be ignored, got valid=%d invalid=%d", len(valid), len(invalid))
	}
}

func TestNormalizeEntriesExtractsExactIssueKeyTag(t *testing.T) {
	valid, invalid := NormalizeEntries([]TimeEntry{
		{
			ID:          "e1",
			Description: "  Investigated   bug ",
			Tags:        []Tag{{Name: "ABC-123"}, {Name: "not-an-issue"}},
			TimeInterval: TimeInterval{
				Start: "2026-04-01T08:00:00Z",
				End:   "2026-04-01T09:00:00Z",
			},
		},
	}, nil)
	if len(invalid) != 0 {
		t.Fatalf("expected no invalid entries, got %#v", invalid)
	}
	if len(valid) != 1 {
		t.Fatalf("expected one valid entry, got %d", len(valid))
	}
	if valid[0].IssueKey != "ABC-123" || valid[0].Description != "Investigated bug" || valid[0].DurationSeconds != 3600 {
		t.Fatalf("unexpected normalized row %#v", valid[0])
	}
}

func TestNormalizeEntriesMarksMissingAndAmbiguousIssueTagsInvalid(t *testing.T) {
	_, invalid := NormalizeEntries([]TimeEntry{
		{
			ID:          "missing",
			Description: "Desc",
			Tags:        []Tag{{Name: "misc"}},
			TimeInterval: TimeInterval{
				Start: "2026-04-01T08:00:00Z",
				End:   "2026-04-01T09:00:00Z",
			},
		},
		{
			ID:          "ambiguous",
			Description: "Desc",
			Tags:        []Tag{{Name: "ABC-123"}, {Name: "ABC-124"}},
			TimeInterval: TimeInterval{
				Start: "2026-04-01T09:00:00Z",
				End:   "2026-04-01T10:00:00Z",
			},
		},
	}, nil)
	if len(invalid) != 2 {
		t.Fatalf("expected 2 invalid entries, got %d", len(invalid))
	}
	if invalid[0].ReasonCode != "missing_issue_key_tag" {
		t.Fatalf("unexpected first invalid reason %#v", invalid[0])
	}
	if invalid[1].ReasonCode != "ambiguous_issue_key_tag" {
		t.Fatalf("unexpected second invalid reason %#v", invalid[1])
	}
}

func TestNormalizeEntriesResolvesTagIDs(t *testing.T) {
	valid, invalid := NormalizeEntries([]TimeEntry{
		{
			ID:          "e1",
			Description: "Issue work",
			TagIDs:      []string{"t1"},
			TimeInterval: TimeInterval{
				Start: "2026-04-01T08:00:00Z",
				End:   "2026-04-01T09:00:00Z",
			},
		},
	}, map[string]Tag{
		"t1": {ID: "t1", Name: "ABC-123"},
	})
	if len(invalid) != 0 || len(valid) != 1 {
		t.Fatalf("unexpected normalize result valid=%d invalid=%d", len(valid), len(invalid))
	}
	if valid[0].IssueKey != "ABC-123" {
		t.Fatalf("unexpected issue key %#v", valid[0])
	}
}

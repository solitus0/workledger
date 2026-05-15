package jiradatacenter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestRequestUsesBearerHeader(t *testing.T) {
	client := NewClient("https://jira.example.com", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("unexpected auth header %q", got)
		}
		return jsonResponse(`{"accountId":"u1"}`), nil
	})}

	if _, err := client.CurrentUser(context.Background()); err != nil {
		t.Fatalf("CurrentUser failed: %v", err)
	}
}

func TestNormalizeIssueWorklogsFiltersAndNormalizes(t *testing.T) {
	user := User{AccountID: "u1"}
	rows, invalid := NormalizeIssueWorklogs("AAPP-1", []Worklog{
		{ID: "w1", Started: "2026-05-01T08:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "  Build feature  ", Author: WorklogUser{AccountID: "u1"}},
		{ID: "w2", Started: "2026-05-01T09:00:00.000+0000", TimeSpentSeconds: 0, Comment: "bad", Author: WorklogUser{AccountID: "u1"}},
		{ID: "w3", Started: "2026-05-01T10:00:00.000+0000", TimeSpentSeconds: 3600, Comment: "skip", Author: WorklogUser{AccountID: "u2"}},
	}, user, mustRFC3339("2026-05-01T00:00:00Z"), mustRFC3339("2026-05-01T23:59:59Z"))

	if len(rows) != 1 || len(invalid) != 1 {
		t.Fatalf("unexpected rows=%d invalid=%d", len(rows), len(invalid))
	}
	if rows[0].Description != "Build feature" {
		t.Fatalf("unexpected description %q", rows[0].Description)
	}
}

func TestReportingDescriptionAvoidsDuplicatePrefix(t *testing.T) {
	if got := ReportingDescription("AAPP-1", "AAPP-1 | Build feature"); got != "AAPP-1 | Build feature" {
		t.Fatalf("unexpected duplicate prefix result %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func mustRFC3339(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

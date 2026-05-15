package jiracloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRequestUsesBasicAuthHeader(t *testing.T) {
	client := NewClient("https://example.atlassian.net", "user@example.com", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Basic dXNlckBleGFtcGxlLmNvbTpzZWNyZXQ=" {
			t.Fatalf("unexpected auth header %q", got)
		}
		return jsonResponse(`{"accountId":"u1"}`), nil
	})}

	if _, err := client.CurrentUser(context.Background()); err != nil {
		t.Fatalf("CurrentUser failed: %v", err)
	}
}

func TestSearchIssuesAndWorklogsPaginate(t *testing.T) {
	client := NewClient("https://example.atlassian.net", "user@example.com", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/rest/api/3/search/jql":
			nextPageToken := req.URL.Query().Get("nextPageToken")
			if nextPageToken == "" {
				return jsonResponse(`{"nextPageToken":"page-2","isLast":false,"issues":[{"id":"1","key":"AAPP-1"}]}`), nil
			}
			if nextPageToken != "page-2" {
				t.Fatalf("unexpected nextPageToken %q", nextPageToken)
			}
			return jsonResponse(`{"isLast":true,"issues":[{"id":"2","key":"AAPP-2"}]}`), nil
		case req.URL.Path == "/rest/api/3/issue/AAPP-1/worklog":
			startAt := req.URL.Query().Get("startAt")
			if startAt == "0" {
				return jsonResponse(`{"startAt":0,"maxResults":100,"total":2,"worklogs":[{"id":"w1"}]}`), nil
			}
			return jsonResponse(`{"startAt":1,"maxResults":100,"total":2,"worklogs":[{"id":"w2"}]}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})}

	issues, err := client.SearchIssues(context.Background(), "project = AAPP", nil)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	worklogs, err := client.ListIssueWorklogs(context.Background(), "AAPP-1")
	if err != nil {
		t.Fatalf("ListIssueWorklogs failed: %v", err)
	}
	if len(worklogs) != 2 {
		t.Fatalf("expected 2 worklogs, got %d", len(worklogs))
	}
}

func TestSearchIssuesFallsBackToLegacySearchOnNotFound(t *testing.T) {
	client := NewClient("https://example.atlassian.net", "user@example.com", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/rest/api/3/search/jql":
			return &http.Response{
				StatusCode: 404,
				Status:     "404 Not Found",
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(`{"errorMessages":["not found"]}`)),
			}, nil
		case req.URL.Path == "/rest/api/3/search":
			startAt := req.URL.Query().Get("startAt")
			if startAt == "0" {
				return jsonResponse(`{"startAt":0,"maxResults":100,"total":2,"issues":[{"id":"1","key":"AAPP-1"}]}`), nil
			}
			if startAt != "1" {
				t.Fatalf("unexpected startAt %q", startAt)
			}
			return jsonResponse(`{"startAt":1,"maxResults":100,"total":2,"issues":[{"id":"2","key":"AAPP-2"}]}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})}

	issues, err := client.SearchIssues(context.Background(), "project = AAPP", nil)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}

func TestCreateAndDeleteWorklog(t *testing.T) {
	client := NewClient("https://example.atlassian.net", "user@example.com", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/rest/api/3/issue/AAPP-1/worklog":
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			comment, ok := payload["comment"].(map[string]any)
			if !ok || comment["type"] != "doc" || comment["version"].(float64) != 1 {
				t.Fatalf("unexpected comment payload %#v", payload["comment"])
			}
			return jsonResponse(`{"id":"created"}`), nil
		case req.Method == http.MethodDelete && req.URL.Path == "/rest/api/3/issue/AAPP-1/worklog/created":
			return &http.Response{StatusCode: 204, Status: "204 No Content", Header: make(http.Header), Body: io.NopCloser(bytes.NewBuffer(nil))}, nil
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
			return nil, nil
		}
	})}

	created, err := client.CreateWorklog(context.Background(), "AAPP-1", CandidateRow{
		StartedAtUTC:    mustRFC3339("2026-05-01T08:00:00Z"),
		DurationSeconds: 3600,
		Description:     "Build feature",
	})
	if err != nil {
		t.Fatalf("CreateWorklog failed: %v", err)
	}
	if created.ID != "created" {
		t.Fatalf("unexpected created worklog %#v", created)
	}

	if err := client.DeleteWorklog(context.Background(), "AAPP-1", "created"); err != nil {
		t.Fatalf("DeleteWorklog failed: %v", err)
	}
}

func TestRequestErrorMapping(t *testing.T) {
	client := NewClient("https://example.atlassian.net", "user@example.com", "secret")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Status:     "401 Unauthorized",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"errorMessages":["bad auth"]}`)),
		}, nil
	})}

	_, err := client.CurrentUser(context.Background())
	var requestErr *RequestError
	if err == nil || !strings.Contains(err.Error(), "jira cloud request failed") {
		t.Fatalf("unexpected error %v", err)
	}
	if !errors.As(err, &requestErr) || requestErr.StatusCode != 401 {
		t.Fatalf("expected request error, got %T %v", err, err)
	}
	if requestErr.Body != `{"errorMessages":["bad auth"]}` {
		t.Fatalf("unexpected error body %q", requestErr.Body)
	}
	if err.Error() != "jira cloud request failed: 401 Unauthorized: bad auth" {
		t.Fatalf("unexpected formatted error %q", err.Error())
	}
}

func TestRequestErrorUsesRawBodyWhenErrorMessagesMissing(t *testing.T) {
	err := (&RequestError{
		StatusCode: 500,
		Status:     "500 Internal Server Error",
		Body:       `{"errors":{"field":"broken"}}`,
	}).Error()

	if err != `jira cloud request failed: 500 Internal Server Error: {"errors":{"field":"broken"}}` {
		t.Fatalf("unexpected error %q", err)
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

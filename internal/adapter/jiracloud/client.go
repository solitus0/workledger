package jiracloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/reconcile/model"
)

type Client struct {
	baseURL    string
	email      string
	token      string
	httpClient *http.Client
}

type RequestError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *RequestError) Error() string {
	if message := jiraCloudErrorMessage(e.Body); message != "" {
		return fmt.Sprintf("jira cloud request failed: %s: %s", e.Status, message)
	}
	return fmt.Sprintf("jira cloud request failed: %s", e.Status)
}

func jiraCloudErrorMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	var payload struct {
		ErrorMessages []string `json:"errorMessages"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil && len(payload.ErrorMessages) > 0 {
		return strings.Join(payload.ErrorMessages, "; ")
	}

	return body
}

type User = jiradatacenter.User
type IssueBrief = jiradatacenter.IssueBrief
type Timetracking = jiradatacenter.Timetracking
type WorklogPage = jiradatacenter.WorklogPage
type Worklog = jiradatacenter.Worklog
type WorklogUser = jiradatacenter.WorklogUser
type CandidateRow = model.Row

type SearchResult struct {
	NextPageToken string       `json:"nextPageToken"`
	IsLast        bool         `json:"isLast"`
	Issues        []IssueBrief `json:"issues"`
}

type legacySearchResult struct {
	StartAt    int          `json:"startAt"`
	MaxResults int          `json:"maxResults"`
	Total      int          `json:"total"`
	Issues     []IssueBrief `json:"issues"`
}

func NewClient(baseURL, email, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		email:      email,
		token:      token,
		httpClient: http.DefaultClient,
	}
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	if err := c.get(ctx, "/rest/api/3/myself", nil, &user); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c *Client) SearchIssues(ctx context.Context, jql string, fields []string) ([]IssueBrief, error) {
	items, err := c.searchIssuesEnhanced(ctx, jql, fields)
	if err == nil {
		return items, nil
	}

	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != http.StatusNotFound {
		return nil, err
	}

	return c.searchIssuesLegacy(ctx, jql, fields)
}

func (c *Client) searchIssuesEnhanced(ctx context.Context, jql string, fields []string) ([]IssueBrief, error) {
	items := make([]IssueBrief, 0)
	nextPageToken := ""
	for {
		query := url.Values{}
		query.Set("jql", jql)
		query.Set("maxResults", "100")
		if nextPageToken != "" {
			query.Set("nextPageToken", nextPageToken)
		}
		if len(fields) > 0 {
			query.Set("fields", strings.Join(fields, ","))
		}
		var page SearchResult
		if err := c.get(ctx, "/rest/api/3/search/jql", query, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Issues...)
		if page.IsLast || page.NextPageToken == "" || len(page.Issues) == 0 {
			break
		}
		nextPageToken = page.NextPageToken
	}
	return items, nil
}

func (c *Client) searchIssuesLegacy(ctx context.Context, jql string, fields []string) ([]IssueBrief, error) {
	items := make([]IssueBrief, 0)
	startAt := 0
	for {
		query := url.Values{}
		query.Set("jql", jql)
		query.Set("maxResults", "100")
		query.Set("startAt", strconv.Itoa(startAt))
		if len(fields) > 0 {
			query.Set("fields", strings.Join(fields, ","))
		}
		var page legacySearchResult
		if err := c.get(ctx, "/rest/api/3/search", query, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Issues...)
		startAt += len(page.Issues)
		if startAt >= page.Total || len(page.Issues) == 0 {
			break
		}
	}
	return items, nil
}

func (c *Client) ListIssueWorklogs(ctx context.Context, issueKey string) ([]Worklog, error) {
	items := make([]Worklog, 0)
	startAt := 0
	for {
		query := url.Values{}
		query.Set("startAt", strconv.Itoa(startAt))
		query.Set("maxResults", "100")
		var page WorklogPage
		route := path.Join("/rest/api/3/issue", issueKey, "worklog")
		if err := c.get(ctx, route, query, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Worklogs...)
		startAt += len(page.Worklogs)
		if startAt >= page.Total || len(page.Worklogs) == 0 {
			break
		}
	}
	return items, nil
}

func (c *Client) GetIssue(ctx context.Context, issueKey string, fields []string) (IssueBrief, error) {
	query := url.Values{}
	if len(fields) > 0 {
		query.Set("fields", strings.Join(fields, ","))
	}
	var issue IssueBrief
	if err := c.get(ctx, path.Join("/rest/api/3/issue", issueKey), query, &issue); err != nil {
		return IssueBrief{}, err
	}
	return issue, nil
}

func (c *Client) CreateWorklog(ctx context.Context, issueKey string, row model.Row) (Worklog, error) {
	payload := map[string]any{
		"started":          row.StartedAtUTC.UTC().Format("2006-01-02T15:04:05.000-0700"),
		"timeSpentSeconds": row.DurationSeconds,
		"comment":          adfComment(row.Description),
	}
	var item Worklog
	if err := c.do(ctx, http.MethodPost, path.Join("/rest/api/3/issue", issueKey, "worklog"), payload, &item); err != nil {
		return Worklog{}, err
	}
	return item, nil
}

func (c *Client) DeleteWorklog(ctx context.Context, issueKey, worklogID string) error {
	return c.do(ctx, http.MethodDelete, path.Join("/rest/api/3/issue", issueKey, "worklog", worklogID), nil, nil)
}

func NormalizeIssueWorklogs(issueKey string, worklogs []Worklog, user User, windowFrom, windowTo time.Time) ([]model.Row, []model.InvalidRow) {
	return jiradatacenter.NormalizeIssueWorklogs(issueKey, worklogs, user, windowFrom, windowTo)
}

func ReportingDescription(sourceIssueKey, description string) string {
	return jiradatacenter.ReportingDescription(sourceIssueKey, description)
}

func adfComment(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []map[string]any{
			{
				"type": "paragraph",
				"content": []map[string]any{
					{
						"type": "text",
						"text": text,
					},
				},
			},
		},
	}
}

func (c *Client) get(ctx context.Context, route string, query url.Values, target any) error {
	resp, err := c.request(ctx, http.MethodGet, route, query, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) do(ctx context.Context, method, route string, body any, target any) error {
	resp, err := c.request(ctx, method, route, nil, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if target == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) request(ctx context.Context, method, route string, query url.Values, body any) (*http.Response, error) {
	endpoint := c.baseURL + route
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token)))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		defer resp.Body.Close()
		return nil, &RequestError{StatusCode: resp.StatusCode, Status: resp.Status, Body: strings.TrimSpace(string(body))}
	}
	return resp, nil
}

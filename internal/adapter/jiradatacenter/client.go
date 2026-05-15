package jiradatacenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/solitus0/workledger/internal/reconcile/model"
	"github.com/solitus0/workledger/internal/worklogs"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type RequestError struct {
	StatusCode int
	Status     string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("jira data center request failed: %s", e.Status)
}

type User struct {
	AccountID    string `json:"accountId"`
	Name         string `json:"name"`
	Key          string `json:"key"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

type SearchResult struct {
	StartAt    int          `json:"startAt"`
	MaxResults int          `json:"maxResults"`
	Total      int          `json:"total"`
	Issues     []IssueBrief `json:"issues"`
}

type IssueBrief struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Timetracking *Timetracking `json:"timetracking"`
	} `json:"fields"`
}

type Timetracking struct {
	OriginalEstimateSeconds *int64 `json:"originalEstimateSeconds"`
}

type WorklogPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Worklogs   []Worklog `json:"worklogs"`
}

type Worklog struct {
	ID               string      `json:"id"`
	Started          string      `json:"started"`
	TimeSpentSeconds int         `json:"timeSpentSeconds"`
	Comment          any         `json:"comment"`
	Author           WorklogUser `json:"author"`
}

type WorklogUser struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Key       string `json:"key"`
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: http.DefaultClient,
	}
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	if err := c.get(ctx, "/rest/api/2/myself", nil, &user); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c *Client) SearchIssues(ctx context.Context, jql string, fields []string) ([]IssueBrief, error) {
	items := make([]IssueBrief, 0)
	startAt := 0
	for {
		query := url.Values{}
		query.Set("jql", jql)
		query.Set("startAt", strconv.Itoa(startAt))
		query.Set("maxResults", "100")
		if len(fields) > 0 {
			query.Set("fields", strings.Join(fields, ","))
		}
		var page SearchResult
		if err := c.get(ctx, "/rest/api/2/search", query, &page); err != nil {
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
		route := path.Join("/rest/api/2/issue", issueKey, "worklog")
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
	if err := c.get(ctx, path.Join("/rest/api/2/issue", issueKey), query, &issue); err != nil {
		return IssueBrief{}, err
	}
	return issue, nil
}

func (c *Client) CreateWorklog(ctx context.Context, issueKey string, row model.Row) (Worklog, error) {
	payload := map[string]any{
		"started":          row.StartedAtUTC.UTC().Format("2006-01-02T15:04:05.000-0700"),
		"timeSpentSeconds": row.DurationSeconds,
		"comment":          row.Description,
	}
	var item Worklog
	if err := c.doJSON(ctx, http.MethodPost, path.Join("/rest/api/2/issue", issueKey, "worklog"), payload, &item); err != nil {
		return Worklog{}, err
	}
	return item, nil
}

func (c *Client) DeleteWorklog(ctx context.Context, issueKey, worklogID string) error {
	return c.do(ctx, http.MethodDelete, path.Join("/rest/api/2/issue", issueKey, "worklog", worklogID), nil, nil)
}

func NormalizeIssueWorklogs(issueKey string, worklogsIn []Worklog, user User, windowFrom, windowTo time.Time) ([]model.Row, []model.InvalidRow) {
	valid := make([]model.Row, 0, len(worklogsIn))
	invalid := make([]model.InvalidRow, 0)
	for _, item := range worklogsIn {
		if !sameUser(item.Author, user) {
			continue
		}
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			invalid = append(invalid, model.InvalidRow{
				SourceRowID:  item.ID,
				ReasonCode:   "invalid_started_at",
				ReasonDetail: "Jira worklog started time is invalid",
				IssueKey:     issueKey,
			})
			continue
		}
		startedAt = startedAt.UTC()
		if startedAt.Before(windowFrom.UTC()) || startedAt.After(windowTo.UTC()) {
			continue
		}
		if item.TimeSpentSeconds <= 0 {
			invalid = append(invalid, model.InvalidRow{
				SourceRowID:  item.ID,
				ReasonCode:   "invalid_duration",
				ReasonDetail: "Jira worklog duration must be positive",
				IssueKey:     issueKey,
			})
			continue
		}
		description, err := worklogs.NormalizeDescription(commentText(item.Comment))
		if err != nil {
			invalid = append(invalid, model.InvalidRow{
				SourceRowID:  item.ID,
				ReasonCode:   "invalid_description",
				ReasonDetail: err.Error(),
				Description:  commentText(item.Comment),
				IssueKey:     issueKey,
			})
			continue
		}
		valid = append(valid, model.Row{
			IssueKey:        issueKey,
			StartedAtUTC:    startedAt,
			DurationSeconds: item.TimeSpentSeconds,
			Description:     description,
			SourceRowID:     item.ID,
		})
	}
	return valid, invalid
}

func ReportingDescription(sourceIssueKey, description string) string {
	prefix := sourceIssueKey + " | "
	if strings.HasPrefix(description, prefix) {
		return description
	}
	return prefix + description
}

func commentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if body, ok := typed["body"].(string); ok {
			return body
		}
		if content, ok := typed["content"].([]any); ok {
			parts := make([]string, 0)
			for _, raw := range content {
				node, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				collectADFText(node, &parts)
			}
			return strings.TrimSpace(strings.Join(parts, " "))
		}
	}
	return ""
}

func collectADFText(node map[string]any, parts *[]string) {
	if text, ok := node["text"].(string); ok && text != "" {
		*parts = append(*parts, text)
	}
	content, ok := node["content"].([]any)
	if !ok {
		return
	}
	for _, raw := range content {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		collectADFText(child, parts)
	}
}

func sameUser(author WorklogUser, user User) bool {
	switch {
	case author.AccountID != "" && user.AccountID != "":
		return author.AccountID == user.AccountID
	case author.Key != "" && user.Key != "":
		return author.Key == user.Key
	default:
		return author.Name != "" && author.Name == user.Name
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

func (c *Client) doJSON(ctx context.Context, method, route string, body any, target any) error {
	return c.do(ctx, method, route, body, target)
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
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, &RequestError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	return resp, nil
}

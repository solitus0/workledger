package clockify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/solitus0/workledger/internal/reconcile/model"
	"github.com/solitus0/workledger/internal/worklogs"
)

var BaseURL = "https://api.clockify.me/api"

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type RequestError struct {
	StatusCode int
	Status     string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("clockify request failed: %s", e.Status)
}

type User struct {
	ID               string `json:"id"`
	ActiveWorkspace  string `json:"activeWorkspace"`
	DefaultWorkspace string `json:"defaultWorkspace"`
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TimeInterval struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type TimeEntry struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"projectId"`
	Description  string       `json:"description"`
	TagIDs       []string     `json:"tagIds"`
	Tags         []Tag        `json:"tags"`
	TimeInterval TimeInterval `json:"timeInterval"`
}

type CandidateRow = model.Row
type InvalidRow = model.InvalidRow

func NewClient(apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(BaseURL, "/"),
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	if err := c.get(ctx, "/v1/user", nil, &user); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c *Client) ListUserTimeEntries(ctx context.Context, workspaceID, userID string, start, end time.Time) ([]TimeEntry, error) {
	items := make([]TimeEntry, 0)
	page := 1
	for {
		query := url.Values{}
		query.Set("start", start.UTC().Format(time.RFC3339))
		query.Set("end", end.UTC().Format(time.RFC3339))
		query.Set("page-size", "100")
		query.Set("page", strconv.Itoa(page))

		route := path.Join("/v1/workspaces", workspaceID, "user", userID, "time-entries")
		var batch []TimeEntry
		resp, err := c.getResponse(ctx, route, query)
		if err != nil {
			return nil, err
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		items = append(items, batch...)
		if strings.EqualFold(resp.Header.Get("Last-Page"), "true") {
			break
		}
		page++
	}
	return items, nil
}

func (c *Client) ListTags(ctx context.Context, workspaceID string) (map[string]Tag, error) {
	items := map[string]Tag{}
	page := 1
	for {
		query := url.Values{}
		query.Set("page-size", "200")
		query.Set("page", strconv.Itoa(page))

		route := path.Join("/v1/workspaces", workspaceID, "tags")
		var batch []Tag
		resp, err := c.getResponse(ctx, route, query)
		if err != nil {
			return nil, err
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		for _, item := range batch {
			items[item.ID] = item
		}
		if strings.EqualFold(resp.Header.Get("Last-Page"), "true") || len(batch) == 0 {
			break
		}
		page++
	}
	return items, nil
}

func (c *Client) ListProjects(ctx context.Context, workspaceID string) ([]Project, error) {
	items := make([]Project, 0)
	page := 1
	for {
		query := url.Values{}
		query.Set("page-size", "200")
		query.Set("page", strconv.Itoa(page))

		route := path.Join("/v1/workspaces", workspaceID, "projects")
		var batch []Project
		resp, err := c.getResponse(ctx, route, query)
		if err != nil {
			return nil, err
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		items = append(items, batch...)
		if strings.EqualFold(resp.Header.Get("Last-Page"), "true") || len(batch) == 0 {
			break
		}
		page++
	}

	return items, nil
}

func (c *Client) CreateTag(ctx context.Context, workspaceID, name string) (Tag, error) {
	payload := map[string]string{"name": name}
	var tag Tag
	if err := c.doJSON(ctx, http.MethodPost, path.Join("/v1/workspaces", workspaceID, "tags"), payload, &tag); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

func (c *Client) CreateTimeEntry(ctx context.Context, workspaceID string, row CandidateRow, projectID string, tagIDs []string) (TimeEntry, error) {
	payload := map[string]any{
		"start":       row.StartedAtUTC.UTC().Format(time.RFC3339),
		"end":         row.StartedAtUTC.UTC().Add(time.Duration(row.DurationSeconds) * time.Second).Format(time.RFC3339),
		"description": row.Description,
		"projectId":   projectID,
		"tagIds":      tagIDs,
	}
	var entry TimeEntry
	if err := c.doJSON(ctx, http.MethodPost, path.Join("/v1/workspaces", workspaceID, "time-entries"), payload, &entry); err != nil {
		return TimeEntry{}, err
	}
	return entry, nil
}

func (c *Client) DeleteTimeEntry(ctx context.Context, workspaceID, entryID string) error {
	return c.do(ctx, http.MethodDelete, path.Join("/v1/workspaces", workspaceID, "time-entries", entryID), nil, nil)
}

func NormalizeEntries(entries []TimeEntry, tagsByID map[string]Tag) ([]model.Row, []model.InvalidRow) {
	valid := make([]model.Row, 0, len(entries))
	invalid := make([]model.InvalidRow, 0)

	for _, entry := range entries {
		if entry.TimeInterval.Start == "" || entry.TimeInterval.End == "" {
			continue
		}

		startedAt, err := time.Parse(time.RFC3339, entry.TimeInterval.Start)
		if err != nil {
			invalid = append(invalid, invalidRow(entry, "invalid_started_at", "Clockify start time is invalid"))
			continue
		}
		endedAt, err := time.Parse(time.RFC3339, entry.TimeInterval.End)
		if err != nil {
			invalid = append(invalid, invalidRow(entry, "invalid_ended_at", "Clockify end time is invalid"))
			continue
		}
		if !endedAt.After(startedAt) {
			invalid = append(invalid, invalidRow(entry, "invalid_duration", "Clockify entry duration must be positive"))
			continue
		}

		resolvedTags := resolveTags(entry, tagsByID)
		issueTags := extractIssueTags(resolvedTags)
		if len(issueTags) != 1 {
			reasonCode := "missing_issue_key_tag"
			reasonDetail := "Clockify entry must contain exactly one exact issue-key tag"
			if len(issueTags) > 1 {
				reasonCode = "ambiguous_issue_key_tag"
			}
			invalid = append(invalid, invalidRow(entry, reasonCode, reasonDetail))
			continue
		}

		description, err := worklogs.NormalizeDescription(entry.Description)
		if err != nil {
			invalid = append(invalid, invalidRow(entry, "invalid_description", err.Error()))
			continue
		}

		valid = append(valid, model.Row{
			IssueKey:        issueTags[0],
			StartedAtUTC:    startedAt.UTC(),
			DurationSeconds: int(endedAt.Sub(startedAt).Seconds()),
			Description:     description,
			SourceRowID:     entry.ID,
			ProjectID:       entry.ProjectID,
		})
	}

	return valid, invalid
}

func extractIssueTags(tags []Tag) []string {
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		if worklogs.IsValidIssueKey(tag.Name) {
			items = append(items, tag.Name)
		}
	}
	return items
}

func resolveTags(entry TimeEntry, tagsByID map[string]Tag) []Tag {
	if len(entry.Tags) > 0 {
		return entry.Tags
	}

	items := make([]Tag, 0, len(entry.TagIDs))
	for _, id := range entry.TagIDs {
		tag, ok := tagsByID[id]
		if !ok {
			continue
		}
		items = append(items, tag)
	}
	return items
}

func EntryIssueTags(entry TimeEntry, tagsByID map[string]Tag) []string {
	return extractIssueTags(resolveTags(entry, tagsByID))
}

func invalidRow(entry TimeEntry, code, detail string) model.InvalidRow {
	tags := make([]string, 0, len(entry.Tags))
	for _, tag := range entry.Tags {
		tags = append(tags, tag.Name)
	}
	return model.InvalidRow{
		SourceRowID:  entry.ID,
		ReasonCode:   code,
		ReasonDetail: detail,
		Description:  entry.Description,
		Tags:         tags,
	}
}

func (c *Client) get(ctx context.Context, route string, query url.Values, target any) error {
	resp, err := c.getResponse(ctx, route, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) getResponse(ctx context.Context, route string, query url.Values) (*http.Response, error) {
	return c.request(ctx, http.MethodGet, route, query, nil)
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

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			base := time.Duration(1<<(attempt-1)) * time.Second
			jitter := time.Duration(rand.Int63n(int64(base) + 1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(base + jitter):
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Api-Key", c.apiKey)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			lastErr = &RequestError{StatusCode: resp.StatusCode, Status: resp.Status}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return nil, &RequestError{StatusCode: resp.StatusCode, Status: resp.Status}
		}
		return resp, nil
	}
	return nil, lastErr
}

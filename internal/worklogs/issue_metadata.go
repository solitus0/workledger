package worklogs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Issue identity and optional local fields are independent of the remote cache.
const issueCatalogSQL = `WITH metadata AS (
    SELECT d.issue_key, d.title, d.epic, d.max_estimate_seconds AS manual_estimate,
           c.max_estimate_seconds AS cached_estimate,
           c.source_adapter_family, c.source_adapter_instance, c.refreshed_at,
           d.created_at, d.updated_at,
           MAX(COALESCE(d.updated_at, ''), COALESCE(c.refreshed_at, '')) AS recent_at
    FROM issues d
    LEFT JOIN issue_metadata c ON c.issue_key = d.issue_key
) `

const issueMetadataFields = `SELECT m.issue_key, m.title, m.epic, m.manual_estimate, m.cached_estimate,
    m.source_adapter_family, m.source_adapter_instance, m.refreshed_at, m.created_at, m.updated_at, `

func (s *Service) LookupIssueMetadata(ctx context.Context, issueKeys []string) (map[string]IssueMetadata, error) {
	keys := uniqueIssueKeys(issueKeys)
	items := make(map[string]IssueMetadata, len(keys))
	for start := 0; start < len(keys); start += sqliteQueryBatchSize {
		batch := keys[start:min(start+sqliteQueryBatchSize, len(keys))]
		args := make([]any, len(batch))
		for i, key := range batch {
			args[i] = key
		}
		// A bounded lookup sums only requested issue histories through the issue index.
		rows, err := s.store.DB().QueryContext(ctx, issueCatalogSQL+issueMetadataFields+
			`(SELECT COALESCE(SUM(duration_seconds), 0) FROM worklogs w WHERE w.issue_key = m.issue_key)
			FROM metadata m WHERE m.issue_key IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		batchItems, err := readIssueMetadata(rows)
		if err != nil {
			return nil, err
		}
		for _, item := range batchItems {
			items[item.IssueKey] = item
		}
	}
	return items, nil
}

// SearchIssues lists durable issues and their lifetime active local work.
func (s *Service) SearchIssues(ctx context.Context, issuePrefix, search, epic string) ([]IssueMetadata, error) {
	if issuePrefix != "" && !IsValidIssuePrefix(issuePrefix) {
		return nil, fmt.Errorf("%w: issue-prefix must be a project key", ErrValidation)
	}
	search = strings.TrimSpace(search)
	epic = strings.TrimSpace(epic)
	// The complete catalog aggregates active work once, including dates outside the visible worklog window.
	rows, err := s.store.DB().QueryContext(ctx, issueCatalogSQL+`, logged AS (
		SELECT issue_key, SUM(duration_seconds) AS logged_seconds FROM worklogs GROUP BY issue_key
	) `+issueMetadataFields+`COALESCE(l.logged_seconds, 0) FROM metadata m LEFT JOIN logged l ON l.issue_key=m.issue_key
		WHERE (? = '' OR m.issue_key LIKE ? ESCAPE '\')
		AND (? = '' OR m.epic = ?)
		ORDER BY substr(m.issue_key, 1, instr(m.issue_key, '-')), length(substr(m.issue_key, instr(m.issue_key, '-') + 1)), m.issue_key`,
		issuePrefix, issuePrefix+"-%", epic, epic)
	if err != nil {
		return nil, err
	}
	items, err := readIssueMetadata(rows)
	if err != nil || search == "" {
		return items, err
	}
	// SQLite NOCASE only folds ASCII. Go's Unicode lowercase mapping keeps
	// local title and epic substring search consistent with interactive search.
	query := strings.ToLower(search)
	filtered := items[:0]
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.IssueKey), query) || strings.Contains(strings.ToLower(item.Title), query) || strings.Contains(strings.ToLower(item.Epic), query) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func readIssueMetadata(rows *sql.Rows) ([]IssueMetadata, error) {
	defer rows.Close()
	items := []IssueMetadata{}
	for rows.Next() {
		var item IssueMetadata
		var title, epic, family, instance, refreshed, created, updated sql.NullString
		var manual, cached sql.NullInt64
		if err := rows.Scan(&item.IssueKey, &title, &epic, &manual, &cached, &family, &instance, &refreshed, &created, &updated, &item.LoggedSeconds); err != nil {
			return nil, err
		}
		item.Title, item.SourceAdapterFamily, item.SourceAdapterInst = title.String, family.String, instance.String
		item.Epic = epic.String
		if manual.Valid {
			item.ManualMaxEstimateSeconds = &manual.Int64
		}
		if cached.Valid {
			item.CachedMaxEstimateSeconds = &cached.Int64
		}
		item.EstimateSource = "none"
		if cached.Valid && cached.Int64 > 0 {
			item.MaxEstimateSeconds, item.EstimateSource = &cached.Int64, "jira"
		}
		if manual.Valid {
			item.MaxEstimateSeconds, item.EstimateSource = &manual.Int64, "manual"
		}
		if item.MaxEstimateSeconds != nil {
			remaining := max(*item.MaxEstimateSeconds-item.LoggedSeconds, 0)
			item.RemainingSeconds = &remaining
			item.OverrunSeconds = max(item.LoggedSeconds-*item.MaxEstimateSeconds, 0)
		}
		for _, value := range []struct {
			raw    sql.NullString
			target *time.Time
		}{{refreshed, &item.RefreshedAt}, {created, &item.CreatedAt}, {updated, &item.UpdatedAt}} {
			if value.raw.Valid {
				parsed, err := time.Parse(time.RFC3339, value.raw.String)
				if err != nil {
					return nil, err
				}
				*value.target = parsed.UTC()
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Presence flags distinguish omitted fields from an explicit null (clear).
type IssueMetadataPatch struct {
	IssueKey           string
	Title              *string
	TitleSet           bool
	Epic               *string
	EpicSet            bool
	MaxEstimateSeconds *int64
	MaxEstimateSet     bool
}

func (p *IssueMetadataPatch) UnmarshalJSON(data []byte) error {
	var raw struct {
		IssueKey string          `json:"issue_key"`
		Title    json.RawMessage `json:"title"`
		Epic     json.RawMessage `json:"epic"`
		Estimate json.RawMessage `json:"max_estimate_seconds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	*p = IssueMetadataPatch{IssueKey: raw.IssueKey, TitleSet: raw.Title != nil, EpicSet: raw.Epic != nil, MaxEstimateSet: raw.Estimate != nil}
	if raw.Title != nil {
		if err := json.Unmarshal(raw.Title, &p.Title); err != nil {
			return err
		}
	}
	if raw.Estimate != nil {
		if err := json.Unmarshal(raw.Estimate, &p.MaxEstimateSeconds); err != nil {
			return err
		}
	}
	if raw.Epic != nil {
		if err := json.Unmarshal(raw.Epic, &p.Epic); err != nil {
			return err
		}
	}
	return nil
}

func (p IssueMetadataPatch) MarshalJSON() ([]byte, error) {
	item := map[string]any{"issue_key": p.IssueKey}
	if p.TitleSet {
		item["title"] = p.Title
	}
	if p.MaxEstimateSet {
		item["max_estimate_seconds"] = p.MaxEstimateSeconds
	}
	if p.EpicSet {
		item["epic"] = p.Epic
	}
	return json.Marshal(item)
}

func ParseIssueMetadataPatches(data []byte) ([]IssueMetadataPatch, error) {
	var patches []IssueMetadataPatch
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&patches); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: expected one JSON array", ErrValidation)
	}
	if err := ValidateIssueMetadataPatches(patches); err != nil {
		return nil, err
	}
	return patches, nil
}

func ValidateIssueMetadataPatches(patches []IssueMetadataPatch) error {
	if len(patches) == 0 {
		return fmt.Errorf("%w: metadata patches must be a non-empty array", ErrValidation)
	}
	seen := map[string]bool{}
	for _, patch := range patches {
		if !IsValidIssueKey(patch.IssueKey) {
			return fmt.Errorf("%w: issue must match <PROJECTKEY>-<NUMBER>", ErrValidation)
		}
		if seen[patch.IssueKey] {
			return fmt.Errorf("%w: duplicate issue %s", ErrValidation, patch.IssueKey)
		}
		seen[patch.IssueKey] = true
		if patch.Title != nil && (strings.TrimSpace(*patch.Title) == "" || strings.ContainsFunc(*patch.Title, unicode.IsControl) || strings.ContainsAny(*patch.Title, "\u2028\u2029")) {
			return fmt.Errorf("%w: title must be non-empty single-line text", ErrValidation)
		}
		if patch.Epic != nil && (strings.TrimSpace(*patch.Epic) == "" || strings.ContainsFunc(*patch.Epic, unicode.IsControl) || strings.ContainsAny(*patch.Epic, "\u2028\u2029")) {
			return fmt.Errorf("%w: epic must be non-empty single-line text", ErrValidation)
		}
		if patch.MaxEstimateSeconds != nil && *patch.MaxEstimateSeconds <= 0 {
			return fmt.Errorf("%w: maximum estimate must be positive", ErrValidation)
		}
	}
	return nil
}

func ParseMaxEstimate(value string) (int64, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return 0, fmt.Errorf("%w: maximum estimate must be a positive whole-second duration (e.g. 16h)", ErrValidation)
	}
	return int64(duration / time.Second), nil
}

func (s *Service) ApplyIssueMetadata(ctx context.Context, patches []IssueMetadataPatch, dry bool) error {
	if err := ValidateIssueMetadataPatches(patches); err != nil {
		return err
	}
	if dry {
		return ctx.Err()
	}
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	updatedAt := s.now().UTC().Format(time.RFC3339)
	upsert, err := tx.PrepareContext(ctx, `INSERT INTO issues(issue_key, title, epic, max_estimate_seconds, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(issue_key) DO UPDATE SET
        title = CASE WHEN ? THEN excluded.title ELSE issues.title END,
        epic = CASE WHEN ? THEN excluded.epic ELSE issues.epic END,
        max_estimate_seconds = CASE WHEN ? THEN excluded.max_estimate_seconds ELSE issues.max_estimate_seconds END,
        updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer upsert.Close()
	for _, patch := range patches {
		var title, epic any
		if patch.Title != nil {
			title = strings.TrimSpace(*patch.Title)
		}
		if patch.Epic != nil {
			epic = strings.TrimSpace(*patch.Epic)
		}
		_, err = upsert.ExecContext(ctx, patch.IssueKey, title, epic, patch.MaxEstimateSeconds, updatedAt, updatedAt, patch.TitleSet, patch.EpicSet, patch.MaxEstimateSet)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type IssueSummary struct {
	IssueCount            int
	EstimatedIssueCount   int
	UnestimatedIssueCount int
	LoggedSeconds         int64
	EstimatedSeconds      int64
	RemainingSeconds      int64
	OverrunSeconds        int64
}

type EpicSummary struct {
	Epic string
	IssueSummary
}

// SummarizeIssues keeps overruns separate so one issue cannot consume another's remaining estimate.
func SummarizeIssues(items []IssueMetadata) IssueSummary {
	var summary IssueSummary
	for _, item := range items {
		summary.IssueCount++
		summary.LoggedSeconds += item.LoggedSeconds
		if item.MaxEstimateSeconds == nil {
			summary.UnestimatedIssueCount++
			continue
		}
		summary.EstimatedIssueCount++
		summary.EstimatedSeconds += *item.MaxEstimateSeconds
		summary.RemainingSeconds += max(*item.MaxEstimateSeconds-item.LoggedSeconds, 0)
		summary.OverrunSeconds += max(item.LoggedSeconds-*item.MaxEstimateSeconds, 0)
	}
	return summary
}

// GroupIssueEpics includes the empty label as the ungrouped issue bucket.
func GroupIssueEpics(items []IssueMetadata) []EpicSummary {
	groups := map[string][]IssueMetadata{}
	for _, item := range items {
		groups[item.Epic] = append(groups[item.Epic], item)
	}
	labels := make([]string, 0, len(groups))
	for epic := range groups {
		labels = append(labels, epic)
	}
	slices.SortFunc(labels, func(a, b string) int {
		if a == "" && b != "" {
			return 1
		}
		if b == "" && a != "" {
			return -1
		}
		return strings.Compare(a, b)
	})
	summaries := make([]EpicSummary, 0, len(labels))
	for _, epic := range labels {
		summaries = append(summaries, EpicSummary{Epic: epic, IssueSummary: SummarizeIssues(groups[epic])})
	}
	return summaries
}

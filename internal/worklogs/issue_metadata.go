package worklogs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

// The catalog merges independent local decorations and the remote estimate cache.
const issueCatalogSQL = `WITH metadata_keys AS (
    SELECT issue_key FROM issue_metadata UNION SELECT issue_key FROM issue_decorations
), metadata AS (
    SELECT k.issue_key, d.title, d.max_estimate_seconds AS manual_estimate,
           c.max_estimate_seconds AS cached_estimate,
           c.source_adapter_family, c.source_adapter_instance, c.refreshed_at,
           d.updated_at,
           MAX(COALESCE(d.updated_at, ''), COALESCE(c.refreshed_at, '')) AS recent_at
    FROM metadata_keys k
    LEFT JOIN issue_decorations d ON d.issue_key = k.issue_key
    LEFT JOIN issue_metadata c ON c.issue_key = k.issue_key
) `

const issueMetadataSelect = `SELECT issue_key, title, manual_estimate, cached_estimate,
    source_adapter_family, source_adapter_instance, refreshed_at, updated_at FROM metadata `

func (s *Service) LookupIssueMetadata(ctx context.Context, issueKeys []string) (map[string]IssueMetadata, error) {
	keys := uniqueIssueKeys(issueKeys)
	items := make(map[string]IssueMetadata, len(keys))
	for start := 0; start < len(keys); start += sqliteQueryBatchSize {
		batch := keys[start:min(start+sqliteQueryBatchSize, len(keys))]
		args := make([]any, len(batch))
		for i, key := range batch {
			args[i] = key
		}
		rows, err := s.store.DB().QueryContext(ctx, issueCatalogSQL+issueMetadataSelect+
			`WHERE issue_key IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...)
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

// SearchIssueMetadata lists the whole local catalog without deriving keys from worklogs.
func (s *Service) SearchIssueMetadata(ctx context.Context, issuePrefix, titleSearch string) ([]IssueMetadata, error) {
	if issuePrefix != "" && !IsValidIssuePrefix(issuePrefix) {
		return nil, fmt.Errorf("%w: issue-prefix must be a project key", ErrValidation)
	}
	rows, err := s.store.DB().QueryContext(ctx, issueCatalogSQL+issueMetadataSelect+
		`WHERE (? = '' OR issue_key LIKE ? ESCAPE '\')
		AND (? = '' OR title LIKE ? ESCAPE '\' COLLATE NOCASE) ORDER BY issue_key`,
		issuePrefix, issuePrefix+"-%", titleSearch, literalSubstringPattern(titleSearch))
	if err != nil {
		return nil, err
	}
	return readIssueMetadata(rows)
}

func readIssueMetadata(rows *sql.Rows) ([]IssueMetadata, error) {
	defer rows.Close()
	items := []IssueMetadata{}
	for rows.Next() {
		var item IssueMetadata
		var title, family, instance, refreshed, updated sql.NullString
		var manual, cached sql.NullInt64
		if err := rows.Scan(&item.IssueKey, &title, &manual, &cached, &family, &instance, &refreshed, &updated); err != nil {
			return nil, err
		}
		item.Title, item.SourceAdapterFamily, item.SourceAdapterInst = title.String, family.String, instance.String
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
		for _, value := range []struct {
			raw    sql.NullString
			target *time.Time
		}{{refreshed, &item.RefreshedAt}, {updated, &item.UpdatedAt}} {
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
	MaxEstimateSeconds *int64
	MaxEstimateSet     bool
}

func (p *IssueMetadataPatch) UnmarshalJSON(data []byte) error {
	var raw struct {
		IssueKey string          `json:"issue_key"`
		Title    json.RawMessage `json:"title"`
		Estimate json.RawMessage `json:"max_estimate_seconds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	*p = IssueMetadataPatch{IssueKey: raw.IssueKey, TitleSet: raw.Title != nil, MaxEstimateSet: raw.Estimate != nil}
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
		if !patch.TitleSet && !patch.MaxEstimateSet {
			return fmt.Errorf("%w: supply at least one metadata field", ErrValidation)
		}
		if patch.Title != nil && (strings.TrimSpace(*patch.Title) == "" || strings.ContainsFunc(*patch.Title, unicode.IsControl)) {
			return fmt.Errorf("%w: title must be non-empty single-line text", ErrValidation)
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
	upsert, err := tx.PrepareContext(ctx, `INSERT INTO issue_decorations(issue_key, title, max_estimate_seconds, updated_at)
        VALUES (?, ?, ?, ?) ON CONFLICT(issue_key) DO UPDATE SET
        title = CASE WHEN ? THEN excluded.title ELSE issue_decorations.title END,
        max_estimate_seconds = CASE WHEN ? THEN excluded.max_estimate_seconds ELSE issue_decorations.max_estimate_seconds END,
        updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer upsert.Close()
	clearEmpty, err := tx.PrepareContext(ctx, `DELETE FROM issue_decorations WHERE issue_key = ? AND title IS NULL AND max_estimate_seconds IS NULL`)
	if err != nil {
		return err
	}
	defer clearEmpty.Close()
	for _, patch := range patches {
		var title any
		if patch.Title != nil {
			title = strings.TrimSpace(*patch.Title)
		}
		_, err = upsert.ExecContext(ctx, patch.IssueKey, title, patch.MaxEstimateSeconds, updatedAt, patch.TitleSet, patch.MaxEstimateSet)
		if err != nil {
			return err
		}
		if _, err := clearEmpty.ExecContext(ctx, patch.IssueKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}

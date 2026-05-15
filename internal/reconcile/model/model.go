package model

import "time"

type Row struct {
	IssueKey        string    `json:"issue_key"`
	StartedAtUTC    time.Time `json:"started_at_utc"`
	DurationSeconds int       `json:"duration_seconds"`
	Description     string    `json:"description"`
	SourceRowID     string    `json:"source_row_id"`
	ProjectID       string    `json:"-"`
}

type InvalidRow struct {
	SourceRowID  string   `json:"source_row_id"`
	ReasonCode   string   `json:"reason_code"`
	ReasonDetail string   `json:"reason_detail"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags,omitempty"`
	IssueKey     string   `json:"issue_key,omitempty"`
}

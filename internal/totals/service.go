package totals

import (
	"context"
	"strings"
	"time"

	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	jiracloudadapter "github.com/solitus0/workledger/internal/adapter/jiracloud"
	jiradataadapter "github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/reconcile/model"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

type DayResult struct {
	Date               string
	State              string
	LocalTotalSeconds  int
	RemoteTotalSeconds int
	DeltaSeconds       int
}

type Result struct {
	State                      string
	LocalTotalSeconds          int
	RemoteTotalSeconds         int
	DeltaSeconds               int
	RunningRemoteEntryDetected bool
	Days                       []DayResult
}

type Service struct {
	store              *sqlitestore.Store
	newClockifyClient  func(cfg config.ClockifyConfig) clockifyClient
	newJiraCloudClient func(cfg config.JiraCloudInstance) jiraCloudClient
	newJiraDataClient  func(cfg config.JiraDataCenterInstance) jiraDataClient
}

type localEntry struct {
	start time.Time
	end   time.Time
}

func NewService(store *sqlitestore.Store) *Service {
	return &Service{
		store: store,
		newClockifyClient: func(cfg config.ClockifyConfig) clockifyClient {
			return clockifyadapter.NewClient(cfg.Auth.APIKey)
		},
		newJiraCloudClient: func(cfg config.JiraCloudInstance) jiraCloudClient {
			return jiracloudadapter.NewClient(cfg.BaseURL, cfg.Auth.Email, cfg.Auth.Token)
		},
		newJiraDataClient: func(cfg config.JiraDataCenterInstance) jiraDataClient {
			return jiradataadapter.NewClient(cfg.BaseURL, cfg.Auth.Bearer.Token)
		},
	}
}

func (s *Service) SummarizeLocal(ctx context.Context, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time) (Result, error) {
	return s.SummarizeLocalFiltered(ctx, cfg, windowFromUTC, windowToUTC, nil, nil)
}

func (s *Service) SummarizeLocalFiltered(ctx context.Context, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, issuePrefixes []string, excludedIssueKeys map[string]struct{}) (Result, error) {
	localEntries, err := s.loadLocalEntries(ctx, issuePrefixes, excludedIssueKeys)
	if err != nil {
		return Result{}, err
	}

	windowEndExclusive := windowToUTC.Add(time.Second)
	return summarizeLocalEntries(localEntries, windowFromUTC, windowEndExclusive, cfg.Location), nil
}

func (s *Service) CompareClockify(ctx context.Context, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, entries []clockifyadapter.TimeEntry) (Result, error) {
	localEntries, err := s.loadLocalEntries(ctx, nil, nil)
	if err != nil {
		return Result{}, err
	}

	windowEndExclusive := windowToUTC.Add(time.Second)
	runningDays := map[string]struct{}{}

	remoteEntries := make([]localEntry, 0, len(entries))
	for _, entry := range entries {
		start, err := time.Parse(time.RFC3339, entry.TimeInterval.Start)
		if err != nil {
			continue
		}
		if entry.TimeInterval.End == "" {
			markRunningDays(runningDays, start.UTC(), windowFromUTC, windowEndExclusive, cfg.Location)
			continue
		}
		end, err := time.Parse(time.RFC3339, entry.TimeInterval.End)
		if err != nil {
			continue
		}
		remoteEntries = append(remoteEntries, localEntry{
			start: start.UTC(),
			end:   end.UTC(),
		})
	}

	result := compareFixedDurationEntries(localEntries, remoteEntries, windowFromUTC, windowEndExclusive, cfg.Location, runningDays)
	runningDetected := len(runningDays) > 0
	if runningDetected {
		result.State = "indeterminate"
	}
	result.RunningRemoteEntryDetected = runningDetected
	return result, nil
}

func (s *Service) CompareJiraData(ctx context.Context, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, rows []model.Row, issuePrefixes []string) (Result, error) {
	return s.CompareJiraDataWithExclusions(ctx, cfg, windowFromUTC, windowToUTC, rows, issuePrefixes, nil)
}

func (s *Service) CompareJiraDataWithExclusions(ctx context.Context, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, rows []model.Row, issuePrefixes []string, excludedIssueKeys map[string]struct{}) (Result, error) {
	localEntries, err := s.loadLocalEntries(ctx, issuePrefixes, excludedIssueKeys)
	if err != nil {
		return Result{}, err
	}

	return compareRowsWithLocalEntries(localEntries, rows, windowFromUTC, windowToUTC, cfg.Location), nil
}

func (s *Service) CompareRowsWithLocalScope(ctx context.Context, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, rows []model.Row, issuePrefixes []string, excludedIssueKeys map[string]struct{}) (Result, error) {
	localEntries, err := s.loadLocalEntries(ctx, issuePrefixes, excludedIssueKeys)
	if err != nil {
		return Result{}, err
	}

	return compareRowsWithLocalEntries(localEntries, rows, windowFromUTC, windowToUTC, cfg.Location), nil
}

func compareRowsWithLocalEntries(localEntries []localEntry, rows []model.Row, windowFromUTC, windowToUTC time.Time, location *time.Location) Result {
	windowEndExclusive := windowToUTC.Add(time.Second)

	remoteEntries := make([]localEntry, 0, len(rows))
	for _, row := range rows {
		remoteEntries = append(remoteEntries, localEntry{
			start: row.StartedAtUTC.UTC(),
			end:   row.StartedAtUTC.UTC().Add(time.Duration(row.DurationSeconds) * time.Second),
		})
	}

	return compareFixedDurationEntries(localEntries, remoteEntries, windowFromUTC, windowEndExclusive, location, nil)
}

func summarizeLocalEntries(localEntries []localEntry, windowFromUTC, windowEndExclusiveUTC time.Time, location *time.Location) Result {
	return compareFixedDurationEntries(localEntries, localEntries, windowFromUTC, windowEndExclusiveUTC, location, nil)
}

func (s *Service) loadLocalEntries(ctx context.Context, issuePrefixes []string, excludedIssueKeys map[string]struct{}) ([]localEntry, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT issue_key, started_at_utc, duration_seconds FROM worklogs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]localEntry, 0)
	for rows.Next() {
		var issueKey string
		var startedAt string
		var durationSeconds int
		if err := rows.Scan(&issueKey, &startedAt, &durationSeconds); err != nil {
			return nil, err
		}
		if _, excluded := excludedIssueKeys[issueKey]; excluded {
			continue
		}
		if !matchesIssuePrefixes(issueKey, issuePrefixes) {
			continue
		}
		start, err := time.Parse(time.RFC3339, startedAt)
		if err != nil {
			return nil, err
		}
		items = append(items, localEntry{
			start: start.UTC(),
			end:   start.UTC().Add(time.Duration(durationSeconds) * time.Second),
		})
	}

	return items, rows.Err()
}

func matchesIssuePrefixes(issueKey string, issuePrefixes []string) bool {
	if len(issuePrefixes) == 0 {
		return true
	}
	for _, prefix := range issuePrefixes {
		if strings.HasPrefix(issueKey, prefix) {
			return true
		}
	}
	return false
}

func compareFixedDurationEntries(localEntries, remoteEntries []localEntry, windowFromUTC, windowEndExclusiveUTC time.Time, location *time.Location, runningDays map[string]struct{}) Result {
	localByDay := map[string]int{}
	remoteByDay := map[string]int{}
	for _, entry := range localEntries {
		addOverlapByDay(localByDay, entry.start, entry.end, windowFromUTC, windowEndExclusiveUTC, location)
	}
	for _, entry := range remoteEntries {
		addOverlapByDay(remoteByDay, entry.start, entry.end, windowFromUTC, windowEndExclusiveUTC, location)
	}

	keys := orderedDayKeys(localByDay, remoteByDay, runningDays)
	days := make([]DayResult, 0, len(keys))
	localTotal := 0
	remoteTotal := 0
	state := "match"

	for _, key := range keys {
		localSeconds := localByDay[key]
		remoteSeconds := remoteByDay[key]
		delta := localSeconds - remoteSeconds
		dayState := "match"
		if _, ok := runningDays[key]; ok {
			dayState = "indeterminate"
		} else if delta != 0 {
			dayState = "mismatch"
		}
		if dayState == "mismatch" && state == "match" {
			state = "mismatch"
		}
		days = append(days, DayResult{
			Date:               key,
			State:              dayState,
			LocalTotalSeconds:  localSeconds,
			RemoteTotalSeconds: remoteSeconds,
			DeltaSeconds:       delta,
		})
		localTotal += localSeconds
		remoteTotal += remoteSeconds
	}

	return Result{
		State:              state,
		LocalTotalSeconds:  localTotal,
		RemoteTotalSeconds: remoteTotal,
		DeltaSeconds:       localTotal - remoteTotal,
		Days:               days,
	}
}

func addOverlapByDay(target map[string]int, startUTC, endUTC, windowFromUTC, windowEndExclusiveUTC time.Time, location *time.Location) {
	if !startUTC.Before(windowEndExclusiveUTC) || !endUTC.After(windowFromUTC) {
		return
	}

	overlapStart := maxTime(startUTC, windowFromUTC)
	overlapEnd := minTime(endUTC, windowEndExclusiveUTC)
	cursor := overlapStart
	for cursor.Before(overlapEnd) {
		local := cursor.In(location)
		nextDayLocal := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location)
		sliceEnd := minTime(overlapEnd, nextDayLocal.UTC())
		target[local.Format("2006-01-02")] += int(sliceEnd.Sub(cursor) / time.Second)
		cursor = sliceEnd
	}
}

func markRunningDays(target map[string]struct{}, startUTC, windowFromUTC, windowEndExclusiveUTC time.Time, location *time.Location) {
	if !startUTC.Before(windowEndExclusiveUTC) {
		return
	}

	cursor := maxTime(startUTC, windowFromUTC)
	for cursor.Before(windowEndExclusiveUTC) {
		local := cursor.In(location)
		target[local.Format("2006-01-02")] = struct{}{}
		nextDayLocal := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location)
		cursor = minTime(windowEndExclusiveUTC, nextDayLocal.UTC())
	}
}

func orderedDayKeys(localByDay, remoteByDay map[string]int, runningDays map[string]struct{}) []string {
	keys := make([]string, 0, len(localByDay)+len(remoteByDay)+len(runningDays))
	seen := map[string]struct{}{}
	for key, value := range localByDay {
		if value == 0 {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key, value := range remoteByDay {
		if value == 0 {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range runningDays {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

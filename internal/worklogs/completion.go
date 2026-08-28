package worklogs

import "strings"

// ListActiveByIDPrefix returns recent active worklogs whose IDs start with the
// supplied prefix. The result is bounded for latency-sensitive advisory uses.
func (s *Service) ListActiveByIDPrefix(prefix string, limit int) ([]LocalWorklog, error) {
	if limit <= 0 {
		return []LocalWorklog{}, nil
	}

	rows, err := s.store.DB().Query(
		`SELECT id, issue_key, started_at_utc, duration_seconds, description
		 FROM worklogs
		 WHERE instr(lower(id), lower(?)) = 1
		 ORDER BY updated_at DESC, id
		 LIMIT ?`,
		strings.TrimSpace(prefix),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LocalWorklog, 0)
	for rows.Next() {
		item, err := scanWorklog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListKnownIssueKeys returns locally known issue keys from active worklogs and
// cached issue metadata. Results are case-insensitively prefix-filtered.
func (s *Service) ListKnownIssueKeys(prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}

	rows, err := s.store.DB().Query(
		`SELECT issue_key
		 FROM (
			SELECT issue_key FROM worklogs
			UNION
			SELECT issue_key FROM issue_metadata
		 )
		 WHERE instr(lower(issue_key), lower(?)) = 1
		 ORDER BY issue_key COLLATE NOCASE, issue_key
		 LIMIT ?`,
		strings.TrimSpace(prefix),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var issueKey string
		if err := rows.Scan(&issueKey); err != nil {
			return nil, err
		}
		items = append(items, issueKey)
	}
	return items, rows.Err()
}

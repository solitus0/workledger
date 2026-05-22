# Workledger command contract

Use this reference when exact command shape matters. The product specs remain the source of truth.

## Source-of-truth boundaries

- YAML config owns operator-managed configuration.
- SQLite owns active local worklogs, deleted-worklog tombstones, issue metadata, saved plans, delivery attempts, and audit events.
- Adapter data is never canonical local identity.
- Remote worklog IDs are transient adapter handles only.
- User-facing output goes to stdout. Logs, diagnostics, and progress go to stderr.
- `--output json` must stay valid JSON without mixed log lines.

## Setup and diagnostics

```text
workledger init
workledger config validate
workledger config env
workledger config env --print-export-template
workledger config env --dotenv-template
workledger config summary
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
workledger setup jira-data-center --instance dc --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
workledger setup clockify --workspace-id <workspace-id> --user-id <user-id> --api-key-env CLOCKIFY_API_KEY --project-map PROJ=Engineering
workledger doctor
workledger doctor --local
workledger doctor --env
workledger doctor --routing
workledger doctor --connectivity
workledger doctor --all
```

`config validate` checks structural config without requiring adapter connectivity. Use `doctor --connectivity` or `status` when credentials and remote reachability matter.

`doctor` local checks include local storage validation for the effective `storage.sqlite_path`, including DB-file writability when present, parent-directory writability, and SQLite sidecar creation viability.

## Routing and mapping inspection

```text
workledger routing list
workledger route explain PROJ-123
workledger clockify mappings validate
```

Use routing commands before push planning when the target adapter, instance, route profile, or Clockify project mapping is unclear.

## Local worklog inspection

```text
workledger worklogs list --today
workledger worklogs list --current-month --issue PROJ-123
workledger worklogs list --last-week --only-deleted
workledger worklogs search "reconciliation"
workledger worklogs search "reconciliation" --only-deleted
workledger worklogs context --today --output json
workledger worklogs context --from 2026-05-01 --to 2026-05-14 --issue PROJ-123 --output json
```

Rules:

- `worklogs list` requires at least one explicit time selector.
- `worklogs search <query>` searches descriptions by literal, case-insensitive substring and can run across all stored dates unless filtered.
- `worklogs context` is read-only and returns planning snapshots over selected days, booked time, free slots, collisions, quota deltas, and payload guidance.
- Use `--fields` only for list/search item shaping; it must not remove JSON `filters` or `total`.

## Local worklog mutation

```text
workledger worklogs add --issue PROJ-123 --started todayT09:00 --duration 2h --description "Implement reconciliation flow"
workledger worklogs add --issue PROJ-123 --started-utc 2026-05-14T07:00:00Z --duration 2h --description "Implement reconciliation flow"
workledger worklogs update <id> --duration 1h45m --description "Refine reconciliation flow"
workledger worklogs shift --today --issue PROJ-123 --by 15m --dry
workledger worklogs shift --today --issue PROJ-123 --by 15m
workledger worklogs delete <id>
workledger worklogs delete <id> --hard
workledger worklogs delete --today --issue PROJ-123 --dry
workledger worklogs delete --today --issue PROJ-123 --yes
workledger worklogs restore --today --issue PROJ-123 --dry
workledger worklogs restore --today --issue PROJ-123 --yes
```

Rules:

- `add` requires `--issue`, exactly one of `--started` or `--started-utc`, `--duration`, and `--description`.
- `update <id>` requires at least one patch flag and rejects both `--started` and `--started-utc` together.
- `shift` requires selectors plus `--by <GoDuration>`.
- Single delete by ID is non-interactive after validation.
- Selector-based delete requires `--yes` for execution and supports `--dry` for preview.
- Restore is selector-based only, requires a time selector, and requires exactly one of `--dry` or `--yes` for safe flows.
- `--force` may bypass duplicate or overlap rejection on supported write paths. Do not use it to hide uncertainty.
- Commands that persist local SQLite state preflight storage writability before opening a write transaction.

## Batch apply payload

`worklogs apply` mutates canonical SQLite worklogs only and applies many add operations atomically.

```json
{
  "adds": [
    {
      "issue_key": "PROJ-123",
      "started_at": "todayT09:00",
      "duration_seconds": 7200,
      "description": "Implement reconciliation flow"
    }
  ]
}
```

```text
workledger worklogs apply --stdin --dry --output json
workledger worklogs apply --stdin --output json
workledger worklogs apply --file worklogs.json --dry --output json
```

Rules:

- The payload must be one JSON object with top-level `adds`.
- `adds` must contain at least one item.
- Each add requires `issue_key`, `duration_seconds`, `description`, and exactly one of `started_at` or `started_at_utc`.
- `duration_seconds` must be positive whole seconds and satisfy the configured minimum duration.
- CLI dry-run is the authoritative payload validation step.
- A successful dry-run does not guarantee the real apply can write to the configured SQLite path in a sandboxed session.

## Metadata, status, and totals

```text
workledger issue-metadata list --issue PROJ-123
workledger issue-metadata list --current-month
workledger issue-metadata refresh --adapter=jira-cloud --field=max-estimate --current-month
workledger issue-metadata refresh --adapter=jira-data-center --field=max-estimate --issue PROJ-123
workledger status
workledger status --adapter=clockify --instance clockify
workledger status --adapter=jira-cloud --instance main
workledger totals --adapter=clockify --instance clockify --today
workledger totals --adapter=jira-cloud --instance main --current-week --details
```

Rules:

- `issue-metadata list --issue <KEY>` can run without a date selector.
- `issue-metadata refresh` requires `--field=max-estimate`, a Jira adapter family, and selected issues from `--issue` or local-worklog selectors.
- `totals` requires exactly one date window.
- Bare `totals` compares every configured adapter target. Explicit adapter totals compare one selected adapter target.
- Use `--progress=auto|bar|plain|off` on remote batch commands where progress is supported.

## Saved remote-sync plans

```text
workledger plan reconcile --push --adapter=clockify --instance clockify --today
workledger plan reconcile --pull --adapter=jira-cloud --instance main --current-week
workledger plan reconcile --push --adapter=jira-data-center --instance dc --route-profile reporting --from 2026-05-01 --to 2026-05-14
workledger plan show
workledger plan show <plan-id> --only-ready
workledger plan list --current-month
workledger plan apply <plan-id>
workledger plan retry <plan-id> --only failed
workledger plan retry <plan-id> --only uncertain
```

Rules:

- `plan reconcile` requires exactly one of `--pull` or `--push`.
- It also requires at least one scope selector from repeated `--adapter=<family>` or repeated `--instance=<name>` and exactly one date window.
- Repeated instances can include the implicit Clockify instance `clockify` and configured Jira instances.
- `plan reconcile` persists at most one saved plan per invocation after command-level validation.
- `plan reconcile` is non-destructive and has no separate dry-run mode.
- `plan show` renders saved data without new external requests.
- `plan apply` executes ready items from one saved plan.
- `plan retry` requires an explicit retry scope.

## Exit codes

| Code | Meaning |
| ---: | --- |
| 0 | Success |
| 1 | Unexpected failure |
| 2 | Validation or input failure |
| 3 | Not found |
| 4 | Authentication failure |
| 5 | External or connectivity failure |
| 6 | Partial success |

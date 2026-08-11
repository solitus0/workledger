# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `--tomorrow` as a shared date-window selector for CLI workflows, resolving to the next day in the configured local timezone.
- Semantic weekday date values from `mon` through `sun` for local timestamps and explicit date ranges, resolving inside the current local Monday-through-Sunday week.

## [0.1.8] - 2026-07-02

### Changed
- Diagnostics commands are now consolidated: `workledger config` replaces `config validate`, `config env`, and `config summary`, while `workledger status` replaces `workledger doctor`, runs config/storage/env/routing/connectivity diagnostics by default, and shows authenticated identity details on successful remote checks.
- `workledger worklogs add` automatic placement now uses explicit `--fit` and `--fill` modes instead of `--snap`; `--fit` creates one continuous worklog, `--fill` can allocate across gaps, and the session worklog creator skill now uses `--fit` for unknown exact starts.
- `workledger plan reconcile` now defaults to push, can target all configured reconcile-capable targets when no `--adapter` or `--instance` is supplied, includes non-default Jira reporting profiles automatically in bare push runs, surfaces per-profile breakdowns in reconcile output, and shows persisted route profiles in `plan show`.
- `workledger worklogs delete` now permanently removes active local worklogs, and push reconcile now discovers owned remote rows directly from the selected remote scope instead of relying on local tombstones for remote cleanup planning.

### Removed
- `workledger doctor` has been removed in favor of `workledger status`.
- `workledger tombstones` commands and tombstone-backed restore flows have been removed.

### Fixed
- `workledger plan reconcile --route-profile` now fails fast when no Jira target is selected, instead of falling through to unrelated adapters such as Clockify.
- Implicit all-target reconcile validation now includes the concrete skipped-target reasons when every configured target is invalid.
- Jira pull reconcile now filters remote worklogs to configured routed `issue_prefixes` instead of importing unrelated issues from the selected instance.
- Jira Cloud push/apply now preserves already-matched ADF worklogs instead of deleting and recreating them during replace flows.

## [0.1.7] - 2026-06-23

### Added
- `workledger trash` commands for listing, searching, and showing read-only archived worklogs removed during pull merges or remote cleanup.
- `workledger plan show` now displays saved diff metrics in compact table output, including `LOCAL`, `REMOTE`, `MATCH`, `CREATE`, and `DELETE` columns.

### Changed
- `workledger plan apply` push cleanup now archives actually deleted remote rows into trash before creating missing replacement worklogs.
- `workledger plan apply` pull merges now archive removed local active rows into trash in the same SQLite transaction as the merge.
- Human-readable `plan apply` success output now reports aggregate trash archive counts without per-scope trash detail lines.
- Ordinary commands no longer create or repair SQLite schema implicitly; schema bootstrap and repair stay scoped to `workledger init`.

### Fixed
- Missing or mismatched SQLite stores now fail with clear `sqlite_store_not_ready` or `sqlite_store_schema_mismatch` errors before feature SQL runs.

## [0.1.6] - 2026-06-02

### Added
- `workledger totals` now supports `--route-profile` for Jira Cloud and Jira Data Center totals, including reporting-profile totals scoped to configured reporting target issues.

### Fixed
- Plan windows in `workledger plan` output now render in the effective local timezone instead of UTC timestamps.
- Jira push-plan `remote_missing` reason detail is now clearer for remote-missing classifications.
- Date normalization for worklog filter functions now uses the current time consistently.
- Applied remote deletes now clear matching local tombstones so they are not retried on the next sync.

## [0.1.5] - 2026-05-29

### Added
- Updating an existing worklog's issue key now creates a tombstone so the old remote entry is removed on next sync.

### Fixed
- Clockify adapter deletes tombstoned entries even when the entry's tags are missing.
- Jira adapter allows create and replace plans when foreign worklogs already exist for the window.
- Jira 404 responses during reconcile are now handled gracefully instead of hard-failing.
- Reconcile output example shows the full `plan inspect` command example when no actionable items are found.

## [0.1.4] - 2026-05-27

### Added
- `workledger worklogs` issue-prefix filtering for narrower list and apply operations.
- Top-level `workledger tombstones` commands for viewing and managing tombstoned entries.

### Changed
- `workledger plan show` now displays only ready items by default; pass `--all` to include non-ready entries.
- Shared weekday window resolution across worklog date-window selectors to keep shortcut behavior consistent.

### Fixed
- `--instance` flag now applies correctly in `workledger worklogs totals`.
- Full config fingerprint is now used when a single-adapter plan is inserted to prevent stale matches.
- Config error message for fingerprint mismatch is now user-friendly and actionable.

## [0.1.3] - 2026-05-25

### Added
- Weekday shortcut flags for worklog date-window selection.
- Worklog snap support for date-window and workday inputs.
- Configurable workday start and end times for worklog flows.

### Changed
- Worklog query output now uses a `WINDOW` header and clearer time formatting.
- Worklog query results are sorted in ascending `started_at` order.
- Session worklog creation contract and skill guidance were tightened for clarity and precision.

### Fixed
- Default daily lunch time now uses `12:00-12:45`.

## [0.1.2] - 2026-05-22

### Fixed
- Jira issue-prefix matching now requires a trailing dash to avoid ambiguous routing.

## [0.1.1] - 2026-05-22

### Added
- Shared local SQLite storage preflight for write-capable CLI commands.
- `workledger doctor` local storage diagnostics with coverage for writable and unwritable storage cases.
- `workledger worklogs add --dry` preview mode for validating and rendering one would-be local worklog without writing it.
- Accepted date formats and examples in CLI help output.
- Onboarding skill and setup-command guidance for first-time configuration.
- Session worklog creator skill for generating local worklogs from session context.

### Changed
- Local write failures now return friendly `local_storage_not_writable` errors with `sqlite_path`, `parent_dir`, and `operation`.
- Read-only command paths continue to skip storage preflight, and `worklogs apply --dry` remains read-only.
- Specs and agent guidance now treat `workledger doctor` as the canonical local storage diagnostic.
- Clockify reconcile applies now pre-resolve projects and tags once before entry creation.

### Fixed
- Clockify entry creation now uses jittered goroutines to reduce burst contention during apply operations.

## [0.1.0] - 2026-05-15

### Added
- Initial MVP CLI implementation for local worklog capture, planning, status, and reconcile workflows.
- Homebrew-oriented release bootstrapping and packaging setup.

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `worklogs list --epic <NAME>` lists active local worklogs by exact local epic membership across all dates, with optional date and issue filters, JSON filter metadata, and epic-name completion.
- Shift+R in TUI Issues pulls Jira titles and original estimates for marked or focused issues, preserves manual overrides, reports progress, and retains failed or canceled issues for retry.
- Immutable local issue creation timestamps and Created at sorting, applied newest first by default in the TUI; `workledger init` backfills older catalogs from their earliest surviving local timestamps.
- Logged time in TUI issue rows and epic summaries, Logged sorting, and expanded issue details with paged, scrollable all-time local worklog history.
- TUI issue multi-selection through Space, visual range selection through `v`/`V`, and filtered select-all through Ctrl+A, with an atomic local epic assignment picker through `b`; Issues/Epics switching now uses `t`.
- TUI Worklogs Month view with a Monday-first calendar, local booked totals, past-weekday quota gaps, overlap warnings, keyboard and mouse date selection, and selected-day previews and worklog creation.
- Local issue-history picker in TUI Add through `Ctrl+K`, with paged issue-key prefix, worklog-description, and title search, unused decorated issues, and a scrollable description-history pane.
- TUI Day worklog moves through `M`, with original and proposed time previews, valid positions around other bookings and workday boundaries, lunch and overlap checks, and guarded saving through `Ctrl+S`.
- Local issue catalog through `workledger issues set` and atomic JSON patch batches through `issues apply`, with optional titles, maximum estimates, and named epic groups. Worklog writes create missing local issues automatically; Jira refresh preserves optional local metadata.
- Issues catalog filtering through `--issue-prefix`, `--search`, and `--epic`, plus `issues epics` budgets with estimate coverage, lifetime active logged totals, known remaining time, and separate overruns. Estimates remain informational and never limit worklog creation.
- Sixth TUI Issues destination with Issues and Epics views, group drilldown, local search, issue creation, and optional metadata editing.
- Issues and Epics sort menus with selection retained across sorting and refresh, plus viewport-aware Page Up/Page Down and Home/End navigation through filtered lists.
- Searchable contextual TUI Actions through `?`, dispatching the same guarded keyboard actions and confirmations.
- Case-insensitive local Day worklog filtering through `/` by issue key, title, or description, preserving day totals, timeline occupancy, and available windows.
- Scrollable TUI details for worklogs, diagnostics, effective configuration, presets, trash records, and saved plan scopes, including full descriptions and saved desired entries.

### Changed
- Unified TUI Find on `/`, separated issue-picker search modes onto Ctrl+T, and made contextual `?` Actions available in every window without losing drafts or confirmations.
- TUI Issues list headings show only the matching issue count, keeping aggregate budgets in Epics and issue details.
- **Breaking:** New worklog and preset writes now use whole-minute timing. Starts and durations round to the nearest minute before validation; automatic placement uses inward-rounded free slots and whole-minute fragments. Shift deltas round to minutes and reject zero results. Existing rows are not converted automatically, and saved plans with second-precision payloads must be reconciled again before apply or retry.
- Pull reconcile now compares minute-normalized candidates while retaining observed remote timing in totals and trash; rounding-induced duplicates or overlaps block affected scopes.
- The TUI now uses a compact navigation rail, shared title/date header, adjacent inspectors, and a single-row action bar without repeated destination shortcuts. Day browsing and placement forms share a chronological segmented timeline with stable interval widths, solid selected-worklog fill, explicit lunch and overtime states, and available-window summaries.
- Saved plan scope review now shows local and remote counts and durations at planning time, projected destination totals, and saved desired entries without remote requests; unavailable remote inspection is explicitly marked.
- TUI Add and shell issue completion now include decorated and cached issues, rank used keys by their latest worklog creation time, and rank unused metadata keys afterward. An empty Add issue field offers the first catalog key for explicit acceptance.
- Session worklog creation now requires evidence of work performed, generates descriptive entries from that evidence, and defaults unknown exact starts to `--fill --today`; unresolved issue selection may use one clear local title match after explicit duration is provided.

### Fixed
- Jira Cloud and Data Center worklog creation now verifies returned start time and duration against the request, stops the affected scope on disagreement, and reports requested and actual timing without automatic mutation replay.
- Worklog context now slices occupancy at local calendar midnights across 23-hour and 25-hour daylight-saving dates, preventing omitted or double-counted time in daily and monthly totals.

## [0.1.10] - 2026-09-10

### Added
- Interactive `workledger tui` workflows for status, worklogs, plans, presets, and trash.
- Bounded diagnostic history through `workledger activity list` and the global TUI Activity drawer.
- Recent local-ledger cleanup through `workledger worklogs delete --created-within <duration>`, with auditable dry-run output and recoverable trash archival.
- Permanent filtered trash deletion and full-trash clearing through `workledger trash delete` and `workledger trash clear`, with mandatory preview or explicit confirmation.
- Reusable local worklog presets with full CLI and TUI management, selected-day application, editable overrides, dry-run and conflict handling, and local preset-name completion.
- Conflict-checked local trash restoration through `workledger trash restore` and a new TUI Trash tab with Day/Week and All/Local/Remote views.
- `--scope local|remote` filtering for trash list and search commands.

### Changed
- Saved-plan item execution now uses delivery attempts as its only persisted source of truth; `workledger init` migrates legacy terminal results and removes the duplicate item lifecycle columns in a clean schema cutover.
- The TUI Activity drawer now shows only meaningful worklog, trash, preset, and plan actions, while activity outcomes no longer appear in the action bar.
- TUI Worklogs now opens a blank worklog directly with `a` and reserves uppercase `A` for adding from a preset.
- TUI rail clicks and numbered tab shortcuts now focus the activated workspace immediately, while keyboard shortcut guidance is centralized in the action bar except for numbered destination labels in the rail.
- Local worklog deletion now atomically archives restorable source identity and revision metadata instead of permanently discarding the row.
- Preview-driven worklog updates and deletes now reject stale revisions; `workledger init` adds revisions to existing stores, and the TUI detects external ledger changes without overwriting open drafts.
- Explicitly canceled plan apply/retry, status checks, and issue-metadata refreshes now return exit code `130`. Plan execution stops scheduling new target groups, leaves unscheduled scopes unattempted, and marks unconfirmed in-flight mutations uncertain while preserving confirmed successes.

### Fixed
- Preset application now commits worklog creation and recency updates atomically and rejects stale TUI preset drafts without creating a worklog.
- Applied pull plans created by older binaries no longer reappear as unapplied after migration because their terminal item results are preserved as delivery attempts.
- `workledger init` now rebuilds explicit indexes when a single truncated final SQLite page affects indexes only, preserving table rows and verifying full database integrity; writability checks no longer open the database outside SQLite and risk releasing active POSIX locks.
- Worklogs Day and Week views now use consistent one-row spacing after the view selector, while selected worklogs use compact purple focus rails separated from the legend by one blank row without changing the timeline scale or repeating table details.

## [0.1.9] - 2026-09-02

### Added
- Bash, Zsh, and Fish completion script generation with local, read-only suggestions for command values, configured targets, issue keys, worklog IDs, and saved plan IDs.
- `--tomorrow` as a shared date-window selector for CLI workflows, resolving to the next day in the configured local timezone.
- Semantic weekday date values from `mon` through `sun` for local timestamps and explicit date ranges, resolving inside the current local Monday-through-Sunday week.
- `workledger worklogs add --overtime` for explicitly allowing automatic `--fit` and `--fill` placement to start at or after the configured workday end.

### Changed
- Automatic `worklogs add --fit` and `--fill` placement now skips slots starting at or after `day_end` unless `--overtime` is supplied, while still allowing an eligible worklog that starts earlier to extend past `day_end`.

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

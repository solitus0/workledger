# Daily workflows

[Documentation](README.md) · [Keybindings](keybindings.md) · [CLI reference](cli.md)

Use these CLI workflows directly or from an agent. The optional TUI provides interactive browsing and editing of the same local data; its controls are called out where useful.

## Record an activity

Use an issue key when you have one:

```sh
workledger worklogs add --issue PROJ-123 --started todayT09:00 --duration 2h --description "Implement event delivery"
workledger worklogs list --today
workledger worklogs context --today
```

`--started` uses the configured local timezone. `--started-utc` accepts an explicit RFC3339 UTC timestamp. Starts and durations normalize to minute boundaries, and entries must satisfy the configured minimum duration. Descriptions identify the work performed.

Add `--dry` to preview an add before writing. Use `worklogs update --help` or the TUI editor to change an entry; duplicate and overlap checks still apply.

## Place work into free time

Use `--fit` for one continuous entry in the earliest free slot, or `--fill` to split the supplied duration across available slots:

```sh
workledger worklogs add --issue PROJ-123 --fit --today --duration 90m --description "Review delivery receipts"
workledger worklogs add --issue PROJ-123 --fill --today --duration 3h --description "Implement retry handling"
```

Placement respects existing bookings and configured lunch. Automatic placement starts entries before the workday end by default; an entry that starts before that boundary may finish afterward. `--overtime` explicitly allows a start at or after the configured end. Use it only when that is intended.

In Worklogs Day view, press `M` to preview moving the selected entry. Up/down selects valid destinations before or after another booking or at a workday boundary. `Ctrl+S` saves, `Esc` cancels, and `r` refreshes the source and available positions. A move keeps the same entry and duration, stays inside the configured day, and avoids lunch and other bookings.

[Placement illustration](images/fill_v_fit.png)

## Work before an issue exists

Capture an unassigned activity now and choose an issue later:

```sh
workledger worklogs add --fill --today --duration 1h --description "Investigate payment retry"
workledger worklogs list --unassigned
workledger worklogs assign <id> <id> --issue PROJ-123 --dry
workledger worklogs assign <id> <id> --issue PROJ-123
workledger worklogs update <id> --issue ""
```

Unassigned work counts toward booked time, occupied slots, and local quota. Push skips it. The Unassigned list includes all dates, oldest first, unless narrowed by date selectors. Assignment changes only the selected unassigned entries and saves the batch atomically.

In the TUI, `u` opens Unassigned. Mark entries with Space, ranges with `v`/`V`, or all matches with `Ctrl+A`, then use `b` to assign an issue. Marks can include hidden selections across dates and filters. The assignment modal shows the selected count; `Ctrl+K` opens issue search, and choosing a key returns to the modal before a separate save.

A pull remains issue-scoped and can import a keyed entry overlapping unassigned work. If later assignment reports a conflict, inspect the IDs and resolve the duplicate before retrying.

## Issues, epics, and effort budgets

Saving a keyed worklog creates its local issue identity. Add optional metadata independently of remote Jira issues:

```sh
workledger issues set --issue PROJ-123 --title "Reliable event delivery" --max-estimate 16h --epic "Platform reliability"
workledger issues list --epic "Platform reliability"
workledger issues epics --issue-prefix PROJ
workledger worklogs list --epic "Platform reliability"
```

Each local issue belongs to at most one named epic. Grouping does not change Jira hierarchy. Epic names match exactly after trimming, including case. An epic or unassigned filter can list worklogs across all dates; ordinary worklog lists require a date selector.

Budgets use all active local worklogs across all dates. Date filters choose issues without reducing their lifetime effort totals. Remaining time sums each estimated issue’s unused budget, clamped at zero; overruns are separate. Missing estimates remain unknown, and partial totals report estimate coverage. These values describe logged effort, not completion.

Manual titles and estimates override cached Jira metadata. `--clear-title` and `--clear-max-estimate` reveal cached values; `--clear-epic` removes local grouping. Estimates accept positive whole-second Go durations and never limit worklog creation or determine an agent’s logged duration.

Apply a JSON array of issue patches with `issues apply`. Only `issue_key` is required; omitted fields remain unchanged and `null` clears an optional field. The batch validates before atomic persistence:

```sh
workledger issues apply --file docs/examples/issues.json --dry --output json
workledger issues apply --file docs/examples/issues.json
```

Those paths assume a repository checkout. The [example issue catalog](examples/issues.json) demonstrates titles, estimates, and grouping.

In the optional TUI’s Issues view (`5`), `t` switches between Issues and Epics. Enter opens an epic’s members. Selecting an issue previews its full metadata and worklog history in independently scrollable right panes. `/` opens Find; `Ctrl+T` switches between issue metadata and worklog-description search. `s` chooses a sort. Space, `v`/`V`, `Ctrl+A`, and `b` support assigning selected issues to an epic, including hidden marks.

Use `Shift+R` to pull Jira titles and original estimates for marked issues, or the focused issue when none are marked. Ownership routes select the original Jira instance. Local overrides, epic assignments, creation dates, and worklogs remain intact. Successful issues clear their marks; failures remain selected for retry. `Esc` stops pending requests while preserving completed work. Lowercase `r` refreshes local data.

## Reuse recurring activities

```sh
workledger presets add daily-sync --issue PROJ-123 --start 10:30 --duration 15m --description "Daily engineering sync"
workledger presets apply daily-sync --date tomorrow
workledger presets apply daily-sync --date mon --start 11:00 --dry
```

A preset is a reusable draft, not a scheduler. Applying it creates an ordinary worklog for an explicit local day. One-off overrides do not modify the saved preset. Presets can also be unassigned. Manage them with `presets list`, `show`, `update`, and `delete`; in Worklogs, uppercase `A` opens the preset picker. Deleting a preset permanently removes the template; existing worklogs are unaffected.

## Recover deleted work

Local deletion moves worklogs to trash. Review the match before a recent-entry batch delete:

```sh
workledger worklogs delete --created-within 15m --dry
workledger worklogs delete --created-within 15m --yes
```

`--created-within` selects when rows entered the local ledger, not when the work happened. A recent pull can therefore match. Open Trash (`6`) to inspect archived records and restore eligible local worklogs. Restoration is conflict-checked. Remote trash records are audit evidence and cannot be restored locally through that action.

Permanent removal is CLI-only and irreversible within Workledger:

```sh
workledger trash delete <trash-id> --dry
workledger trash delete <trash-id> --yes
workledger trash delete --scope local --trashed-within 15m --dry
workledger trash clear --dry
workledger trash clear --yes
```

Trash date selectors refer to the original worklog start; `--trashed-within` refers to archival time. `trash clear` removes both recoverable local trash and remote audit evidence. These operations do not modify active worklogs, saved plans, or remote services. SQLite deletion is not forensic erasure; WAL files, snapshots, and backups may retain data.

## Review activity

`workledger activity list` shows recent CLI and TUI diagnostic history. The `g` key toggles the TUI Activity drawer. History is capped at 500 entries and is diagnostic rather than an immutable audit log.

For tracker comparisons and reviewed changes, continue with [synchronization](synchronization.md).

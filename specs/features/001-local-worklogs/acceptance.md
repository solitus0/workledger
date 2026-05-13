# Local worklogs acceptance

Key acceptance coverage:

- `001-local-worklogs` owns only MVP phase 1 workspace bootstrap and MVP phase 2 local worklog CRUD.
- `workledger init`, `workledger version`, and `workledger config validate` satisfy the bootstrap slice.
- bare `workledger` and root help forms `-h`, `--help`, and `help` satisfy the root help slice.
- every MVP command also accepts `-h` and `--help` and returns command help on stdout with exit code `0`.
- `workledger worklogs list`, `search`, `show`, `add`, `update`, and `delete` satisfy the local canonical worklog slice.
- `workledger worklogs delete --dry` previews filtered batch delete and `workledger worklogs delete --yes` executes it atomically.
- Write validation failures return exit code `2` with no partial writes.
- Missing active worklogs return exit code `3`.
- JSON output remains stable and table output remains deterministic.
- Tombstones preserve local deletion intent and protect deleted allocations from blind recreation during later deferred pull flows.

## Phase 1 acceptance

The bootstrap phase is accepted only when all of the following hold:

- `workledger version` succeeds without requiring config presence or config validity.
- `workledger version --output json` returns a fixed machine-readable object on stdout with no mixed log lines.
- bare `workledger` returns root help on stdout and exit code `0`.
- `workledger -h`, `workledger --help`, and `workledger help` return the same root help on stdout and exit code `0`.
- every MVP command accepts `-h` and `--help`, and help bypasses config validation, SQLite access, and normal argument validation.
- `workledger init` resolves the operator config path according to the documented local path rules.
- `workledger init` provisions starter YAML without requiring any remote adapter configuration.
- `workledger init` provisions the SQLite path and an empty schema for local-only MVP use.
- rerunning `workledger init` on an already bootstrapped workspace behaves deterministically and does not corrupt existing local state.
- `workledger config validate` enforces strict MVP-only config rules and reports all discovered validation errors in one invocation.
- bootstrap commands remain local-only and do not require network access.

## Phase 2 acceptance

The local CRUD phase is accepted only when all of the following hold:

- SQLite is the canonical source of truth for active local worklogs and tombstones.
- active local worklogs are addressable by stable local UUID `id`.
- default delete removes an active record from the active set and writes a tombstone that retains deletion evidence.
- hard delete removes an active record from the active set without retaining tombstone evidence.
- local commands remain authoritative for manual adds, corrections, and deletes without requiring adapter participation.
- list and show expose only canonical local state for MVP and do not infer or fetch remote data.
- date-window inspection, local-time input handling, explicit UTC input handling, and duplicate or overlap validation all behave according to the shared CLI contract.

## `worklogs list`

Acceptance requires all of the following:

- `workledger worklogs list` requires at least one explicit time selector.
- `workledger worklogs list` returns active local worklogs for the selected time scope by default.
- `workledger worklogs list --only-deleted` returns deleted tombstones for the selected time scope instead of active rows.
- list reuses the documented selectors and returns the full filtered set without pagination.
- list supports the shared date-window shortcut selectors `--current-week`, `--last-week`, `--current-month`, and `--last-month`.
- list sorting is fixed in MVP as `started_at desc`, then stable local `id`.
- empty table results still render deterministic headers with zero data rows.
- default table columns for active rows are `ID`, `ISSUE`, `STARTED`, `DURATION`, and `DESCRIPTION`.
- `STARTED` renders the effective local timestamp with an explicit offset.
- human-facing table output renders aligned columns.
- human-facing table output appends a blank line followed by a totals footer with matched row count and summed booked duration in human-readable format.
- active list footers render as `Totals: <N> worklogs, <duration>` and deleted list footers render as `Totals: <N> tombstones, <duration>`.
- the totals footer is computed from the full matched result set and does not depend on `--fields`.
- in `worklogs list`, `DESCRIPTION` truncates to 80 characters with `...` when longer.
- `--fields` restricts table columns to the requested fields in the requested order.
- JSON list output always includes `filters`, `items`, and `total`.
- JSON `filters` expose both raw operator inputs and effective normalized values.
- JSON `--fields` changes only item field selection and does not remove `filters` or `total`.
- active worklog JSON rows expose canonical worklog fields only and do not project issue metadata.
- deleted tombstone rows expose only `id`, `issue_key`, and `deleted_at`.

## `worklogs search`

Acceptance requires all of the following:

- `workledger worklogs search <query>` requires one positional query argument.
- blank or whitespace-only query values fail validation with exit code `2`.
- search does not require a time selector and searches across all stored dates by default.
- search matches canonical stored descriptions by partial, case-insensitive literal substring.
- `%` and `_` in the query are treated literally rather than as wildcard syntax.
- `--issue` narrows matches to one issue key when provided.
- shared date-window selectors narrow search scope when provided.
- search sorting is fixed in MVP as `started_at desc`, then stable local `id`.
- zero matches return exit code `0`.
- `--only-deleted` switches search from active worklogs to deleted tombstones only.
- active search table output uses the same columns, truncation, and totals footer contract as `worklogs list`.
- deleted search table output uses the same tombstone columns and totals footer contract as `worklogs list --only-deleted`.
- JSON search output always includes `filters`, `items`, and `total`.
- JSON search `filters.raw.query` preserves the operator-supplied query.
- JSON search `filters.effective.query` exposes the trimmed normalized query used for matching.
- JSON search `--fields` changes only active item field selection and does not remove `filters` or `total`.

## `worklogs show`

Acceptance requires all of the following:

- `workledger worklogs show <id>` loads exactly one active local worklog by stable local UUID.
- show returns canonical fields only.
- show renders `started_at` in the effective local timezone with an explicit offset.
- JSON show output uses the default active-worklog JSON record shape.
- show returns exit code `3` when the ID does not exist.
- show also returns exit code `3` when the ID exists only as a tombstone.

## `worklogs add`

Acceptance requires all of the following:

- add requires `--issue`, one started-time source, `--duration`, and `--description`.
- add accepts exactly one of `--started <LocalTimestamp>` or `--started-utc <RFC3339UTC>`.
- issue keys must match canonical `<PROJECTKEY>-<NUMBER>` grammar and invalid case must fail rather than normalize.
- local timestamp input accepts the documented absolute and relative grammars.
- explicit UTC input accepts RFC3339 UTC only.
- local and UTC started inputs normalize to one canonical UTC instant before storage.
- invalid local civil times, including timezone gaps and ambiguous repeated wall-clock times, fail validation.
- future `started_at` timestamps are allowed.
- duration normalizes to strictly positive whole seconds and must satisfy the effective configured minimum duration.
- the effective minimum duration defaults to `900` seconds when config does not override it.
- description normalization trims outer whitespace, collapses internal whitespace, and produces a non-empty single-line string.
- duplicate and overlap detection evaluates against all active local worklogs, not only matching issue keys.
- exact boundary touch is allowed and is not treated as an overlap.
- tombstones are ignored for duplicate and overlap validation.
- conflict failures return exit code `2` with machine-readable conflict details in the selected output mode.
- `--force` explicitly bypasses duplicate and overlap rejection.
- successful add auto-generates the local `id` and returns the created canonical record.

## `worklogs update`

Acceptance requires all of the following:

- update uses patch-style flags only.
- update requires at least one patch flag.
- update rejects invocations that provide both `--started` and `--started-utc`.
- update validates the full resulting record after patching, not only the changed fields.
- update preserves the same minimum-duration, timestamp, description, duplicate, and overlap rules as add.
- update excludes the row being changed from duplicate and overlap detection.
- normalization may produce a semantic no-op and that still succeeds as a canonical update response.
- tombstoned IDs are treated as not found.
- missing IDs return exit code `3`.
- `--force` explicitly bypasses duplicate and overlap rejection.
- successful update returns the updated canonical record.

## `worklogs delete`

Acceptance requires all of the following:

- single delete removes exactly one active local worklog.
- single delete is local-only and non-interactive once validation passes.
- default single delete writes one tombstone preserving the deleted `id`, `issue_key`, original `started_at`, and `deleted_at`.
- tombstones preserve the deleted row `description`.
- single delete with `--hard` permanently removes the active row without writing a tombstone.
- single delete returns deterministic success output in table or JSON mode.
- missing IDs return exit code `3`.
- duplicate and overlap enforcement does not apply to delete.

## Filtered batch delete

Acceptance requires all of the following:

- filtered batch delete is part of the MVP through `workledger worklogs delete`.
- filtered batch delete reuses the active-worklog selectors from `worklogs list`.
- single-ID delete mode and filtered batch-delete mode are mutually exclusive.
- any non-empty valid selector subset is sufficient for filtered batch delete.
- filtered batch delete requires `--yes` for execution.
- filtered batch delete supports `--hard` to skip tombstone creation for matched active rows.
- `--dry` is available only for filtered batch-delete preview and is mutually exclusive with `--yes`.
- preview returns the full matched active records and the matched count without writing tombstones.
- executed filtered batch delete returns deleted IDs and deleted count rather than full deleted records.
- both preview and executed batch delete echo the effective normalized filters used for matching.
- filtered batch delete is valid when zero rows match and remains a deterministic no-op.
- filtered batch delete applies only to active rows and never operates on tombstones.
- executed filtered batch delete is atomic and writes one normal tombstone per deleted worklog unless `--hard` is used.

## `worklogs restore`

Acceptance requires all of the following:

- `worklogs restore` is selector-based only and does not accept a tombstone ID in MVP.
- `worklogs restore` reuses the time selectors from `worklogs list` and requires at least one explicit time selector.
- `worklogs restore` optionally accepts `--issue <KEY>` in addition to the required time selector family.
- `worklogs restore` operates on tombstones only and matches them by original `started_at`.
- `worklogs restore` requires `--yes` for execution and supports `--dry` preview with the same selector scope.
- `--dry` and `--yes` are mutually exclusive.
- restore dry-run returns the matched tombstones together with the active rows that would be recreated.
- executed restore returns restored IDs and restored count rather than full active records.
- restore validates the full restore set against active rows and against conflicts inside the restore set.
- restore fails atomically on duplicate or overlap conflicts unless `--force` is set.
- `--force` explicitly bypasses duplicate and overlap rejection for restore.
- executed restore recreates active rows with the original `id`, `issue_key`, `started_at`, `duration`, and `description`.
- executed restore consumes the matching tombstones atomically.
- restore is valid when zero tombstones match and remains a deterministic no-op.

## Shared write guarantees

Acceptance requires all of the following:

- add, update, delete, and restore mutate canonical SQLite state only.
- validation failures never produce partial writes.
- duplicate and overlap failures use exit code `2`.
- not-found cases use exit code `3`.
- unexpected failures use exit code `1`.
- user-facing results stay on stdout and diagnostics stay on stderr.
- `--output json` always produces valid JSON on stdout without mixed progress or log output.
- table output remains deterministic for equivalent inputs.

## Tombstone guarantees

Acceptance requires all of the following:

- tombstones are visible through `workledger worklogs list --only-deleted`.
- tombstones are not visible through `workledger worklogs show <id>`.
- tombstones are ignored by duplicate and overlap validation for later adds or updates.
- tombstones preserve enough local identity and timing data to support later deferred reconcile logic.
- tombstones preserve enough local record content to recreate deleted rows through restore.
- default local deletes therefore remain durable intent, not just transient row removal.
- hard deletes intentionally skip that durable delete intent.

## Explicitly out of scope for this feature

Acceptance for `001-local-worklogs` must not require:

- `workledger worklogs context`
- `workledger worklogs shift`
- `workledger worklogs apply`
- adapter connectivity or health checks
- adapter pull
- outbound delivery
- routing
- saved plans
- retries
- delivery correlation
- tombstone-specific top-level commands

Canonical references:

- [API / CLI and acceptance](../../api.md#cli-and-acceptance)
- [API / Local worklogs](../../api.md#local-worklogs)
- [API / JSON contracts](../../api.md#json-contracts)
- [UX / Agent workflows](../../ux.md#agent-workflows)
- [Testing](../../testing.md)

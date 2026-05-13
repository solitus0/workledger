# API

This file owns the CLI contract, machine-readable output contract, validation rules, and adapter command surface.

## Configuration and Storage

### `workledger init`

`workledger init` prepares the local config path, writes a starter YAML config when one does not already exist, and provisions the empty SQLite worklog store.
`workledger init` must support the standard MVP output modes.

Config path:

* use `~/.config/workledger/config.yaml`

MVP rules:

* create the config directory when missing
* write a starter config only when the file does not exist
* when the config file already exists, succeed as a no-op and print a clear message to stdout
* never overwrite an existing config without an explicit future flag
* when the existing config file is invalid, fail clearly and do not rewrite it
* write only the minimum valid MVP config shape
* create the config directory with private permissions suitable for local credentials
* write the config file with private permissions suitable for local credentials
* fail clearly when required private config permissions cannot be enforced
* when the config directory has overly broad permissions, tighten them automatically
* when the existing config file has overly broad permissions, tighten them automatically
* create the SQLite parent directory when missing for any validated configured `storage.sqlite_path`
* when the SQLite parent directory has overly broad permissions, tighten them automatically
* create the SQLite file and initialize the full empty MVP schema when the database does not exist
* enforce private file permissions on the SQLite file suitable for local operator data
* when the SQLite file already exists, leave it unchanged
* when the existing SQLite file has overly broad permissions, tighten them automatically
* when the SQLite file exists but is missing MVP tables, repair it additively by creating the missing tables only
* when the existing SQLite file is incompatible or corrupt and cannot be repaired additively, fail clearly
* bootstrap `default_output: table`
* bootstrap `storage.sqlite_path: ~/.local/share/workledger/worklogs.db`
* bootstrap `worklogs.minimum_duration_seconds: 900`
* write a commented Clockify example block by default
* when process env `CLOCKIFY_API_KEY` is set and the config file does not yet exist, call Clockify `GET /v1/user`, then write an active `clockify` block with `auth.api_key` from `CLOCKIFY_API_KEY`, `user_id` from the returned user `id`, and `workspace_id` from `activeWorkspace` falling back to `defaultWorkspace`
* read `CLOCKIFY_API_KEY` from process env only during `init`; do not parse repo-local `.env` files at runtime
* do not test adapter connectivity or create saved-plan tables in MVP
* reject unsupported future top-level sections until the CLI explicitly supports them

When a valid config file already exists, `workledger init` must still attempt SQLite path provisioning and schema bootstrap from the configured `storage.sqlite_path`.
This keeps `init` idempotent and usable as a local bootstrap or repair command without rewriting config.
The bootstrapped schema includes both active-worklog storage and tombstone storage.
SQLite schema details are defined in [data-model.md#sqlite-schema](data-model.md#sqlite-schema).

In JSON output, `workledger init` must report lifecycle states rather than booleans.
The exact JSON success schema is defined in [api.md#json-contracts](api.md#json-contracts).

* `config` uses `created` or `reused`
* `sqlite` uses `created`, `reused`, or `repaired`

### `workledger config validate`

Configuration validity is explicit in MVP and remains local-only.
All commands that depend on config must require the full config file to be globally valid, including optional adapter sections when present.

It must verify at least:

* YAML parses successfully
* only MVP-supported sections and keys are present
* `storage.sqlite_path` is present and non-empty
* `storage.sqlite_path` may use `~` and must resolve to a valid absolute local path after expansion
* the resolved SQLite path must be usable for local SQLite operations, including a writable parent directory
* `worklogs.minimum_duration_seconds`, when present, must be a positive whole number of seconds
* when `worklogs.minimum_duration_seconds` is absent, the effective minimum local worklog duration defaults to `900`
* `selection.timezone`, when present, is valid
* when `selection.timezone` is absent, local timestamp resolution and date selection fall back to the system local timezone
* `default_output`, when present, must be `table` or `json`
* optional `jira_cloud`, `jira_data_center`, and `clockify` sections follow their own schema when present
* optional adapter sections fail clearly on unknown keys or missing required auth fields
* optional top-level `routing` follows its own schema when present

Timezone rules:

* canonical stored worklog timestamps use UTC
* `selection.timezone` affects local `started_at` input resolution for worklog writes
* `selection.timezone` affects date-based filters such as `--today`, `--yesterday`, `--from`, and `--to`
* `selection.timezone` affects operator-facing `started_at` rendering
* timezone normalization is storage behavior only and must not rewrite the config file

Normalization rules:

* trim trailing `/` from configured adapter `base_url` values after validation
* normalization is in-memory only and must not rewrite the config file

Validation should report all discovered schema and field errors in one pass and fail clearly instead of attempting partial command execution on invalid config.

`workledger config validate` must support the standard MVP output modes.
The exact JSON success and minimum error-payload rules are defined in [api.md#json-contracts](api.md#json-contracts).

* success in table output prints an explicit success line to stdout
* success in JSON output returns a valid JSON success payload on stdout
* failure in JSON output returns a valid JSON error payload with all discovered validation errors

Output mode precedence is:

* command `--output` flag
* config `default_output`
* built-in fallback `table` when the config key is absent

### Example YAML

```yaml
default_output: table
selection:
  timezone: Europe/Vilnius
storage:
  sqlite_path: ~/.local/share/workledger/worklogs.db
worklogs:
  minimum_duration_seconds: 900
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token: my-jira-token
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token: my-dc-token
clockify:
  workspace_id: 0123456789abcdef
  user_id: 0123456789abcdef
  auth:
    api_key: my-clockify-api-key
```

### Adapter sections

Adapter sections are optional in MVP.

Rules:

* `jira_cloud` owns Jira Cloud adapter config only
* `jira_data_center` owns Jira Data Center adapter config only
* `clockify` owns Clockify adapter config only
* YAML config section names use the shared `snake_case` adapter-family form
* each adapter section is validated independently
* adapter credentials stay local-only and are never rewritten by the CLI


## Local Worklogs

### Goal

Provide canonical local worklog CRUD on top of SQLite without requiring any remote adapter.
Include default local tombstone retention and visibility in a way that stays compatible with future sync behavior, while allowing explicit hard-delete bypass.

### Commands

This file defines:

* current MVP command semantics for local worklog bootstrap and CRUD
* deferred local follow-up command semantics for planning and batch mutation

Current MVP commands in this section are:

* `workledger worklogs list`
* `workledger worklogs search <query>`
* `workledger worklogs show <id>`
* `workledger worklogs add`
* `workledger worklogs update <id>`
* `workledger worklogs delete <id>`
* `workledger worklogs restore`

Deferred local follow-up commands in this section are:

* `workledger worklogs context`
* `workledger worklogs shift`
* `workledger worklogs apply`

### Shared rules

All commands must:

* read and write canonical local worklogs in SQLite only
* support `table` and `json` output
* keep stdout reserved for command output and stderr reserved for diagnostics
* validate config before opening the SQLite store
* keep local issue-key validation independent from adapter routing or connectivity

Exact JSON success schemas and minimum error-payload guarantees for these commands are defined in [api.md#json-contracts](api.md#json-contracts).
SQLite schema, indexes, and transaction boundaries are defined in [data-model.md#sqlite-schema](data-model.md#sqlite-schema).
Recommended operator and agent sequences are defined in [ux.md#agent-workflows](ux.md#agent-workflows).

Canonical local fields are:

* `id`
* `issue_key`
* `started_at`
* `duration_seconds`
* `description`

`id` is an opaque UUID string generated locally.
The CLI must not expose or depend on SQLite row IDs.
The operator cannot supply or override `id`.
Operator-facing `started_at` values are rendered in the effective local timezone as RFC3339 with an explicit offset.
SQLite stores one canonical `started_at_utc` instant per worklog.
`started_at_utc` is an explicit UTC input field for write operations and a default field in active-worklog JSON output.

The default active-worklog JSON record shape includes:

* `id`
* `issue_key`
* `started_at`
* `started_at_utc`
* `duration_seconds`
* `description`

### Shared selectors

The shared selector families in this file are:

* active-worklog selectors: `--issue`, `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`
* date-window selectors: `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`
* deleted-worklog selector: `--only-deleted`
* field selector: `--fields`
* planning issue selector: repeated `--issue <KEY>`

#### Date-window selectors

Date-window selectors must:

* support `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`
* treat the shortcut selectors `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, and `--last-month` as mutually exclusive with each other and with `--from` and `--to`
* evaluate local dates in `selection.timezone` when configured
* use the system local timezone when `selection.timezone` is absent
* accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, and signed day offsets in `+Nd` or `-Nd` form for `--from` and `--to`
* treat week ranges as Monday-through-Sunday calendar weeks in the effective selection timezone
* expand `--current-week` to the start of the current local Monday and the end of the current local Sunday
* expand `--last-week` to the start of the previous local Monday and the end of the previous local Sunday
* treat month ranges as calendar-month windows in the effective selection timezone
* expand `--current-month` to the start of the first local day of the current calendar month and the end of the last local day of that month
* expand `--last-month` to the start of the first local day of the previous calendar month and the end of the last local day of that month
* expand `--from` to the start of the selected day in the effective selection timezone
* expand `--to` to the end of the selected day in the effective selection timezone
* ignore explicit range option order
* use the earlier expanded bound as the effective start and the later expanded bound as the effective end when both `--from` and `--to` are set
* convert expanded date windows to canonical UTC query boundaries before querying storage

#### Active-worklog selectors

Active-worklog selectors must:

* support at most one `--issue` filter value per invocation
* select active local worklogs by date and issue

#### Deleted-worklog selector

`--only-deleted` must:

* switch `worklogs list` from active worklogs to deleted tombstones
* reuse the same date filtering logic as active worklogs
* evaluate date filters against the deleted worklog's original `started_at`
* apply `--issue` filtering in deleted-only mode
* keep the same `started_at desc`, then stable local `id` ordering as active results
* remain invalid in filtered batch-delete mode

#### Field selector

`--fields` must:

* accept a comma-separated ordered subset of the selected record shape
* fail validation on duplicate field names
* allow `id`, `issue_key`, `started_at`, `started_at_utc`, `duration_seconds`, and `description` for active worklogs
* allow `id`, `issue_key`, and `deleted_at` for deleted tombstones
* fail validation when any requested field is not valid for the selected record class

#### Planning issue selector

Planning issue selector must:

* allow repeated `--issue <KEY>` values
* preserve operator-supplied issue order
* reuse the canonical `<PROJECTKEY>-<NUMBER>` issue-key grammar
* remain optional on `worklogs context`
* act as planning input only and never filter existing stored worklogs selected by date

### `workledger worklogs list`

`worklogs list` must:

* require at least one explicit time selector from the shared date-window selector family
* render active local worklogs within the selected time scope
* render deleted tombstones within the selected time scope instead of active worklogs when `--only-deleted` is set
* support the active-worklog selectors, deleted-worklog selector, and field selector from [Shared selectors](#shared-selectors)
* return the full filtered result set without pagination
* keep sorting fixed in MVP and do not expose sort-selection flags
* sort by `started_at desc`, then stable local `id`

Table output columns:

* `ID`
* `ISSUE`
* `STARTED`
* `DURATION`
* `DESCRIPTION`

Empty table results still render the selected table headers with zero data rows.
When `--fields` is set, table output renders only the selected columns in the requested order.
`STARTED` renders the effective local `started_at` value with an explicit offset.
Human-facing table output must render aligned columns rather than raw tab-delimited cells.
After the table, human-facing output must append a blank line and a totals footer with matched row count plus summed booked duration in human-readable format.
For active rows, the footer format is `Totals: <N> worklogs, <duration>`.
For deleted tombstones, the footer format is `Totals: <N> tombstones, <duration>`.
The totals footer is derived from the full matched result set and is unaffected by `--fields`.
In `worklogs list`, `DESCRIPTION` is a list-view summary cell and must truncate to 80 characters with `...` when longer.

When `--only-deleted` is set, table output columns are:

* `ID`
* `ISSUE`
* `DELETED`

JSON output must include:

* `filters`
* `items`
* `total`

Active worklog JSON items must expose only canonical worklog row fields and must not project joined issue metadata.

For JSON list output, `filters` must expose both:

* `raw` values as supplied by the operator
* `effective` values after normalization, timezone expansion, and date-bound reordering
* active-worklog JSON items include the full default active-worklog JSON record shape when `--fields` is not set
* `--fields` does not remove `filters` or `total`
* when `--fields` is set, each JSON item includes only the selected item fields in the requested order

`worklogs list` returns one record class per invocation in MVP:

* active worklogs by default
* deleted tombstones when `--only-deleted` is set

Deleted tombstone rows expose only:

* `id`
* `issue_key`
* `deleted_at`

### `workledger worklogs search <query>`

`worklogs search` must:

* require one positional `<query>` argument
* trim outer whitespace from `<query>` and fail validation when the result is empty
* preserve the remaining internal query text exactly
* search canonical stored normalized `description` values by partial, case-insensitive substring match
* treat `<query>` as a literal substring rather than wildcard syntax
* reuse the active-worklog selectors, deleted-worklog selector, and field selector from [Shared selectors](#shared-selectors)
* not require any time selector
* search active local worklogs by default across all stored dates
* search deleted tombstones instead of active worklogs when `--only-deleted` is set
* apply `--issue` and any supplied date-window selectors in the same way as `worklogs list`
* evaluate deleted-mode date filters against each tombstone's original `started_at`
* return the full filtered result set without pagination
* keep sorting fixed in MVP and do not expose sort-selection flags
* sort by `started_at desc`, then stable local `id`
* return exit code `0` when zero matches are found

Table output rules:

* active results use the same columns, field-selection behavior, description truncation, and totals footer contract as `worklogs list`
* deleted results use the same deleted-row columns and tombstone totals footer contract as `worklogs list --only-deleted`

JSON output must include:

* `filters`
* `items`
* `total`

For JSON search output, `filters` must expose both:

* `raw` values as supplied by the operator, including the supplied `query`
* `effective` values after normalization, timezone expansion, date-bound reordering, and query trimming, including the normalized `query`
* active-worklog JSON items include the full default active-worklog JSON record shape when `--fields` is not set
* `--fields` does not remove `filters` or `total`
* when `--fields` is set for active results, each JSON item includes only the selected item fields in the requested order

`worklogs search` returns one record class per invocation in MVP:

* active worklogs by default
* deleted tombstones when `--only-deleted` is set

Deleted tombstone rows expose only:

* `id`
* `issue_key`
* `deleted_at`

### `workledger worklogs context`

`worklogs context` must:

* remain read-only and load local planning state only
* reuse the date-window selectors and planning issue selector from [Shared selectors](#shared-selectors)
* support optional workday-analysis inputs `--day-start`, `--day-end`, `--lunch`, and `--no-lunch`
* remain descriptive only and must not generate worklogs or apply payloads
* return one first-class planning snapshot for the selected scope instead of only flat worklog rows
* structure the planning snapshot per selected local day
* include empty selected days even when no worklogs exist for those days
* support both `table` and `json` output, with `json` as the canonical machine-readable contract

Output should combine at minimum:

* `filters` with raw and effective selector values
* `summary` with planning-specific aggregate values instead of `list`-style `total`
* `settings` with effective timezone and workday settings used for the analysis
* `planning` with machine-readable helper fields for external payload construction
* `days` as one item per selected local day

Each `days` item should combine at minimum:

* all active local worklogs already present for that day using the same active-worklog row shape as `worklogs list`
* the total booked duration for that day
* free slots inside the configured workday
* collisions already present in local state for that day

`days[*].worklogs` groups rows by the local day of each row's `started_at`.
One stored worklog row must appear only once in the planning snapshot, under its local start day.
When a stored worklog crosses midnight, `days[*].booked_seconds`, `days[*].free_slots`, and `days[*].collisions` must be computed from the actual occupied interval slice inside each selected local day.
Cross-midnight occupancy affects each selected day touched by the interval even though the stored row still appears only once under its local start day.
Selected planning issues from repeated `--issue` inputs must not hide unrelated stored worklogs from the returned day snapshots.

The default workday window is `08:00-17:00`.
`--day-start <HH:MM>` overrides the default start.
`--day-end <HH:MM>` overrides the default end.
`--day-start` must be earlier than `--day-end`.
The default lunch exclusion window is `12:00-13:00`.
`--lunch <HH:MM-HH:MM>` overrides that default.
`--no-lunch` disables lunch subtraction for the invocation.
`--lunch` and `--no-lunch` are mutually exclusive.
`--lunch` must define a positive interval that fits strictly inside the effective workday window.
`settings.lunch` defines the configured exclusion window or `null` when lunch is disabled.
`days[*].free_slots` must already exclude lunch time and must represent only genuinely available working time.
`days[*].free_slots` order is normative for automation and must be ascending by local start time.
Allocation, splitting, and final add generation stay outside the CLI and are expected to be handled by the operator or an external agent using the returned planning data.

`days[*].collisions` must expose explicit conflicting intervals plus the involved local worklog IDs.
`planning.issue_order` must reflect the operator-supplied repeated `--issue` order.
`planning.issues[*]` must include baseline fields `issue_key` and `max_estimate_seconds`.
`planning.issues[*]` may include additive read-only metadata fields in this scope.
When additive metadata is unavailable for a field that is otherwise surfaced, the value must remain explicit as `null`.
`planning.minimum_duration_seconds` must expose the effective minimum add/apply duration from config.
`planning.payload_contract` must indicate that downstream writers should construct a raw `worklogs apply` payload with top-level `adds`.
`planning.slot_order` must indicate that free slots are ordered by ascending local day and ascending slot start.

Table output rules:

* table output is a shallow summary view for human inspection
* table output should render one row per selected day
* table output must not be the normative contract for downstream automation

JSON output rules:

* JSON output is the normative contract for downstream automation
* `summary` may include `day_count`, `worklog_count`, `booked_seconds`, `free_seconds`, and `collision_count`
* `days[*].worklogs` uses the same default active-worklog JSON record shape as `worklogs list`
* `summary.booked_seconds`, `summary.free_seconds`, and `summary.collision_count` must aggregate the selected days after per-day proration of any cross-midnight occupancy
* `days[*].free_slots` exposes explicit local start and end timestamps plus `duration_seconds`
* `days[*].collisions[*]` exposes `start`, `end`, and `worklog_ids`
* `planning` must expose `issue_order`, `issues`, `minimum_duration_seconds`, `payload_contract`, and `slot_order`

### `workledger worklogs show <id>`

`worklogs show` must:

* load one local worklog by stable local UUID `id`
* render the canonical fields only
* render `started_at` in the effective local timezone with an explicit offset
* return exit code `3` when the worklog does not exist

In JSON output, `worklogs show` uses the default active-worklog JSON record shape.

`worklogs show` remains an active-worklog command in MVP.
Deleted tombstones are visible through `worklogs list --only-deleted`, not through `worklogs show`.
When an `id` exists only as a tombstone, `worklogs show <id>` returns exit code `3`.

### `workledger issue-metadata list`

`issue-metadata list` must:

* read cached issue metadata from local SQLite only
* support `--issue` and the shared explicit time selectors
* when invoked with `--issue <KEY>` and no time selector, return the cached row for that issue only
* when invoked with time selectors, derive distinct issue keys from matching active local worklogs and return cached metadata for those issues
* return `filters`, `items`, and `total` in JSON output
* expose issue-metadata fields `issue_key`, `max_estimate_seconds`, `source_adapter_family`, `source_adapter_instance`, and `refreshed_at`

### `workledger worklogs add`

`worklogs add` must require:

* `--issue <KEY>`
* exactly one of `--started <LocalTimestamp>` or `--started-utc <RFC3339UTC>`
* `--duration <GoDuration>`
* `--description <text>`

MVP description input is flag-only.
Stdin and file-based description input are deferred.

Rules:

* the issue key must match the canonical `<PROJECTKEY>-<NUMBER>` grammar from `glossary.md`
* lowercase or otherwise non-canonical issue keys must fail validation rather than being normalized
* `--started` accepts `YYYY-MM-DDTHH:MM`, `todayTHH:MM`, `yesterdayTHH:MM`, `tomorrowTHH:MM`, and signed day offsets in `+NdTHH:MM` or `-NdTHH:MM` form
* `--started` resolves local civil time in `selection.timezone` when configured, otherwise in the system local timezone
* `--started-utc` accepts an explicit UTC RFC3339 timestamp only
* local and UTC started flags are mutually exclusive
* the started timestamp must normalize to one canonical UTC instant before storage
* invalid local civil times such as timezone gaps or ambiguous repeated-wall-clock timestamps must fail validation
* future `started_at` timestamps are allowed in MVP
* the duration must normalize to strictly positive whole seconds
* the duration must be greater than or equal to the effective configured minimum local worklog duration in seconds
* the effective minimum local worklog duration comes from `worklogs.minimum_duration_seconds` in YAML config and defaults to `900`
* the description must normalize to a single line by trimming outer whitespace and collapsing internal newlines or repeated whitespace to single spaces
* the normalized description must remain non-empty
* duplicate or overlapping active local worklogs must be detected before insert
* tombstones are ignored for duplicate and overlap validation
* overlap detection applies across all active local worklogs, not only rows with the same issue key
* two worklogs overlap when their canonical UTC time intervals intersect
* exact boundary touch is allowed and is not an overlap
* when duplicate or overlap detection finds a conflict, the command must fail with exit code `2` unless `--force` is set
* conflict failures must return a structured conflict summary that identifies the conflict reason, the attempted canonical record, and the conflicting local IDs
* overlap conflicts must also return the conflicting time windows
* `--force` must allow the operator to bypass duplicate or overlap rejection explicitly
* the created worklog `id` is always auto-generated by the CLI
* a successful add returns the created record

### `workledger worklogs update <id>`

`worklogs update` must:

* use patch-style flags `--issue`, `--started`, `--started-utc`, `--duration`, and `--description`
* support `--force` to bypass duplicate or overlap rejection explicitly
* require at least one patch flag
* reject an invocation that supplies both `--started` and `--started-utc`
* validate the full resulting record after patching
* succeed and return the canonical record even when normalization makes the patch a semantic no-op
* preserve the same configured minimum duration rule as add
* preserve the same non-empty normalized description invariant as add
* detect duplicate or overlapping active local worklogs for the resulting record before write
* exclude the row being updated from duplicate and overlap detection
* treat tombstoned IDs as not found for update behavior
* return exit code `3` when the worklog does not exist
* return the updated canonical record

### `workledger worklogs shift`

`worklogs shift` must:

* shift all selected active local worklogs by one signed duration delta
* reuse the active-worklog selectors from [Shared selectors](#shared-selectors)
* require at least one explicit selector so the command cannot shift the entire store accidentally
* require `--by <GoDuration>`
* support `--dry` for validation and preview without writing

Rules:

* only the selected active local worklogs are shifted
* every selected row uses the same signed delta
* shifting changes only the canonical `started_at` instant
* shifting must preserve `id`, `issue_key`, `duration_seconds`, and `description`
* shifting must preserve the relative spacing and ordering between selected worklogs
* the `--by` duration must normalize to non-zero whole seconds
* the full resulting active-worklog set must validate atomically before write
* the shifted result must satisfy the same configured minimum duration rule as `worklogs add`
* the shifted result must fail when the final active-worklog set contains duplicate or overlapping rows
* overlap validation applies against both selected and non-selected active worklogs using the final shifted timestamps
* tombstones are ignored for shift validation
* when no active worklogs match the selector set, the command must fail with exit code `3`
* when validation fails, the command must return exit code `2` and must not write partial results
* a successful non-dry shift returns the shifted canonical records

Dry-run output must preview at minimum:

* matched worklog count
* shift delta in normalized seconds
* per-row `id`
* per-row original `started_at`
* per-row shifted `started_at`
* per-row `duration_seconds`

### `workledger worklogs apply`

`worklogs apply` must:

* apply many local add operations from one payload
* remain local-only and mutate canonical SQLite worklogs only
* support `--dry` for validation and preview without writing
* support `--force` to bypass duplicate or overlap rejection explicitly
* support exactly one payload source per invocation: `--file <path>` or `--stdin`
* reject an invocation when neither or both payload sources are provided
* treat payload construction as external to the CLI
* validate the entire payload before any write
* execute all writes atomically when validation succeeds and `--dry` is not set

Payload rules:

* the payload must be JSON
* the payload must be one raw apply payload object
* the top-level payload must contain `adds`
* at least one add operation is required
* each `adds` item must include `issue_key`, exactly one of `started_at` or `started_at_utc`, `duration_seconds`, and `description`
* each `adds` item follows the same validation and normalization rules as `worklogs add`
* payload `started_at` values use the same local timestamp grammar as `--started`
* payload `started_at_utc` values use explicit UTC RFC3339
* payload `duration_seconds` values are whole seconds and must satisfy the same effective minimum local worklog duration as `worklogs add`
* each payload row must not include both `started_at` and `started_at_utc`
* duplicate and overlap validation must evaluate the final resulting active worklog set, including conflicts introduced between add operations inside the same payload
* when duplicate or overlap detection finds a conflict, the command must fail with exit code `2` unless `--force` is set
* conflict failures must return a structured conflict summary that identifies the conflict reason, the attempted canonical record, and the conflicting local IDs
* overlap conflicts must also return the conflicting time windows
* `--force` must allow the operator to bypass duplicate or overlap rejection for both payload-to-store conflicts and conflicts introduced between add operations inside the same payload
* when payload validation fails for any non-force-bypassable reason, the command returns exit code `2` and must not write partial results

Output rules:

* successful dry-run output returns a deterministic would-apply summary plus per-operation results
* successful non-dry output returns a deterministic applied summary plus per-operation results
* per-operation results identify the operation type and the resulting local worklog `id`

### `workledger worklogs delete <id>`

`worklogs delete <id>` must:

* delete exactly one active local worklog
* remove that worklog from the active worklog set
* support a default delete mode that writes one tombstone record preserving the deleted local `id`, `issue_key`, original `started_at`, and deletion timestamp
* support `--hard` to permanently remove the selected active local worklog without creating a tombstone
* remain non-interactive and delete immediately once validation passes
* return exit code `3` when the worklog does not exist
* return a deterministic success payload in `table` or `json` with `id`, `issue_key`, `deleted_at`, and `hard_delete`

Delete in MVP is local-only.
Default delete retains tombstones.
`--hard` explicitly bypasses tombstone retention and therefore does not preserve durable local delete intent for later reconcile behavior.
Provenance and sync-aware delete planning are deferred.
Duplicate and overlap enforcement applies to add, update, and apply only, not delete.

Batch delete is part of the MVP through the existing `workledger worklogs delete` command family.
Filtered batch delete reuses the active-worklog selectors from [Shared selectors](#shared-selectors).
Any non-empty valid selector subset from that active-worklog selector set is sufficient for batch delete.
Batch-delete matching semantics are shared with `worklogs list` and are independent from output-mode selection.
Filtered batch delete requires `--yes`.
Filtered batch delete also supports `--hard` to delete matched active worklogs without writing tombstones.
`--force` remains reserved for duplicate and overlap bypass on add, update, or apply.
Single-delete by `<id>` and filtered batch-delete selectors are mutually exclusive modes.
Filtered batch delete is a valid no-op when zero active worklogs match the selector set.
Filtered batch delete applies to active worklogs only and does not operate on tombstones.
Filtered batch delete remains valid when exactly one active worklog matches.
Filtered batch delete also supports `--dry` to preview the matched active worklogs without deleting them.
`--dry` is part of filtered batch-delete mode only and is mutually exclusive with `--yes`.
Batch-delete dry-run returns the full matched active records together with the matched count.
Dry-run output reuses the active-worklog record shape and adds explicit delete-preview metadata.
Executed filtered batch delete returns deleted IDs and deleted count rather than full deleted records.
Both dry-run and executed batch-delete output echo the effective normalized filters that were applied.
Filtered batch delete supports both `table` and `json` output in MVP.
Executed filtered batch delete is atomic in MVP.
Executed filtered batch delete writes one normal tombstone per deleted worklog unless `--hard` is set.

### `workledger worklogs restore`

`worklogs restore` must:

* operate in selector-based batch mode only
* reuse the active-worklog selector family from [Shared selectors](#shared-selectors)
* require at least one explicit time selector
* optionally accept `--issue <KEY>`
* select tombstones by their original `started_at_utc`
* require `--yes` for execution
* support `--dry` preview and make it mutually exclusive with `--yes`
* support `--force` to bypass duplicate or overlap rejection explicitly
* validate the full restore set against active rows and against conflicts inside the restore set unless `--force` is set
* insert active local worklogs using the original `id`, `issue_key`, `started_at_utc`, `duration_seconds`, and `description`
* delete the matching tombstones in the same transaction when execution succeeds
* remain a valid no-op when zero tombstones match
* return executed success payloads with restored IDs and restored count rather than full active records

Restore is local-only in MVP.
`worklogs restore` does not accept `--only-deleted`.
Restore dry-run returns the active-worklog record shape and adds explicit restore-preview metadata plus the tombstone deletion timestamp.


## CLI and Acceptance

### Command Surface

The canonical MVP command surface is defined in [product.md#mvp-command-surface](product.md#mvp-command-surface).
No commands beyond the documented MVP worklog, status, and plan surfaces are part of the current MVP.
The CLI implementation must use Cobra for root, group, and leaf command construction.
The root command should not expose Cobra's default `completion` command in MVP.
`workledger version` does not depend on config presence or validity.
Bare `workledger` must render root help and exit `0`.
`workledger -h`, `workledger --help`, and `workledger help` must render the same root help and exit `0`.
Every MVP root, group, and leaf command must also accept `-h` and `--help`, render command-specific plain-text help to stdout, and exit `0`.
Help output must bypass config loading, SQLite access, normal argument validation, and `--output` mode selection.

### Output and Logging Contract

* user-facing command output goes to stdout
* logs and diagnostics go to stderr
* `--output json` must remain valid JSON on stdout without mixed log lines
* table output also belongs on stdout

MVP commands are local-only.
Any future concurrency is deferred with remote adapter work.

For `workledger worklogs list` and `workledger worklogs search`, MVP date-window filters accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, and signed day offsets such as `-7d` or `+2d`.
The shared shortcut family also accepts `--current-week`, `--last-week`, `--current-month`, and `--last-month` with local calendar boundaries in the effective selection timezone.
Deferred local follow-up commands such as `workledger worklogs context` and `workledger worklogs shift` reuse that same date-window grammar.
For local worklog writes, `started_at` accepts `YYYY-MM-DDTHH:MM`, `todayTHH:MM`, `yesterdayTHH:MM`, `tomorrowTHH:MM`, and signed day offsets such as `-7dT09:15`.
For explicit UTC worklog writes, `started_at_utc` accepts RFC3339 UTC.

Stable machine-readable success payloads and minimum error-payload guarantees are defined in [api.md#json-contracts](api.md#json-contracts).

### Error Model and Exit Codes

Keep exit codes small and stable.

* `0` success
* `1` unexpected failure
* `2` validation or input failure
* `3` not found
* `4` reserved for future auth failure
* `5` reserved for future external or connectivity failure
* `6` reserved for future partial success

Write-path duplicate and overlap conflicts are validation failures.
They must use exit code `2` with machine-readable conflict details in the selected output format.

### Delivery Phases

#### Phase 1: Workspace bootstrap

Deliver:

* root command
* version output with raw string table mode and fixed JSON object mode
* config path resolution
* starter YAML bootstrap
* SQLite path provisioning and empty schema creation
* strict MVP-only config validation with batch error reporting

Done when:

* `workledger init` works
* `workledger version` works
* `workledger config validate` works

#### Phase 2: Local worklog CRUD

Deliver:

* canonical local worklog storage in SQLite
* default tombstone retention for deleted worklogs with explicit hard-delete bypass
* local worklog listing and inspection by stable local UUID
* local worklog description search by literal case-insensitive substring
* deleted-worklog visibility through `worklogs list --only-deleted`
* field selection through `worklogs list --fields`
* local-time default `started_at` input and rendering through the effective selection timezone
* explicit UTC timestamp input through `started_at_utc`
* manual local worklog creation
* local worklog update
* local worklog delete
* hard local worklog delete through `workledger worklogs delete --hard`
* guarded batch delete through `workledger worklogs delete`
* batch-delete dry-run through `workledger worklogs delete --dry`
* duplicate and overlap detection with explicit `--force` bypass on write commands

Done when:

* `workledger worklogs list` works with an explicit time selector
* `workledger worklogs search <query>` works without a required time selector
* `workledger worklogs show <id>` works
* `workledger worklogs add` works
* `workledger worklogs update <id>` works
* `workledger worklogs delete <id>` works

### Explicitly Deferred

Do not include these in the current MVP:

* `workledger worklogs context`
* `workledger worklogs shift`
* `workledger worklogs apply`
* adapter connectivity or health checks
* adapter pull
* outbound delivery
* routing
* saved plans
* retries
* delivery correlation
* tombstone-specific top-level commands


## JSON Contracts

This file defines the stable machine-readable MVP output contract for `--output json`.

### Shared rules

In `--output json`, success payloads are fixed per command in MVP.
Unless a command section below says otherwise, the top-level JSON value is one object with exactly the documented keys.
All timestamps in JSON must use RFC3339.
JSON output must remain valid JSON on stdout without mixed log lines.

Exit codes remain defined in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).

### Shared record shapes

Active worklog record:

```json
{
  "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
  "issue_key": "ABC-123",
  "started_at": "2026-05-03T09:00:00+03:00",
  "started_at_utc": "2026-05-03T06:00:00Z",
  "duration_seconds": 3600,
  "description": "Investigated bug"
}
```

Deleted tombstone record:

```json
{
  "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
  "issue_key": "ABC-123",
  "deleted_at": "2026-05-03T07:30:00Z"
}
```

### Success payloads

`workledger version`:

```json
{
  "version": "0.1.0"
}
```

`workledger init`:

```json
{
  "config": "created",
  "sqlite": "created",
  "config_path": "/Users/alice/.config/workledger/config.yaml",
  "sqlite_path": "/Users/alice/.local/share/workledger/worklogs.db"
}
```

`config` is `created` or `reused`.
`sqlite` is `created`, `reused`, or `repaired`.

`workledger config validate`:

```json
{
  "valid": true,
  "config_path": "/Users/alice/.config/workledger/config.yaml",
  "effective": {
    "default_output": "table",
    "selection": {
      "timezone": "Europe/Vilnius"
    },
    "storage": {
      "sqlite_path": "/Users/alice/.local/share/workledger/worklogs.db"
    },
    "worklogs": {
      "minimum_duration_seconds": 900
    }
  }
}
```

`workledger worklogs list`:

```json
{
  "filters": {
    "raw": {
      "issue": "ABC-123",
      "today": false,
      "yesterday": false,
      "current_week": false,
      "last_week": false,
      "current_month": false,
      "last_month": false,
      "from": "2026-05-03",
      "to": "2026-05-03",
      "only_deleted": false,
      "fields": null
    },
    "effective": {
      "issue_key": "ABC-123",
      "from": "2026-05-03T00:00:00+03:00",
      "to": "2026-05-03T23:59:59+03:00",
      "timezone": "Europe/Vilnius",
      "only_deleted": false,
      "fields": null
    }
  },
  "items": [],
  "total": 0
}
```

Rules:

* default list mode uses active worklog records in `items`
* `--only-deleted` uses deleted tombstone records in `items`
* when `--fields` is set, each `items[*]` object contains only the selected item fields

`workledger worklogs search <query>`:

```json
{
  "filters": {
    "raw": {
      "query": "api docs",
      "issue": null,
      "today": false,
      "yesterday": false,
      "current_week": false,
      "last_week": false,
      "current_month": false,
      "last_month": false,
      "from": null,
      "to": null,
      "only_deleted": false,
      "fields": null
    },
    "effective": {
      "query": "api docs",
      "timezone": "Europe/Vilnius",
      "only_deleted": false,
      "fields": null
    }
  },
  "items": [],
  "total": 0
}
```

Rules:

* default search mode uses active worklog records in `items`
* `--only-deleted` uses deleted tombstone records in `items`
* `filters.raw.query` preserves the operator-supplied query argument
* `filters.effective.query` contains the trimmed normalized query used for literal substring matching
* when `--fields` is set for active results, each `items[*]` object contains only the selected item fields

`workledger totals --adapter=clockify`:

```json
{
  "filters": {
    "raw": {
      "adapter": "clockify",
      "today": false,
      "yesterday": false,
      "current_week": false,
      "last_week": false,
      "current_month": false,
      "last_month": false,
      "from": "2026-05-01",
      "to": "2026-05-31"
    },
    "effective": {
      "adapter": "clockify",
      "from": "2026-05-01T00:00:00+03:00",
      "to": "2026-05-31T23:59:59+03:00",
      "timezone": "Europe/Vilnius"
    }
  },
  "summary": {
    "state": "match",
    "local_total_seconds": 28800,
    "remote_total_seconds": 28800,
    "delta_seconds": 0,
    "running_remote_entry_detected": false
  },
  "days": [
    {
      "date": "2026-05-01",
      "state": "match",
      "local_total_seconds": 14400,
      "remote_total_seconds": 14400,
      "delta_seconds": 0
    }
  ]
}
```

`workledger totals --adapter=jira-data-center`:

```json
{
  "filters": {
    "raw": {
      "adapter": "jira-data-center",
      "instance": "main",
      "today": false,
      "yesterday": false,
      "current_week": false,
      "last_week": true,
      "current_month": false,
      "last_month": false,
      "from": null,
      "to": null
    },
    "effective": {
      "adapter": "jira-data-center",
      "instance": "main",
      "from": "2026-04-27T00:00:00+03:00",
      "to": "2026-05-03T23:59:59+03:00",
      "timezone": "Europe/Vilnius"
    }
  },
  "summary": {
    "state": "match",
    "local_total_seconds": 14400,
    "remote_total_seconds": 14400,
    "delta_seconds": 0,
    "running_remote_entry_detected": false
  },
  "days": []
}
```

Rules:

* the top-level object contains exactly `filters`, `summary`, and `days`
* `summary.state` is one of `match`, `mismatch`, or `indeterminate`
* `days` is ordered chronologically by local date in the effective selection timezone
* each `days[*].state` is one of `match`, `mismatch`, or `indeterminate`
* days with zero local and zero remote time may be omitted unless an indeterminate running remote entry overlaps that day

`workledger worklogs context`:

```json
{
  "filters": {
    "raw": {},
    "effective": {}
  },
  "settings": {
    "timezone": "Europe/Vilnius",
    "day_start": "08:00",
    "day_end": "17:00",
    "lunch": {
      "start": "12:00",
      "end": "13:00"
    }
  },
  "planning": {
    "issue_order": [
      "AAPP-123",
      "AAPP-124"
    ],
    "issues": [
      {
        "issue_key": "AAPP-123",
        "max_estimate_seconds": 7200,
        "summary": "Homepage hero copy polish"
      },
      {
        "issue_key": "AAPP-124",
        "max_estimate_seconds": null
      }
    ],
    "minimum_duration_seconds": 900,
    "payload_contract": "apply_raw_adds_v1",
    "slot_order": "date_asc,start_asc"
  },
  "summary": {
    "day_count": 1,
    "worklog_count": 0,
    "booked_seconds": 0,
    "free_seconds": 28800,
    "collision_count": 0
  },
  "days": [
    {
      "date": "2026-05-03",
      "worklogs": [],
      "booked_seconds": 0,
      "free_slots": [
        {
          "start": "2026-05-03T08:00:00+03:00",
          "end": "2026-05-03T12:00:00+03:00",
          "duration_seconds": 14400
        },
        {
          "start": "2026-05-03T13:00:00+03:00",
          "end": "2026-05-03T17:00:00+03:00",
          "duration_seconds": 14400
        }
      ],
      "collisions": []
    }
  ]
}
```

`workledger worklogs show <id>`:

* JSON output is exactly one active worklog record

`workledger worklogs add`:

* JSON output is exactly one active worklog record

`workledger worklogs update <id>`:

* JSON output is exactly one active worklog record

`workledger worklogs shift`:

```json
{
  "filters": {
    "raw": {},
    "effective": {}
  },
  "dry_run": true,
  "delta_seconds": 900,
  "matched": 1,
  "items": [
    {
      "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
      "issue_key": "ABC-123",
      "started_at_before": "2026-05-03T09:00:00+03:00",
      "started_at_after": "2026-05-03T09:15:00+03:00",
      "started_at_utc_before": "2026-05-03T06:00:00Z",
      "started_at_utc_after": "2026-05-03T06:15:00Z",
      "duration_seconds": 3600,
      "description": "Investigated bug"
    }
  ]
}
```

Rules:

* dry-run `items[*]` use the preview shape above
* non-dry success keeps the same top-level keys but uses active worklog records in `items`

`workledger worklogs apply`:

```json
{
  "dry_run": false,
  "summary": {
    "add_count": 1
  },
  "items": [
    {
      "op": "add",
      "index": 0,
      "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
      "record": {
        "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
        "issue_key": "ABC-123",
        "started_at": "2026-05-03T09:00:00+03:00",
        "started_at_utc": "2026-05-03T06:00:00Z",
        "duration_seconds": 3600,
        "description": "Investigated bug"
      }
    }
  ]
}
```

Rules:

* both dry-run and non-dry success use the same schema
* `summary.add_count` is the number of add operations in the effective payload
* each `items[*]` object identifies the zero-based payload row in `index`

`workledger worklogs delete <id>` has three success shapes:

Single delete by `id`:

```json
{
  "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
  "issue_key": "ABC-123",
  "deleted_at": "2026-05-03T07:30:00Z",
  "hard_delete": false
}
```

Rules:

* single delete uses the same shape for default and hard delete
* `hard_delete` is `true` when `--hard` is used
* `deleted_at` is the command's effective deletion timestamp in both modes

Filtered batch delete dry-run:

```json
{
  "filters": {
    "raw": {},
    "effective": {}
  },
  "dry_run": true,
  "hard_delete": false,
  "matched": 1,
  "items": [
    {
      "delete_preview": true,
      "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
      "issue_key": "ABC-123",
      "started_at": "2026-05-03T09:00:00+03:00",
      "started_at_utc": "2026-05-03T06:00:00Z",
      "duration_seconds": 3600,
      "description": "Investigated bug"
    }
  ]
}
```

Filtered batch delete executed:

```json
{
  "filters": {
    "raw": {},
    "effective": {}
  },
  "dry_run": false,
  "hard_delete": false,
  "deleted": 1,
  "items": [
    {
      "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae"
    }
  ]
}
```

Rules:

* filtered batch delete dry-run and executed output use the same shapes whether or not `--hard` is set
* `hard_delete` is `true` when `--hard` is part of the effective delete mode

Restore dry-run:

```json
{
  "filters": {
    "raw": {},
    "effective": {}
  },
  "dry_run": true,
  "matched": 1,
  "items": [
    {
      "restore_preview": true,
      "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae",
      "issue_key": "ABC-123",
      "started_at": "2026-05-03T09:00:00+03:00",
      "started_at_utc": "2026-05-03T06:00:00Z",
      "duration_seconds": 3600,
      "description": "Investigated bug",
      "deleted_at": "2026-05-04T10:00:00Z"
    }
  ]
}
```

Restore executed:

```json
{
  "filters": {
    "raw": {},
    "effective": {}
  },
  "dry_run": false,
  "restored": 1,
  "items": [
    {
      "id": "6f8dbb6a-6c09-4d1d-b0f2-6d1dc0f1b8ae"
    }
  ]
}
```

### Error payload guarantees

This file does not define one fully fixed shared error envelope for every command.
The MVP guarantees the following minimum machine-readable error behavior:

* when `--output json` is selected, error output must still be valid JSON on stdout
* `workledger config validate` failure returns one JSON error payload with all discovered validation errors
* duplicate and overlap failures return a structured conflict summary that identifies the conflict reason, the attempted canonical record, and the conflicting local IDs
* overlap failures also include the conflicting time windows

Command-specific error semantics remain defined in [api.md#configuration-and-storage](api.md#configuration-and-storage), [api.md#local-worklogs](api.md#local-worklogs), and [api.md#cli-and-acceptance](api.md#cli-and-acceptance).


## Reconcile Planning and Review

This document defines deferred shared reconcile requirements for `workledger plan reconcile` and `workledger plan show`.

The current MVP does not include plan commands. Shared CLI conventions such as stdout/stderr handling and common exit codes remain in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).
Shared terminology and key grammar live in [glossary.md](glossary.md).

### Scope added beyond MVP

Deferred reconcile planning adds:

* local SQLite worklogs as canonical state
* adapter worklog pull planning into SQLite
* manual local worklog creation
* saved plan creation in SQLite
* saved plan review before execution
* scope classification into `ready`, `invalid`, `blocked`, `skipped`, and `check_failed`

### `workledger plan reconcile`

`plan reconcile` is the only command that may create a remote-sync plan.

It must require:

* exactly one of `--pull` or `--push`
* exactly one `--adapter=<family>`
* an explicit selected date window supplied either by `--from` plus `--to` or by exactly one date-window shortcut selector

Each saved plan must persist:

* `plan_direction` as `pull` or `push`
* the selected target adapter family

`plan reconcile` must always be non-destructive.
After command-level validation succeeds, it must persist exactly one saved plan unless a reporting reconcile resolves only non-actionable scopes.

There is no separate `plan reconcile --dry-run` mode.

#### Common reconcile responsibilities

`plan reconcile` is the only command that may:

* validate the selected adapter family and its config
* choose one reconcile direction before any adapter inspection begins
* apply local date and field filters where that direction uses local selection
* fetch remote worklogs from the selected adapter family
* group selected rows into reconcile scopes
* compute the planned diff or merge result for each scope
* classify reconcile scopes
* persist non-importable remote observations as saved plan findings when the selected adapter returns rows that cannot be normalized into canonical candidate rows
* materialize the saved plan payload snapshot for each scope
* materialize the saved inspection summary for each scope
* persist a saved plan in SQLite

#### Push responsibilities

When `--push` is selected, `plan reconcile` may:

* load canonical local worklogs from SQLite for delivery planning
* load deleted-worklog tombstones from SQLite for remote cleanup planning
* optionally accept `--only-deleted` to limit push planning to tombstone-backed cleanup scopes
* determine the routing adapter family from `--adapter`
* select the named route profile from `--route-profile` when provided, otherwise use that routing adapter family's configured `default` profile
* resolve one or more target adapter instances from that route profile using the selected adapter family's routing or selection rules
* fetch current remote worklogs from each resolved target adapter instance
* compute the local-versus-remote diff for each selected scope against its resolved target adapter instance

Push planning concurrency rules:

* use goroutines only for remote adapter I/O
* remote reads and advisory checks may run concurrently per resolved target adapter instance
* all planning-time remote HTTP work must use the same fixed tool-defined global concurrency limit
* local scope grouping, diffing, classification, and saved-plan persistence remain single-threaded
* push planning must not spawn per-scope goroutines after remote target-instance reads complete

#### Pull responsibilities

When `--pull` is selected, `plan reconcile` may:

* inspect remote worklogs from the selected adapter family inside that adapter family's configured source scope
* normalize remote observations into the shared canonical allocation contract
* compare those normalized observations with the current local canonical ledger
* produce a saved merge plan without mutating local canonical worklogs during reconcile

### Push selection model

Push planning reuses the local worklog selector semantics for date and issue filtering.

Rules:

* active local worklogs are selected by default
* deleted tombstones may participate in push planning for remote cleanup scopes even when `--only-deleted` is not selected
* tombstone date filtering evaluates against the deleted worklog's original `started_at`
* delete intent is derived from local tombstones, not from remote state alone
* active selected local worklogs are grouped into scopes by target adapter family, target adapter instance, issue key, and the selected reconcile time window
* a delete-only scope exists when tombstones leave no active local rows in that same target, issue, and selected reconcile window

### Pull planning model

Pull planning uses the selected adapter family's configured source scope.

Rules:

* remote rows are grouped into canonical reconcile scopes after normalization
* pull planning may compare remote rows against current local canonical rows in the same issue and reconcile-window scope
* local date and issue filters, when provided, apply to the normalized canonical candidate rows rather than to adapter-native remote row identity
* pull planning must never treat remote row IDs or remote row boundaries as canonical local identity
* a pull plan must not recreate a local allocation that is protected by a matching tombstone

### Push validation order

Command-level preconditions run before per-scope validation:

1. normalize selector flags and resolve the effective local time window
2. require `--push`
3. require exactly one `--adapter=<family>`
4. load the selected local records from SQLite, including tombstones for delete-only scope detection
5. build reconcile scopes from the selected active rows and tombstones

Then validate each selected scope in this order:

1. scope issue key exists and is well-formed
2. each active row in the scope has parseable canonical started time
3. each active row in the scope has positive canonical duration
4. each active row in the scope has non-empty canonical description after trimming
5. the target adapter instance resolves for that scope when the selected adapter family requires routing or another scope resolver
6. the resolved target adapter instance exists in current YAML config
7. required issue existence and worklog capability checks pass
8. target-adapter diff planning classifies the scope as create, replace, delete, skip, or blocked
9. required remote cleanup checks pass

### Pull validation order

Command-level preconditions run before per-scope validation:

1. require `--pull`
2. require exactly one `--adapter=<family>`
3. validate the selected adapter config
4. fetch remote worklogs from the adapter's configured source scope
5. normalize remote observations into canonical candidate rows
6. apply local issue and date filters to the normalized candidate rows
7. build reconcile scopes from the normalized candidate rows

Then validate each selected scope in this order:

1. each normalized row has a well-formed canonical issue key
2. each normalized row has parseable canonical started time
3. each normalized row has positive canonical duration
4. each normalized row has non-empty canonical description after trimming
5. the selected adapter family can read the required source scope
6. canonical merge planning classifies the scope as merge, skip, or blocked

### Planning-specific routing and reporting rules

Base local CRUD behavior remains defined by MVP.
Shared pull ownership, canonical-identity, and local-edit-preservation rules remain defined in [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules).
Selection timezone behavior remains defined in [architecture.md#configuration-and-state](architecture.md#configuration-and-state).
Issue-key grammar remains defined in [glossary.md](glossary.md).

Remote adapters are import sources and optional delivery targets during planning.
Routing behavior is adapter-family-specific.

Rules:

* Jira-family planning may resolve the target adapter instance from the local issue key by scanning the selected adapter family's configured instance-local routing
* Jira-family planning may also resolve one fixed target issue from a reporting-target rule selected by route profile
* adapter families that do not route from issue key alone must use their own resolver contract
* Clockify planning does not infer its target from the issue key alone
* Clockify planning may resolve a Clockify project inside the single configured Clockify target from the local issue-key prefix using `clockify.project_mapping.issue_prefixes`
* Clockify planning may use `clockify.project_mapping.default_project` as a fallback project inside the single configured Clockify target
* Clockify planning may ensure an exact issue-key tag such as `AAPP-123` exists or can be created when the Clockify config allows that behavior
* Jira-family push planning must not infer a target adapter instance without configured routing

Jira-family reporting delivery rules:

* `--route-profile` is push-only
* a reporting-target rule may remap many local issue keys into one fixed target issue on one configured Jira target adapter instance in the selected Jira adapter family
* a reporting-target rule matches canonical local issue-key prefixes regardless of which adapter family originally supplied or owns those local rows for pull or totals scope
* a reporting route profile must not mix issue-preserving routing and reporting-target remap rules
* when a reporting-target rule matches, the local issue key remains part of the canonical payload identity but remote diff and cleanup scope use the resolved target issue
* when multiple matched source issue keys resolve to the same target adapter instance, target issue, and selected window, planning must aggregate them into one saved remote execution scope
* reporting delivery is an additive mirror only and must not mutate source-system worklogs
* each canonical local worklog row is delivered as one remote reporting worklog row
* the resolved target issue is assumed to be dedicated to `workledger`-managed reporting worklogs
* for example, canonical local `APPS-*` rows may be pushed with `--adapter=jira-data-center --route-profile=aciu_reporting` into `jira_data_center.instances.ito_jira` issue `ACIU-123` even when `APPS` is owned by `jira_cloud.instances.maxima_lt_jira` for issue-preserving routing
* reporting push payload materialization follows the saved payload contract in [features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry](features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry)
* reporting delivery may use arbitrary operator-selected date windows
* reporting window membership is determined by worklog `started_at` only and never by interval overlap or row-splitting across boundaries
* when no routing or reporting rule matches one selected local issue key, only that affected scope is `blocked`
* unmapped reporting scopes must not fail the whole plan when other selected scopes can still be planned
* reporting-loop prevention relies on Jira pull scope implicitly excluding reporting target issues from canonical local import on the owning adapter instance
* overlapping saved reporting plans are valid; each new plan compares current local and remote state independently for the selected scope
* shared reporting targets compare equality against the full normalized target-issue window scope as one aggregated row set, not per contributing source issue
* when a new reporting plan observes exact normalized row-set equality in its selected target issue and window scope, that scope is non-actionable even if another saved plan overlaps it
* a reporting reconcile that finds only non-actionable scopes must return a no-op result and must not persist a saved plan
* that no-op result must return exit code `0`, no plan ID, and a deterministic stdout summary that states the route profile, target adapter family, resolved target adapter instances, selected window, matched scope count, actionable scope count `0`, and an explicit already-in-sync result
* in JSON output, that no-op result must include `plan_created=false` and a machine-readable reason such as `already_in_sync`
* when a reporting reconcile matches zero mapped scopes for the selected rows, it must use the same no-plan result shape with a distinct machine-readable reason such as `no_matching_routes`
* when a reporting reconcile contains any `blocked`, `invalid`, or `check_failed` scope, it must persist a saved plan even if there are zero actionable `ready` scopes
* when push reconcile saves a plan containing one or more `check_failed` scopes, it must keep stdout in the normal saved-plan shape and return exit code `6`

`plan reconcile` must not:

* re-read remote sources other than the selected comparison or import scope for the current plan
* derive canonical worklogs directly from remote source records outside the saved plan flow

### Scope classification

Saved plan items must persist:

* `plan_direction`
* `planned_action`

Each saved plan item represents one reconcile scope, not one local worklog.

`planned_action` must use:

* `merge` or `none` for `plan_direction=pull`
* `create`, `replace`, `delete`, or `none` for `plan_direction=push`

#### Pull classification

`plan reconcile --pull` compares the normalized remote row set and the current local canonical row set within the same saved scope.

Pull inspection outcomes:

* exact normalized row-set equality in the scope => `skipped` with `planned_action=none`
* remote rows differ from current local canonical rows and can be merged without violating canonical merge rules => `ready` with `planned_action=merge`
* incomplete remote visibility for the selected import scope => `check_failed`
* a required local safety rule blocks the merge => `blocked`

#### Push classification

`plan reconcile --push` compares the normalized local row set and the current remote row set within the same saved scope:

* target adapter family
* target adapter instance
* target issue
* selected reconcile time window

Push inspection outcomes:

* exact normalized row-set equality in the scope => `skipped` with `planned_action=none`
* local rows exist and the remote scope is empty => `ready` with `planned_action=create`
* local rows exist and the remote scope is not exactly equal => `ready` with `planned_action=replace`
* local rows do not exist, explicit delete intent exists, and the remote scope is non-empty => `ready` with `planned_action=delete`
* local rows do not exist, explicit delete intent exists, and the remote scope is empty => `skipped` with `planned_action=none` and exact-match reporting
* incomplete remote visibility for the selected comparison scope => `check_failed`
* a required cleanup action cannot be proven safe within the saved scope => `blocked`

For Jira Cloud push reconcile specifically, auth, permission, not-found, transport, or other remote read failures may be captured per scope as `check_failed` instead of aborting the whole reconcile:

* `CurrentUser` failure on one resolved Jira Cloud instance marks every planned scope on that instance as `check_failed`
* `ListIssueWorklogs` failure marks only that selected scope as `check_failed`
* other scopes on the same or other resolved Jira Cloud instances continue to normal `ready|skipped|blocked` classification

Rules:

* final correctness is exact local-versus-remote row-set equality within the saved scope
* per-entry remote worklog identity is not part of reconcile identity
* remote cleanup and replacement are scoped only by target adapter instance, target issue, and saved reconcile time window
* for reporting delivery, cleanup within the saved scope deletes all currently visible remote worklogs in that target issue and window before recreating the canonical local rows
* reporting cleanup within one saved window must not delete remote worklogs outside that saved window
* active local drift may authorize `replace` cleanup within the saved issue and selected reconcile window even when no tombstone exists
* tombstones are only required for delete-only scopes where no active local rows remain
* no local rows and no tombstone-backed delete intent must not trigger cleanup

`--not-pushed` is a push-only render shortcut.
It must limit the displayed actionable view to `ready` items whose `planned_action` is `create`, `replace`, or `delete`.
Non-actionable items still remain persisted in the saved plan.

### Required remote checks

A `ready` classification also requires:

* source-read capability for `plan_direction=pull`
* target issue existence when `plan_direction=push` and the planned action is `create` or `replace`
* target worklog capability when `plan_direction=push` and the planned action is `create` or `replace`
* delete capability when `plan_direction=push` and cleanup is required
* create capability when `plan_direction=push` and create or replacement is required

Rules:

* unsupported or unreliable checks classify the item as `check_failed`
* supported checks that fail a business rule classify the item as `blocked`
* auth, permission, transport, or adapter API support failures for these checks are `check_failed`
* cleanup scope is limited to the resolved target adapter instance and the saved issue/window scope on that plan item
* delete-only planning is driven by tombstone-backed local delete intent
* reconcile may only mutate the resolved target adapter instance saved on that planned item

Examples of `blocked`:

* the target issue does not exist for a scope that requires `create` or `replace`
* the adapter confirms that worklog creation is not allowed for the target issue
* the adapter confirms that delete capability is not available for a scope that requires cleanup
* delete-only cleanup was requested, but the selected scope is outside the allowed target adapter instance or saved issue/window scope
* a pull scope would blindly recreate a local allocation that is protected by a tombstone

### `workledger plan show`

`plan show` must load a saved plan and render the saved reconciliation report without new external requests.

It must:

* load the requested plan ID when provided
* load the most recent saved plan when no plan ID is provided
* use the saved plan payload snapshot and saved inspection summary from `plan reconcile`
* render deterministic output suitable for operator review

The report must state, per scope:

* `plan_status`
* `planned_action`
* comparison status as `match`, `merge_needed`, `missing`, `diff`, `conflict`, `not_checked`, or `check_failed`
* target adapter family
* target issue
* saved reconcile time window
* local row count
* remote row count
* execution state

Rules:

* local row count and total seconds represent active local worklogs only
* tombstone-backed delete intent must not increase local counts in the saved report
* a delete-only push scope whose remote scope is already empty must report exact equality through `comparison_status=match`

Comparison status mapping:

* `skipped` with `planned_action=none` after exact scope equality => `match`
* `ready` with `planned_action=merge` => `merge_needed`
* `ready` with `planned_action=create` => `missing`
* `ready` with `planned_action=replace` => `diff`
* `ready` with `planned_action=delete` => `conflict`
* `invalid` or `blocked` before remote comparison completes => `not_checked`
* `check_failed` => `check_failed`

### Frozen plan rule

`plan apply` must consume the saved scope definition, saved plan payload snapshot, and saved inspection snapshot.

Changes in local SQL or remote adapter state after planning do not alter an existing saved plan.
The frozen plan preserves the saved reconcile scope and saved payload, but it does not freeze exact remote row identities.

To deliver or import updated state, the operator must create a new plan.


## Reconcile Apply and Retry

This document defines deferred shared reconcile-execution requirements for `workledger plan apply`, `workledger plan list`, and `workledger plan retry`.

The current MVP does not include reconcile commands. Shared CLI conventions remain in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).
Deferred live progress behavior for long-running remote batch work lives in [ux.md#progress-reporting](ux.md#progress-reporting).

### Saved payload contract

Each saved plan item carries one saved reconcile scope plus the materialized plan payload for that scope.

Each normalized payload row consists of:

* normalized issue key
* started time in UTC
* duration in whole seconds
* normalized description in the adapter-specific comment field or the canonical local description field

Rules:

* `plan_direction` is persisted as `pull` or `push`
* `content_hash` is computed from exactly the saved normalized row-set payload fields
* `plan apply` must execute exactly the saved normalized row set
* selection timezone affects planning only, not the saved plan payload
* a timezone configuration change after planning must not alter execution for an existing saved plan
* for reporting push payloads, normalized description must preserve the source issue key in the remote comment text using the fixed format `<ISSUE_KEY> | <description>`
* reporting description normalization must avoid exact double-prefixing when the canonical local description already starts with the same `<ISSUE_KEY> | ` prefix

Plans may contain items for more than one target adapter instance within the selected adapter family.
Apply and retry must use the target adapter family and target adapter instance saved on each plan item.
They must not re-run routing.

Each saved plan item must retain at minimum:

* issue key
* `plan_direction`
* target adapter family
* target adapter instance when one exists for that scope
* saved reconcile window
* `plan_status`
* `planned_action`
* reason code
* reason detail
* saved plan payload rows for that scope
* `delivery_key`
* `content_hash`
* saved inspection summary for review

### Execution identity and state

Use two values:

* `delivery_key` for stable saved-plan execution identity
* `content_hash` for the materialized saved payload

Recommended `delivery_key` inputs:

* `plan_direction`
* target adapter family
* target adapter instance
* issue key
* saved reconcile window
* planned action
* `content_hash` when the saved action carries rows

Execution state rules:

* `plan_status` is immutable after planning
* attempt history is append-only
* attempt states are `pending`, `succeeded`, `failed`, and `uncertain`
* derived execution states are `not_attempted`, `pending`, `succeeded`, `failed`, and `uncertain`
* once an item has succeeded, it is terminal and may not be retried

### `workledger plan list`

`plan list` must load saved-plan metadata from SQLite only.
It must not make external adapter requests.

It must:

* render saved plans ordered by `created_at desc`, then stable plan ID
* include deterministic summary counts at minimum for total items, ready items, and terminally succeeded items
* expose `plan_direction`, the saved target adapter family, and the saved reconcile time window
* optionally support the shared date-window selectors `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to` against saved plan `created_at` in the effective selection timezone
* support operator review before `plan show`, `plan apply`, or `plan retry`

### `workledger plan apply`

`plan apply` must:

* load the requested plan ID when provided, otherwise the most recent saved plan
* fail before execution when the saved plan was created under a different effective configuration fingerprint than the current one
* build tasks only from `ready` items whose current execution state is `not_attempted`
* succeed as a no-op when the saved plan contains zero executable `ready` items
* use the saved scope definition and saved payload snapshot for execution
* execute according to the saved `plan_direction`
* execute only one saved plan at a time
* record delete and create outcomes separately when one saved push item requires both steps
* continue executing other eligible scopes when one scope fails, and persist per-scope results independently
* build a deterministic execution order by resolved target issue key, then target adapter family, then target adapter instance, then window start, then stable saved plan item ID before running tasks
* schedule concurrency at the saved-plan-item level and never split one saved plan item into multiple concurrent goroutines
* allow at most one active goroutine per resolved target issue key at a time
* return exit code `6` when one apply execution ends with mixed per-scope success and failure results
* include `failed_count` and `mixed_result` in JSON output

It must not:

* re-read adapter imports outside the saved pull plan payload
* mutate any adapter other than the target adapter instance saved on each push plan item
* re-select the target adapter family
* re-resolve routing or target-instance selection
* reclassify validation status
* re-plan cleanup scope or saved payload

#### Pull apply rules

When `plan_direction=pull`, `plan apply` must:

* merge the saved normalized remote payload into canonical local SQLite state
* use the canonical merge rules captured at planning time
* preserve local canonical edits when the saved merge scope conflicts with later local edits
* refuse blind recreation of a local allocation protected by a tombstone

It must not:

* fetch a fresh remote row set for the same scope during apply
* overwrite local canonical rows automatically when the saved merge would violate canonical merge rules

#### Push apply rules

When `plan_direction=push`, `plan apply` must:

* re-discover current remote worklogs inside the saved issue/window scope at execution time when cleanup is required
* apply any saved remote cleanup on the target adapter instance saved on that plan item before creating replacement worklogs

Replace-specific rules:

* `planned_action=replace` must list current remote worklogs in the saved issue/window scope at apply time
* apply must delete all currently visible remote worklogs in that saved scope before recreating the saved local rows
* apply must never silently downgrade a saved replace into a create-only action

Delete-specific rules:

* `planned_action=delete` must list current remote worklogs in the saved issue/window scope at apply time
* `planned_action=delete` may originate from a deleted local-worklog tombstone rather than an active local worklog
* delete applies only to the target adapter instance saved on that plan item
* apply must delete all currently visible remote worklogs in that saved scope
* a delete plan item must not create a replacement worklog

Configuration fingerprint details live in [architecture.md#configuration-and-state](architecture.md#configuration-and-state).

### `workledger plan retry`

`plan retry` must:

* load the requested saved plan by ID
* fail before execution when the saved plan was created under a different effective configuration fingerprint than the current one
* operate on saved plan items only and never reselect local worklogs or remote source rows
* require an explicit retry scope such as `--only failed` or `--only uncertain`

### Retry model

Plain `plan apply` must never start a new execution for an item whose effective state is `pending`, `failed`, or `uncertain`.

Retry commands operate on saved plan items, not reselection:

* `workledger plan retry <id> --only failed` processes `ready` items with `execution_state=failed`
* `workledger plan retry <id> --only uncertain` processes `ready` items with `execution_state=uncertain`

Retry must reuse the same saved scope and the same saved payload.
Retry may re-list the current remote row set only when `plan_direction=push` and the planned action requires cleanup or safety checks.
Retry must reuse the same concurrency rules as `plan apply`, including single-plan execution, saved-plan-item concurrency units, fixed-limit remote I/O, and resolved-target-issue serialization.

Uncertain push retries must first perform best-effort target-adapter reconciliation where supported.

If prior remote creation cannot be ruled out, the push item remains `uncertain` and must not be recreated automatically.

No force-recreate path exists.

### Crash safety

Before local merge or remote delete/create:

1. reserve the execution locally
2. persist an attempt row with `pending`

After execution:

3. mark the attempt `succeeded` and persist the resulting execution metadata when success is confirmed
4. mark the attempt `failed` when failure is confirmed
5. mark the attempt `uncertain` when the outcome is ambiguous

A `pending` attempt older than 15 minutes is treated as effective `uncertain`.

Historical attempt rows are not rewritten solely because they became stale.

### Concurrency and performance

Use goroutines only for remote adapter I/O.

Rules:

* build tasks from saved `ready` items whose execution state is eligible
* run remote HTTP work with `errgroup.Group` when `plan_direction=push`
* use bounded concurrency with one fixed tool-defined global `SetLimit()` across the whole plan execution
* concurrency units are saved plan items, not sub-steps within one plan item
* before any remote HTTP execution begins, sort eligible plan items by resolved target issue key
* target-issue isolation applies before fixed-limit scheduling so only one goroutine may actively execute remote I/O for a given resolved target issue key at a time
* the same fixed tool-defined global concurrency limit also applies to planning-time remote HTTP work
* treat per-task failures as result data, not fatal group errors
* unexpected per-item infrastructure failures during remote I/O must be recorded on those items while other independent items continue executing
* keep SQLite writes serialized through one writer path
* sort and render deterministically after execution finishes

Within a single apply execution:

* cache repeated issue capability checks and similar advisory validations where practical for push flows
* handle `429` and transient transport failures with narrow retry behavior inside the HTTP client only when safe


## Jira Cloud Adapter

Jira Cloud is a deferred optional adapter for `workledger`.

Shared pull rules live in [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules).
Shared reconcile planning and apply rules live in [features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review](features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review) and [features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry](features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry).

Its responsibilities are:

* authenticate against configured Jira Cloud instances
* participate in shared reconcile pull planning for remote worklogs that will later be applied into the canonical local ledger
* participate in shared reconcile delivery to Jira Cloud targets when the operator runs a push flow
* decorate local issue metadata such as per-issue max estimate through a dedicated adapter action

### Configuration

Jira Cloud config remains YAML-owned in `~/.config/workledger/config.yaml`.

Deferred shape:

```yaml
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token: my-jira-token
```

Rules:

* the entire `jira_cloud` section is optional
* each instance name must be unique within `jira_cloud.instances`
* `base_url`, `auth.email`, and `auth.token` are required for each configured instance
* credentials must stay local-only and must never be written back by the CLI
* `jira_cloud.instances.<name>.routing`, when present, owns route profiles for delivery into that specific target instance
* `jira_cloud.instances.<name>.routing.profiles.<profile>.issue_prefixes` is an optional list of canonical source prefixes owned by that target instance in that profile
* `jira_cloud.instances.<name>.routing.profiles.<profile>.reporting_targets` is an optional mapping of canonical source prefixes to fixed reporting issues owned by that target instance in that profile
* one Jira Cloud route profile may not mix `issue_prefixes` and `reporting_targets`
* the same prefix may not be owned by more than one Jira Cloud instance in the same route profile

### Command surface in this slice

Pull uses `workledger plan reconcile --pull --adapter=jira-cloud` followed by `workledger plan apply`.
Push uses `workledger plan reconcile --push --adapter=jira-cloud`, `workledger plan show`, `workledger plan apply`, and `workledger plan retry`.
Jira Cloud routing resolves target adapter instances from the selected family's instance-local route profiles where configured.
Shared adapter status also covers bare `workledger status` and filtered `workledger status --adapter=jira-cloud`.

Shared status rules:

* bare `workledger status` inspects only configured adapter families and every configured instance owned by each family
* `workledger status --adapter=<family>` supports `clockify`, `jira-cloud`, and `jira-data-center`
* when the selected family has zero configured targets, or when no families are configured, the command succeeds with an empty result set
* bare `workledger status` keeps collecting and rendering rows even when one adapter or instance fails; successful rows report `status="OK"`, failed rows report the adapter error message, and the command returns the first deterministic non-zero exit code after rendering
* JSON output uses one normalized payload shape as `{"items":[...]}` for both bare and filtered status
* each `items[]` entry includes `adapter`, `instance`, `status`, `base_url`, `workspace_id`, `user_id`, and `user`; fields that do not apply to that adapter are `null`
* table output uses the shared headers `ADAPTER`, `INSTANCE`, `STATUS`, `BASE_URL`, and `USER`
* in table output, Clockify renders `workspace_id` in `INSTANCE` and `user_id` in `USER`
* rows are ordered by family as `clockify`, `jira-cloud`, then `jira-data-center`; Jira instance rows are sorted by instance name ascending

Issue-metadata refresh rules:

* the command must remain separate from reconcile planning and apply
* `--field=max-estimate` reads Jira issue original estimate and stores it as local `issue_metadata.max_estimate_seconds`
* selection reuses active local-worklog selectors and deduplicates remote reads by `issue_key`
* missing Jira original estimate clears the local cached value to `null`
* `--instance <name>` is required when more than one `jira_cloud` instance is configured


## Jira Data Center Adapter

Jira Data Center is a deferred optional adapter for `workledger`.

Shared pull rules live in [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules).
Shared reconcile planning and apply rules live in [features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review](features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review) and [features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry](features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry).

Its responsibilities are:

* authenticate against configured Jira Data Center instances
* participate in shared reconcile pull planning for remote worklogs that will later be applied into the canonical local ledger
* participate in shared reconcile delivery to Jira Data Center targets when the operator runs a push flow
* decorate local issue metadata such as per-issue max estimate through a dedicated adapter action

### Configuration

Jira Data Center config remains YAML-owned in `~/.config/workledger/config.yaml`.

Deferred shape:

```yaml
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token: my-dc-token
      routing:
        profiles:
          default:
            issue_prefixes:
              - ITO
          aciu_reporting:
            reporting_targets:
              APPS: ACIU-123
```

Rules:

* the entire `jira_data_center` section is optional
* each instance name must be unique within `jira_data_center.instances`
* `base_url` is required for each configured instance
* auth uses `bearer.token` only in this slice
* credentials must stay local-only and must never be written back by the CLI
* `jira_data_center.instances.<name>.routing`, when present, owns route profiles for delivery into that specific target instance
* `jira_data_center.instances.<name>.routing.profiles.<profile>.issue_prefixes` is an optional list of canonical source prefixes owned by that target instance in that profile
* `jira_data_center.instances.<name>.routing.profiles.<profile>.reporting_targets` is an optional mapping of canonical source prefixes to fixed reporting issues owned by that target instance in that profile
* `jira_data_center.instances.<name>.pull.exclude_issues` is optional and lists extra issue keys that pull must never import into canonical local storage
* Jira Data Center reporting target issues are implicitly excluded from pull on the owning instance and do not need to be repeated in `pull.exclude_issues`
* one Jira Data Center route profile may not mix `issue_prefixes` and `reporting_targets`
* the same prefix may not be owned by more than one Jira Data Center instance in the same route profile
* pull, issue-metadata refresh, and totals require `--instance <name>` when more than one Jira Data Center instance is configured

### Command surface

* `workledger status --adapter=jira-data-center`
* `workledger totals --adapter=jira-data-center`
* `workledger issue-metadata refresh --adapter=jira-data-center --field=max-estimate`

Pull uses `workledger plan reconcile --pull --adapter=jira-data-center` followed by `workledger plan apply`.
Push uses `workledger plan reconcile --push --adapter=jira-data-center`, `workledger plan show`, `workledger plan apply`, and `workledger plan retry`.
Jira Data Center routing resolves target adapter instances from the selected family's instance-local route profiles where configured.

Issue-metadata refresh rules:

* the command must remain separate from reconcile planning and apply
* `--field=max-estimate` reads Jira issue original estimate and stores it as local `issue_metadata.max_estimate_seconds`
* selection reuses active local-worklog selectors and deduplicates remote reads by `issue_key`
* missing Jira original estimate clears the local cached value to `null`
* when Jira Data Center participates in shared status, the command inspects every configured Jira Data Center instance and emits one shared status row or `items[]` entry per instance
* Jira Data Center pull imports only worklogs authored by the authenticated Jira user
* Jira Data Center push compares and mutates only worklogs authored by the authenticated Jira user
* reporting push may rewrite the remote Jira worklog comment to `<SOURCE_ISSUE_KEY> | <description>` and must avoid duplicate prefixes
* apply must never re-run routing; it uses the saved target adapter instance and target issue from the saved plan item


## Clockify Adapter Overview

Clockify is a deferred optional adapter for `workledger`.

Its responsibilities are:

* authenticate to one Clockify workspace selected in local config
* read remote worklog entries for the current operator
* participate in shared reconcile pull planning that normalizes those entries into saved pull-plan payloads
* participate in shared reconcile push delivery to the configured Clockify target when the operator runs a push flow

Clockify does not own or override canonical data.

Shared pull rules live in [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules).

The shared local-ledger and reconcile behavior is defined in:

* [architecture.md#configuration-and-state](architecture.md#configuration-and-state)
* [architecture.md#storage-and-architecture](architecture.md#storage-and-architecture)
* [features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review](features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review)
* [features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry](features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry)


## Clockify Commands and Config

This document owns the deferred Clockify-specific command surface, credentials, and target-selection behavior.

### Command surface

Clockify adds:

* `workledger status --adapter=clockify`
* `workledger totals --adapter=clockify`

These commands are optional extensions to the core CLI roadmap in [product.md#cli-roadmap](product.md#cli-roadmap).
Shared pull rules live in [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules).
Shared reconcile planning and apply rules live in [features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review](features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review) and [features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry](features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry).

### Configuration

Clockify config remains YAML-owned in `~/.config/workledger/config.yaml`.

Deferred shape:

```yaml
clockify:
  workspace_id: 0123456789abcdef
  user_id: 0123456789abcdef
  auth:
    api_key: my-clockify-api-key
  project_mapping:
    issue_prefixes:
      AAPP: Product
      ITO: Internal
    default_project: Inbox
    create_issue_tag_if_missing: true
```

Rules:

* the entire `clockify` section is optional
* `workspace_id`, `user_id`, and `auth.api_key` are required when `workledger status --adapter=clockify` is used, when `workledger totals --adapter=clockify` is used, or when a reconcile flow selects `--adapter=clockify`
* Clockify credentials must stay local-only
* `workledger init` may write the active `clockify` section only when bootstrapping a new config file from process env `CLOCKIFY_API_KEY`
* aside from that `workledger init` bootstrap path, Clockify credentials must never be written back by the CLI
* future config validation for this section must fail clearly on unknown keys or missing required values
* Clockify does not use Jira-style issue-key routing to choose its delivery target
* Clockify pull and push flows use the single configured Clockify target selected by the reconcile flow
* `project_mapping`, when present, configures Clockify project resolution inside that single configured target
* `project_mapping.issue_prefixes` maps canonical local issue-key prefixes such as `AAPP` to exact Clockify project names in the configured workspace
* `project_mapping.default_project`, when present, names the fallback Clockify project used when no prefix rule matches
* `project_mapping.create_issue_tag_if_missing` controls whether push planning may rely on creating an exact issue-key tag such as `AAPP-123` when Clockify does not already contain it
* Clockify project mapping belongs under the `clockify` section rather than the shared top-level `routing` section because it does not select among multiple Clockify adapter instances

### `workledger status --adapter=clockify`

`workledger status` verifies that the configured Clockify adapter can authenticate and inspect its configured scope when filtered with `--adapter=clockify`.

It must:

* validate the deferred Clockify config section before any remote call
* check API reachability and credential acceptance
* confirm the authenticated Clockify user `id` exactly matches configured `clockify.user_id`
* confirm the configured `clockify.workspace_id` is visible to that authenticated user through `activeWorkspace` or `defaultWorkspace`
* confirm the configured workspace and user are readable
* remain read-only
* render deterministic output in `table` or `json`

### `workledger totals`, `workledger totals --adapter=clockify`, `workledger totals --adapter=jira-cloud`, and `workledger totals --adapter=jira-data-center`

Bare `workledger totals` compares canonical local booked time against every configured adapter target for one selected date window.
When filtered with `--adapter=clockify`, `--adapter=jira-cloud`, or `--adapter=jira-data-center`, it compares canonical local booked time against remote adapter booked time for that same selected window.

It must:

* require exactly one selected date window supplied either by `--from` plus `--to` or by exactly one date-window shortcut selector
* support `--progress=auto|bar|plain|off` on bare totals and every explicit adapter totals form
* read local totals from active canonical SQLite worklogs only
* exclude deleted tombstones from local totals
* bare `workledger totals` inspects only configured adapter families and every configured instance owned by each family
* bare `workledger totals` orders rows by family as `clockify`, `jira-cloud`, then `jira-data-center`; Jira instance rows are sorted by instance name ascending
* bare `workledger totals` contributes at most one Clockify row, one Jira Cloud row per configured instance, and one Jira Data Center row per configured instance
* bare `workledger totals` validates and executes each adapter target independently
* bare `workledger totals` may fetch distinct adapter targets concurrently but must preserve the existing final row order
* bare `workledger totals` renders every successful and failed target row before exiting
* bare `workledger totals` returns an empty `items` array and exit code `0` when no adapters are configured
* when an adapter is selected, validate that adapter config section before any remote call
* Clockify totals read remote totals from entries visible to the configured `clockify.workspace_id` and `clockify.user_id` scope only
* Jira Cloud totals require one resolved `jira_cloud` instance and require `--instance <name>` when more than one instance is configured
* Jira Cloud totals derive managed scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* Jira Cloud totals ignore `reporting_targets` when deriving totals scope
* Jira Cloud totals exclude exact issue keys from both local and remote totals when those keys appear in `pull.exclude_issues` or as configured `reporting_targets` target issues on the selected instance
* Jira Cloud totals fail validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* Jira Cloud totals filter local totals to worklogs whose issue keys match that managed routed issue-prefix scope only
* Jira Cloud totals discover candidate issues with `worklogAuthor = currentUser() AND worklogDate >= "<from>" AND worklogDate <= "<to>"`
* Jira Cloud totals may fetch per-issue worklogs concurrently after issue discovery but must keep final aggregation deterministic
* Jira Cloud totals read remote totals only from worklogs authored by the authenticated Jira Cloud user in the resolved instance whose issue keys match that same managed routed issue-prefix scope
* Jira Data Center totals require one resolved `jira_data_center` instance and require `--instance <name>` when more than one instance is configured
* Jira Data Center totals derive managed scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* Jira Data Center totals ignore `reporting_targets` when deriving totals scope
* Jira Data Center totals exclude exact issue keys from both local and remote totals when those keys appear in `pull.exclude_issues` or as configured `reporting_targets` target issues on the selected instance
* Jira Data Center totals fail validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* Jira Data Center totals filter local totals to worklogs whose issue keys match that managed routed issue-prefix scope only
* Jira Data Center totals discover candidate issues with `worklogAuthor = currentUser() AND worklogDate >= "<from>" AND worklogDate <= "<to>"`
* Jira Data Center totals may fetch per-issue worklogs concurrently after issue discovery but must keep final aggregation deterministic
* Jira Data Center totals read remote totals only from worklogs authored by the authenticated Jira Data Center user in the resolved instance whose issue keys match that same managed routed issue-prefix scope
* compare totals by overlap with the effective selected window rather than by start timestamp alone
* split both local and remote overlapping intervals at local day boundaries in the effective selection timezone before per-day aggregation
* compute aggregate totals and one per-day comparison row from the same sliced interval set
* report `match` only when aggregate totals match exactly and every returned day matches exactly
* report `mismatch` when any aggregate or per-day delta is non-zero
* report `indeterminate` only when a running Clockify entry overlaps the selected effective window
* explicit `--adapter` returns exit code `0` for `match`, `mismatch`, and `indeterminate`
* bare `workledger totals` returns the first deterministic non-zero per-target exit code after rendering all rows
* use exit code `2` for validation or config errors, `3` for surfaced Jira-family `404`, `4` for auth failure, and `5` for external or connectivity failure
* remain read-only
* render deterministic output in `table` or `json`
* write all live progress output to stderr only and never change stdout payload shape
* explicit `--adapter` keeps the existing single-result `table` and `json` shape unchanged
* `--details` expands explicit single-result table output to include per-day rows and does not change JSON output

Bare JSON output must:

* render `{"items":[...]}` where each `items[]` entry includes `adapter`, `instance`, `from`, `to`, and `timezone`
* use `instance=null` for Clockify and `instance=<name>` for Jira families
* include `summary` and `days` for successful targets
* include `status` and `message` for failed targets
* keep adapter scopes separate and must not merge disparate adapter totals into one aggregate result

Table output must:

* render columns `DATE`, `LOCAL`, `REMOTE`, `DELTA`, and `STATE`
* render only one aggregate `TOTAL` row by default
* render one row per returned local day in ascending order before `TOTAL` when `--details` is present
* append one final `TOTAL` row with aggregate local, remote, and delta values plus overall state in both modes
* append one final summary line naming the adapter, effective selected range, effective timezone, and overall state
* append `instance=<name>` in that summary line for Jira Cloud and Jira Data Center totals
* bare `workledger totals` instead renders one row per adapter target with adapter, instance, effective window metadata, comparable summary totals, and deterministic failure text when a target fails
* when a bare `workledger totals` target fails after local scope is resolved, its row still renders `LOCAL` from the scoped local aggregate
* when a bare `workledger totals` target fails, `REMOTE` and `DELTA` stay empty unless a full comparison completed

### Reconcile flow

Clockify pull follows the shared adapter pull contract through:

* `workledger plan reconcile --pull --adapter=clockify`
* `workledger plan apply`

Clockify push follows the shared reconcile delivery contract through:

* `workledger plan reconcile --push --adapter=clockify`
* `workledger plan show`
* `workledger plan apply`
* `workledger plan retry`

Rules:

* pull reads Clockify worklogs for the configured operator and workspace only
* pull uses only the configured Clockify workspace and user scope
* pull does not infer source scope from issue-key routing
* pull must resolve Clockify time-entry `tagIds` to tag names using the configured workspace tags API before applying issue-key tag validation
* pull treats entries without exactly one resolved exact issue-key tag such as `AAPP-123` as non-importable findings rather than actionable pull rows
* push targets the single configured Clockify target without resolving a target adapter instance from issue key alone
* push may resolve a Clockify project inside that single configured target from the local issue-key prefix using `clockify.project_mapping.issue_prefixes`
* push uses `clockify.project_mapping.default_project` when no prefix rule matches and a default project is configured
* push must fail that plan item as `blocked` when no prefix rule matches and no default project is configured
* push must resolve Clockify projects by exact project name within the configured workspace
* push must fail that plan item as `blocked` when the resolved Clockify project name does not exist or resolves ambiguously in the configured workspace
* push must ensure an exact issue-key tag such as `AAPP-123` exists for the local worklog before creating the remote Clockify time entry
* when `clockify.project_mapping.create_issue_tag_if_missing=true`, push may create the missing exact issue-key tag in the configured workspace and then attach it to the created Clockify time entry
* when `clockify.project_mapping.create_issue_tag_if_missing=false`, push must fail that plan item as `blocked` when the exact issue-key tag is missing
* Clockify project mapping must not require one config rule per full issue key

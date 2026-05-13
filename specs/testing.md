# Testing

This file owns acceptance criteria, validation expectations, and verification obligations for the docs contract.

## CLI and Acceptance

### Command Surface

The canonical MVP command surface is defined in [product.md#mvp-command-surface](product.md#mvp-command-surface).
No additional commands are part of the current MVP beyond the documented command surface.
The CLI implementation must use Cobra for root, group, and leaf command construction.
The root command should not expose Cobra's default `completion` command in MVP.
`workledger version` does not depend on config presence or validity.
Every MVP root, group, and leaf command must accept `-h` and `--help`, print help to stdout, and exit `0` without loading config or SQLite.

### Output and Logging Contract

* user-facing command output goes to stdout
* logs and diagnostics go to stderr
* `--output json` must remain valid JSON on stdout without mixed log lines
* table output also belongs on stdout

MVP commands are local-only.
Any future concurrency is deferred with remote adapter work.

For commands that use date-window selectors, `--from` and `--to` accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, and signed day offsets such as `-7d` or `+2d`.
The shortcut selectors `--current-week` and `--last-week` are also supported and use Monday-through-Sunday calendar weeks in the effective selection timezone.
The shortcut selectors `--current-month` and `--last-month` are also supported and use local calendar-month windows in the effective selection timezone.
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
* root help through bare `workledger`, `workledger -h`, `workledger --help`, and `workledger help`
* `-h` and `--help` on every MVP command
* version output with raw string table mode and fixed JSON object mode
* config path resolution
* starter YAML bootstrap
* SQLite path provisioning and empty schema creation
* strict MVP-only config validation with batch error reporting

Done when:

* bare `workledger` shows help
* `workledger -h` works
* `workledger worklogs add --help` works without config
* `workledger init` works
* `workledger version` works
* `workledger config validate` works

#### Phase 2: Local worklog CRUD

Deliver:

* canonical local worklog storage in SQLite
* default tombstone retention for deleted worklogs with explicit hard-delete bypass
* local worklog listing and inspection by stable local UUID
* local worklog search by literal case-insensitive description substring
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
* guarded tombstone restore through `workledger worklogs restore`
* duplicate and overlap detection with explicit `--force` bypass on write commands

Done when:

* `workledger worklogs list` works with an explicit time selector
* `workledger worklogs search <query>` works without a required time selector
* `workledger worklogs show <id>` works
* `workledger worklogs add` works
* `workledger worklogs update <id>` works
* `workledger worklogs delete <id>` works
* `workledger worklogs restore` works

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

### Search-specific verification

For `workledger worklogs search <query>`, tests must cover at minimum:

* case-insensitive active matching on canonical stored descriptions
* default cross-date search with no time selector
* issue and date-window narrowing
* blank-query validation with exit code `2`
* zero-match success with exit code `0`
* literal `%` and `_` handling
* deleted-only matching through `--only-deleted`
* deterministic `started_at desc`, then `id` ordering
* list-style table headers and totals footer reuse
* JSON `filters`, `items`, and `total`, including `filters.raw.query` and `filters.effective.query`


## CLI Roadmap

This document captures deferred CLI phases beyond the current MVP.
The MVP remains a CLI tool, but post-MVP planning should leave room for an additional TUI surface built with Bubble Tea and Lip Gloss on top of the same core logic.

MVP phases remain in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).

### Deferred command surface

Post-MVP commands add:

* `workledger status`
* `workledger totals`
* `workledger issue-metadata refresh`
* `workledger plan reconcile`
* `workledger plan show`
* `workledger plan apply`
* `workledger plan list`
* `workledger plan retry`
* `workledger tui`

Rules:

* bare `workledger` continues to show help
* future TUI work lives behind the explicit `workledger tui` command
* the TUI must call the same reusable services as the CLI rather than shelling out to CLI commands

Status uses one shared command:

* `workledger status` shows all configured adapters
* `workledger status --adapter=jira-cloud` limits status to Jira Cloud
* `workledger status --adapter=jira-data-center` limits status to Jira Data Center
* `workledger status --adapter=clockify` limits status to Clockify

Status rules:

* `workledger status` inspects every configured adapter family and configured adapter instance owned by that family
* `--adapter=<family>` limits inspection to one adapter family
* when `--adapter` names an adapter family with zero configured targets, the command succeeds and returns an empty result set
* when no adapter families are configured, the command succeeds and returns an empty result set
* bare `workledger status` renders every configured row even when one adapter or instance fails; successful rows report `status="OK"`, failed rows report the adapter error message, and the command returns the first deterministic non-zero exit code after rendering
* `workledger status` remains read-only and must not mutate local worklogs or local issue metadata

Totals uses one shared command:

* `workledger totals --adapter=clockify`
* `workledger totals --adapter=jira-cloud`
* `workledger totals --adapter=jira-data-center`

Totals rules:

* `workledger totals` remains read-only and must not mutate local worklogs, local issue metadata, saved plans, or remote adapter state
* `workledger totals` supports `--progress=auto|bar|plain|off` for bare and explicit adapter forms
* bare `workledger totals` inspects every configured adapter family and configured adapter instance owned by that family
* bare `workledger totals` may fetch adapter targets concurrently while preserving rendered row order and first deterministic non-zero exit code
* bare `workledger totals` renders one row per configured adapter target, continues rendering when one target fails, and exits with the first deterministic non-zero per-target exit code after rendering
* bare `workledger totals` returns an empty result set and exit code `0` when no adapters are configured
* `workledger totals --adapter=<family>` supports `clockify`, `jira-cloud`, and `jira-data-center`
* `--adapter=clockify` uses the configured Clockify workspace plus user scope
* `--adapter=jira-cloud` compares against one Jira Cloud instance and requires `--instance <name>` when more than one `jira_cloud` instance is configured
* `--adapter=jira-cloud` derives totals scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* `--adapter=jira-cloud` ignores `reporting_targets` when deriving totals scope
* `--adapter=jira-cloud` excludes exact issue keys from both local and remote totals when those keys appear in `pull.exclude_issues` or as configured `reporting_targets` target issues on the selected instance
* `--adapter=jira-cloud` fails validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* `--adapter=jira-data-center` compares against one Jira Data Center instance and requires `--instance <name>` when more than one `jira_data_center` instance is configured
* `--adapter=jira-data-center` derives totals scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* `--adapter=jira-data-center` ignores `reporting_targets` when deriving totals scope
* `--adapter=jira-data-center` excludes exact issue keys from both local and remote totals when those keys appear in `pull.exclude_issues` or as configured `reporting_targets` target issues on the selected instance
* `--adapter=jira-data-center` fails validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* selection reuses the shared date-window selectors: `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`
* the command compares canonical local booked time against remote adapter booked time inside one effective selected date window
* Clockify totals compare all active canonical local worklogs in scope against all Clockify entries visible in the configured `workspace_id` plus `user_id` scope for the same effective window
* Jira Cloud totals compare only the routed local issue-prefix subset owned by the selected instance against only authenticated-user Jira Cloud worklogs on that same routed issue-prefix subset for the same effective window
* Jira Data Center totals compare only the routed local issue-prefix subset owned by the selected instance against only authenticated-user Jira Data Center worklogs on that same routed issue-prefix subset for the same effective window
* deleted tombstones are excluded from local totals
* explicit `--adapter` keeps the single-result output contract; bare `workledger totals` uses `{"items":[...]}` in JSON and one table row per target in table output
* default table stdout reports only aggregate comparison results while JSON stdout includes aggregate plus per-day comparison results
* `--details` adds per-day comparison rows to table stdout without changing JSON output
* progress output stays on stderr only and must not corrupt JSON stdout

Issue metadata uses one shared command:

* `workledger issue-metadata refresh --adapter=jira-cloud --field=max-estimate`
* `workledger issue-metadata refresh --adapter=jira-data-center --field=max-estimate`
* Jira Cloud metadata refresh requires `--instance` when more than one Jira Cloud instance is configured
* Jira Data Center metadata refresh requires `--instance` when more than one Jira Data Center instance is configured

Issue-metadata refresh rules:

* the command decorates local issue metadata only and must not create, merge, update, or delete canonical local worklogs
* `--adapter=<family>` is required and currently supports Jira adapter families only
* `--field=max-estimate` is required in the first implementation
* selection reuses the active-worklog selectors from `workledger worklogs list`: `--issue`, `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`
* the command selects active local worklogs, extracts distinct issue keys, fetches per-issue metadata from the selected adapter family, and upserts local metadata rows
* when Jira exposes no original estimate for an issue, the command persists `max_estimate_seconds=null`
* stdout reports deterministic per-issue outcomes in `table` or `json`

Remote sync uses one shared flow:

* `workledger plan reconcile --pull --adapter=<family>`
* `workledger plan reconcile --push --adapter=<family>`
* `workledger plan reconcile --push --adapter=<family> --route-profile=<name>`
* Jira Data Center pull requires `--instance` when more than one Jira Data Center instance is configured
* `workledger plan apply`

Adapter-specific details plus shared pull and delivery behavior live under [features/004-adapters-and-reconcile/spec.md](features/004-adapters-and-reconcile/spec.md).

### Phase 4: Import and local ledger

Deliver:

* adapter worklog pull planning into SQLite
* allocation-based merge into canonical local worklogs
* local issue-metadata refresh from adapters into SQLite
* read-only issue-metadata inspection from local SQLite via `issue-metadata list`
* Jira Data Center saved plans persist resolved target instance, target issue, source issue keys, and foreign-author detection in the inspection summary
* deterministic pull-plan output

Done when:

* operators can create a pull plan from any configured adapter with `workledger plan reconcile --pull --adapter=<family>`
* operators can apply that saved pull plan into local SQL with `workledger plan apply`
* operators can refresh Jira-backed issue metadata with `workledger issue-metadata refresh --adapter=<family> --field=max-estimate`
* operators can compare local canonical booked time against Clockify, Jira Cloud, or Jira Data Center remote booked time with `workledger totals --adapter=<family>`
* the same allocation imported from multiple adapters can resolve into one local record within the same issue/window scope
* imports preserve local canonical edits and do not overwrite or recreate local fields automatically

Verification includes:

* exact aggregate and per-day match across a selected multi-day range
* aggregate mismatch with one-day delta
* aggregate equality with per-day mismatch
* local and remote cross-midnight entries sliced correctly into selected local days
* zero local and zero remote booked time returns `match`
* local-only or remote-only booked time returns `mismatch`
* an overlapping running Clockify entry returns `indeterminate` with exit code `0`
* invalid Clockify config returns `2`
* Clockify auth failure returns `4`
* Clockify connectivity or external failure returns `5`

### Additive effects on MVP worklog commands

The base `workledger worklogs` contract remains defined by MVP:

* [api.md#local-worklogs](api.md#local-worklogs)
* [api.md#json-contracts](api.md#json-contracts)

Issue metadata remains separate from canonical worklog row contracts.

Additive deferred effects:

* `issue-metadata list` exposes cached per-issue metadata such as `max_estimate_seconds`
* `worklogs context` may include additive read-only issue metadata in planning issue hints
* `worklogs context` must list a cross-midnight worklog row once on its local start day while still prorating occupied time into every selected local day touched by that interval
* `worklogs context` must reduce the later day's `free_slots` and update per-day `booked_seconds` and `collisions` from the carried-over occupied slice when a selected range spans a cross-midnight worklog
* `worklogs context` JSON must always include `planning.issues[*].issue_key` and `planning.issues[*].max_estimate_seconds`, and may include additive metadata fields without breaking the contract
* when a surfaced planning-issue metadata field is unavailable, `worklogs context` must render that field as `null` rather than omitting it selectively within the same scope
* write commands such as `add`, `update`, `shift`, `apply`, and `delete` must not create or mutate issue metadata
* issue-metadata refresh remains a separate adapter action and must not be coupled to pull reconcile or apply
* human-facing table output must render aligned columns rather than raw tab-delimited cells
* `worklogs list` table output must append a blank line followed by a totals footer with matched row count and summed booked duration in human-readable format
* in `worklogs list`, `description` is a list-view summary field and table output must truncate values longer than 80 characters with `...`

Shared pull authority and local-versus-remote preservation rules live in [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules).

### Phase 5: Planning engine

Deliver:

* direction-aware reconcile planning with `--pull` and `--push`
* local-worklog selection and scope grouping
* validation classification
* target-adapter diff and pull-merge planning rules
* issue and worklog capability checks
* saved plan snapshot creation with payload materialization
* scope-level remote cleanup planning for push flows
* deterministic plan output

Done when:

* `workledger plan reconcile` classifies selected reconcile scopes as `ready`, `invalid`, `blocked`, `skipped`, or `check_failed`
* a saved reconcile plan that contains one or more `check_failed` scopes still prints the normal saved-plan stdout payload, persists one saved plan, and exits `6`
* `workledger plan show` renders the saved reconciliation report without external adapter requests
* pull plans persist `plan_direction=pull` with `planned_action=merge|none`
* push plans persist `plan_direction=push` with `planned_action=create|replace|delete|none`
* planning can target any adapter that advertises import or delivery capability
* Jira-family push planning can use a named route profile to deliver many source issue keys into one reporting issue on another configured Jira target in the selected adapter family
* Jira-family reporting routes stay within the Jira adapter family named by `--adapter`

### Phase 6: Reconcile apply engine

Deliver:

* task building from saved `ready` plan items with `execution_state=not_attempted`
* execution reservation
* local merge execution for pull plans
* concurrent outbound execution for push plans
* attempt-state persistence
* idempotent rerun behavior based on local SQLite state
* merge, create, replace, and delete execution from saved `planned_action`
* structured per-task result output

Done when:

* `workledger plan apply` with an explicit plan ID can apply saved ready scopes from a pull or push plan
* `workledger plan apply` without an explicit plan ID uses the most recent saved plan
* applying a pull plan merges the saved normalized remote payload into local SQL without refetching remote worklogs
* rerunning does not duplicate remote worklogs when local delivery state already shows success
* current remote worklogs in a saved replace or delete scope are removed before replacement worklogs are created
* ambiguous outcomes are recorded as `uncertain`
* stale pending attempts become effective `uncertain`

### Phase 7: Retry and recovery

Deliver:

* retry failed items from a saved plan
* retry uncertain push items from a saved plan through explicit target-adapter reconciliation
* clear operator visibility into blocked conflict cases and uncertain states

Done when:

* operators can safely retry only the needed subset of a prior plan
* uncertain items are never blindly replayed

### Deferred acceptance slice

Reporting delivery is a push-planning variant, not a separate command family.

Reporting delivery rules:

* it is an additive mirror from canonical local worklogs into the reporting Jira target
* it does not delete or mutate source-system worklogs
* each canonical local worklog row is delivered as one reporting worklog row
* the reporting issue is assumed to be dedicated to `workledger`
* when multiple source issues resolve to the same reporting issue in the same selected window, they are saved and compared as one aggregated reporting plan item
* reporting routes may be pushed for arbitrary operator-selected date windows and are not restricted to monthly buckets
* reporting-loop prevention is configuration-based: reporting target issues must be excluded from pull into canonical local worklog storage
* overlapping saved reporting plans are allowed; when the selected reporting scope already matches exactly, the new plan must contain no actionable reporting items
* reporting reconcile must not create a saved plan when all selected scopes are already in sync

Example operator flow:

* `workledger plan reconcile --push --adapter=jira-cloud --route-profile=reporting_b --from 2026-05-01 --to 2026-05-31`
* `workledger plan show`
* `workledger plan apply`

The deferred roadmap is complete when an operator can:

* create a pull plan with `workledger plan reconcile --pull --adapter=<family>`
* apply that pull plan into local SQL with `workledger plan apply`
* inspect local worklogs from the CLI
* inspect deleted local worklog tombstones from the CLI when explicitly requested
* inspect one local worklog with full detail from the CLI
* update an imported or manual local worklog from the CLI
* delete one or more local worklogs through the CLI with guarded batch behavior
* run `workledger plan reconcile --push --adapter=<family>` on a set of local worklogs
* review the saved reconciliation result with `plan show`
* push `ready` scopes from a saved plan using `plan apply` with an explicit plan ID
* rerun the same saved plan without duplicate target worklog creation when local delivery state already shows success
* retry failed items explicitly from a saved plan
* refuse blind replay of uncertain items unless reconciliation can prove safety


## Progress Reporting

This document defines deferred progress-reporting requirements for long-running commands that perform remote batch HTTP work.

The current MVP is local-only and does not require progress reporting.
Shared stdout, stderr, output-mode, and exit-code rules remain in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).

### Goals

Progress reporting exists to:

* show that a long-running remote command is still making forward progress
* give the operator a rough sense of remaining work without changing the command result contract
* keep machine-readable stdout stable

### Applicable commands

Deferred progress reporting may be used by commands that perform multiple remote HTTP operations, including:

* `workledger plan reconcile`
* `workledger plan apply`
* `workledger plan retry`
* future adapter-scoped health or import commands when they perform batch remote work

Single fast requests should not render progress UI.

### Output contract

Live progress output must go to stderr only.
It must never be mixed into stdout.

Rules:

* final command results still render only in the selected stdout output mode
* `--output json` must remain valid JSON on stdout with no progress fragments
* progress rendering must be best-effort presentation only and must not affect command success, failure, or exit code
* when stderr is not writable, the command still runs without progress UI

### CLI surface

Deferred remote commands should share one common flag:

* `--progress=auto|bar|plain|off`

Mode rules:

* `auto` is the default
* `auto` renders an animated progress bar only when stderr is a TTY; otherwise it stays silent
* `bar` forces interactive bar rendering and falls back to `plain` when stderr is not a TTY
* `plain` emits line-oriented progress summaries to stderr without cursor control
* `off` disables live progress output

This keeps one stable operator-facing control across planning, apply, and retry flows.

### Progress model

Progress should track logical work units, not raw transport attempts.

Rules:

* retries caused by `429` or transient transport failures must not create fake forward progress
* when a command has a stable work-set before execution starts, progress totals must use that stable work-set
* when the exact remote request count is unknown up front because of pagination, replace cleanup, or safety re-checks, progress may render without a percentage bar and should instead show phase plus completed-unit counts

Recommended logical units by command:

* `plan apply` and `plan retry`: saved plan items
* `plan reconcile --push`: resolved reconcile scopes or target-instance fetch tasks
* `plan reconcile --pull`: remote page or source-scope fetch tasks when page count is not known in advance

Operator-visible progress should prefer stable scope counts over volatile HTTP-call counts.

### Rendering requirements

Interactive bar mode should show at minimum:

* current phase
* completed logical units
* total logical units when known
* failed logical units so far
* elapsed time

Plain mode should emit:

* one start line
* periodic summary lines only when counts change materially or a time threshold is crossed
* one final completion line with succeeded, failed, and uncertain counts when relevant

Progress text should use deterministic wording so operators can understand logs quickly.

### Phase guidance

Commands may expose coarse phases such as:

* `discovering`
* `fetching`
* `applying`
* `finalizing`

One logical unit may internally perform more than one HTTP request.
For example, a reporting `replace` apply item may re-list remote rows, delete them, and then create replacement rows.
The progress model should still advance by saved plan item completion rather than by each sub-request.

### Failure handling

Progress UI must tolerate mixed outcomes.

Rules:

* per-unit failures increment failure counts without stopping unrelated eligible work
* a command that ends in partial success still renders its normal final stdout payload and exit code `6` where already defined
* Jira Cloud push reconcile covers partial remote-read failures where one resolved scope or instance becomes `check_failed` while other scopes may still classify normally
* interrupted or crashed commands do not need resumable progress state; persisted attempt history remains the source of truth

### Non-goals

This feature does not:

* change concurrency rules
* expose per-request debug logs in the progress UI
* guarantee an exact remaining-time estimate
* replace normal structured logs or diagnostics on stderr

### Detailed proposal

1. [Progress bars for `plan reconcile` and `plan apply`](ux.md#progress-bars-for-plan-reconcile-and-plan-apply)

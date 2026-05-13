# Product

This file owns product scope, roadmap, and feature boundaries for `workledger`.

## MVP Docs Index

This folder defines the current MVP scope for `workledger`.

### Read order for agents

1. [Overview and goals](constitution.md#overview-and-goals)
2. [Configuration and storage](api.md#configuration-and-storage)
3. [Local worklogs](api.md#local-worklogs)
4. [CLI and acceptance](api.md#cli-and-acceptance)
5. [JSON contracts](api.md#json-contracts)
6. [SQLite schema](data-model.md#sqlite-schema)
7. [Agent workflows](ux.md#agent-workflows)
8. [Roadmap adapters](product.md#roadmap-adapters)

### MVP command surface

The current MVP includes only:

* `workledger init`
* `workledger config validate`
* `workledger version`
* `workledger worklogs list`
* `workledger worklogs search <query>`
* `workledger worklogs show <id>`
* `workledger worklogs add`
* `workledger worklogs update <id>`
* `workledger worklogs delete <id>`
* `workledger worklogs restore`

The CLI implementation for this MVP command surface must use Cobra.
`workledger worklogs list` requires an explicit time selector and also supports field selection through `--fields` in MVP.
`workledger worklogs search <query>` searches canonical local descriptions without requiring a time selector and reuses list-style selectors, fields, output modes, and totals in MVP.
`workledger worklogs delete` also supports a guarded batch-delete mode and explicit `--hard` delete bypass in MVP.
`workledger worklogs restore` restores tombstones back into active local worklogs through guarded selector-based batch execution in MVP.
Bare `workledger` and the root help forms `workledger -h`, `workledger --help`, and `workledger help` are part of the MVP root-command behavior.
Every MVP command in this surface must also accept `-h` and `--help`.

### Implemented beyond MVP

The current codebase also implements:

* `workledger worklogs context`
* `workledger worklogs shift`
* `workledger worklogs apply`
* `workledger status`
* `workledger totals`
* `workledger issue-metadata list`
* `workledger issue-metadata refresh`
* `workledger plan reconcile`
* `workledger plan show`
* `workledger plan list`
* `workledger plan apply`
* `workledger plan retry`

### Known deferred areas

The current codebase still does not include:

* delivery correlation
* `workledger tui`
* tombstone-specific top-level commands


## Roadmap Adapters

This file is a short bridge from the MVP contract to deferred adapter and platform work.

### Deferred beyond MVP

Deferred areas still include:

* delivery correlation
* `workledger tui`

### Where deferred behavior lives

Authoritative deferred behavior lives outside this folder:

* [Platform docs](architecture.md)
* [Local worklog context](features/002-local-worklog-context/spec.md)
* [Local worklog batch planning](features/003-local-worklog-batch-planning/spec.md)
* [Adapter docs](features/004-adapters-and-reconcile/spec.md)

MVP readers do not need those docs to implement current MVP behavior.


## CLI Roadmap

This document captures deferred CLI phases beyond the current MVP.
The MVP remains a CLI tool, but post-MVP planning should leave room for an additional TUI surface built with Bubble Tea and Lip Gloss on top of the same core logic.

MVP phases remain in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).

### Implemented post-MVP command surface

The current codebase already adds:

* `workledger status`
* `workledger totals`
* `workledger issue-metadata list`
* `workledger issue-metadata refresh`
* `workledger plan reconcile`
* `workledger plan show`
* `workledger plan apply`
* `workledger plan list`
* `workledger plan retry`

### Deferred command surface

Still deferred:

* `workledger tui`

Rules:

* bare `workledger` continues to show help
* future TUI work lives behind the explicit `workledger tui` command
* the TUI must call the same reusable services as the CLI rather than shelling out to CLI commands

Status uses one shared command:

* `workledger status` shows all configured adapters
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
* bare `workledger totals` inspects every configured adapter family and configured adapter instance owned by that family
* bare `workledger totals` renders one row per configured adapter target, continues rendering when one target fails, and exits with the first deterministic non-zero per-target exit code after rendering
* bare `workledger totals` returns an empty result set and exit code `0` when no adapters are configured
* `workledger totals --adapter=<family>` supports `clockify`, `jira-cloud`, and `jira-data-center`
* `--adapter=clockify` uses the configured Clockify workspace plus user scope
* `--adapter=jira-cloud` compares against one Jira Cloud instance and requires `--instance <name>` when more than one `jira_cloud` instance is configured
* `--adapter=jira-cloud` derives managed scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* `--adapter=jira-cloud` ignores `reporting_targets` when deriving totals scope
* `--adapter=jira-cloud` fails validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* `--adapter=jira-data-center` compares against one Jira Data Center instance and requires `--instance <name>` when more than one `jira_data_center` instance is configured
* `--adapter=jira-data-center` derives managed scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* `--adapter=jira-data-center` ignores `reporting_targets` when deriving totals scope
* `--adapter=jira-data-center` fails validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* selection reuses the shared date-window selectors: `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`
* the command compares canonical local booked time against remote adapter booked time inside one effective selected date window
* Clockify totals compare all active canonical local worklogs in scope against all Clockify entries visible in the configured `workspace_id` plus `user_id` scope for the same effective window
* Jira Cloud totals compare only the routed local issue-prefix subset owned by the selected instance against only authenticated-user Jira Cloud worklogs on that same routed issue-prefix subset for the same effective window
* Jira Data Center totals compare only the routed local issue-prefix subset owned by the selected instance against only authenticated-user Jira Data Center worklogs on that same routed issue-prefix subset for the same effective window
* deleted tombstones are excluded from local totals
* explicit `--adapter` keeps the single-result output contract; bare `workledger totals` uses `{"items":[...]}` in JSON and one table row per target in table output
* default table stdout reports only aggregate comparison results while explicit JSON stdout includes aggregate plus per-day comparison results
* `--details` adds per-day comparison rows to table stdout without changing JSON output

Issue metadata uses one shared command family:

* `workledger issue-metadata list`
* `workledger issue-metadata refresh --adapter=jira-cloud --field=max-estimate`
* `workledger issue-metadata refresh --adapter=jira-data-center --field=max-estimate`

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
* `workledger plan apply`

Adapter-specific details plus shared pull and delivery behavior live under [features/004-adapters-and-reconcile/spec.md](features/004-adapters-and-reconcile/spec.md).

### Phase 4: Import and local ledger

Deliver:

* adapter worklog pull planning into SQLite
* allocation-based merge into canonical local worklogs
* local issue-metadata refresh from adapters into SQLite
* read-only issue-metadata inspection from local SQLite via `issue-metadata list`
* deterministic pull-plan output

Done when:

* operators can create a pull plan from any configured adapter with `workledger plan reconcile --pull --adapter=<family>`
* operators can apply that saved pull plan into local SQL with `workledger plan apply`
* operators can refresh Jira-backed issue metadata with `workledger issue-metadata refresh --adapter=<family> --field=max-estimate`
* operators can compare local canonical booked time against Clockify, Jira Cloud, or Jira Data Center remote booked time with `workledger totals --adapter=<family>`
* the same allocation imported from multiple adapters can resolve into one local record within the same issue/window scope
* imports preserve local canonical edits and do not overwrite or recreate local fields automatically

### Additive effects on MVP worklog commands

The base `workledger worklogs` contract remains defined by MVP:

* [api.md#local-worklogs](api.md#local-worklogs)
* [api.md#json-contracts](api.md#json-contracts)

Issue metadata remains separate from canonical worklog row contracts.

Additive deferred effects:

* `issue-metadata list` exposes cached per-issue metadata such as `max_estimate_seconds`
* `worklogs context` may include additive read-only issue metadata in planning issue hints
* write commands such as `add`, `update`, `shift`, `apply`, and `delete` must not create or mutate issue metadata
* issue-metadata refresh remains a separate adapter action and must not be coupled to pull reconcile or apply

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

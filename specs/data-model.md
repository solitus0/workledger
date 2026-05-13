# Data Model

This file owns canonical entities, persisted schema, indexes, and execution-state records.

## SQLite Schema

This file defines the authoritative MVP SQLite schema and storage rules for canonical local worklogs.
Deferred additive schema beyond the MVP local store is defined in [architecture.md#storage-and-architecture](architecture.md#storage-and-architecture).

### Scope

The MVP SQLite store persists only:

* active local worklogs
* tombstones for deleted local worklogs

Do not add MVP tables for saved plans, delivery attempts, audit events, issue metadata, or adapter runtime state.

### Naming and identity rules

Keep persisted names deterministic:

* table and column names use `snake_case`

Local worklog identity rules:

* `worklogs.id` is the canonical local worklog identity
* `worklog_tombstones.worklog_id` preserves the deleted local worklog identity
* the CLI must not expose or depend on SQLite row IDs

Canonical stored timestamps use UTC.

### Tables and columns

Required MVP tables:

* `worklogs`
* `worklog_tombstones`

Minimum columns:

* `worklogs`: `id`, `issue_key`, `started_at_utc`, `duration_seconds`, `description`, `created_at`, `updated_at`
* `worklog_tombstones`: `worklog_id`, `issue_key`, `started_at_utc`, `duration_seconds`, `description`, `deleted_at`

Local CRUD commands mutate only canonical local worklog fields:

* `issue_key`
* `started_at_utc`
* `duration_seconds`
* `description`

### Indexes

Key MVP indexes:

* unique index on `worklogs(id)`
* index on `worklogs(issue_key, started_at_utc)`
* index on `worklogs(started_at_utc)`
* unique index on `worklog_tombstones(worklog_id)`
* index on `worklog_tombstones(issue_key, started_at_utc)`
* index on `worklog_tombstones(deleted_at)`

### Tombstone rules

Default delete removes a worklog from the active `worklogs` set and writes a tombstone record.
Hard delete removes a worklog from the active `worklogs` set without writing a tombstone.

Each tombstone must retain enough data for:

* `worklogs list --only-deleted`
* future delete-only reconcile scopes
* pull protection for the same issue and reconcile-window allocation

Tombstone data must include:

* original local `worklog_id`
* `issue_key`
* original `started_at_utc`
* original `duration_seconds`
* original `description`
* deletion timestamp

Tombstones must not require or persist remote worklog identifiers as local identity.
Hard deletes produce no tombstone row and therefore no durable local delete intent.

### Bootstrap and repair rules

`workledger init` owns schema bootstrap.

Rules:

* create the SQLite file and initialize the full empty MVP schema when the database does not exist
* when the SQLite file exists but is missing MVP tables, repair it additively by creating the missing tables only
* when the existing SQLite file is incompatible or corrupt and cannot be repaired additively, fail clearly
* when the SQLite file already exists, leave existing compatible schema data unchanged

### Transaction boundaries

Use explicit SQLite write transactions for every worklog mutation so write boundaries stay deterministic.

Minimum atomic mutation scope:

* `worklogs add`: insert one active worklog
* `worklogs update`: validate then update one active worklog
* `worklogs delete <id>`: remove one active worklog and insert one tombstone, unless `--hard` is set
* filtered batch delete: remove all matched active worklogs and insert one tombstone per deleted row, unless `--hard` is set
* `worklogs restore`: insert the original active rows from matched tombstones and delete those tombstones

When validation fails, no partial writes are allowed.


## Storage and Architecture

This document captures deferred architecture needed beyond the MVP local CRUD baseline: adapter pull into canonical local state, saved plans, execution state, and adapter reconcile execution.

The current MVP already includes SQLite as the canonical local worklog store. Shared package and implementation constraints still follow the repository guidance in [../AGENTS.md](../AGENTS.md).
Future planning should preserve a path to a TUI built with Bubble Tea and Lip Gloss, so reconcile and worklog logic must stay independent from Cobra-specific command handlers and terminal rendering details.

### Naming rules

Keep persisted names deterministic:

* table and column names use `snake_case`
* persisted adapter family values use the YAML `snake_case` form such as `jira_cloud`, `jira_data_center`, and `clockify`
* CLI `--adapter` values such as `jira-cloud` must normalize to the persisted YAML form before store access
* `target_adapter_instance` stores the configured YAML target name; do not invent numeric target IDs

### Package layout

Recommended shape:

* `cmd/workledger` for process startup and dependency wiring only
* `internal/cli` for flag parsing helpers, rendering, confirmation, and exit code mapping
* `internal/config` for YAML loading, path resolution, normalization, and validation
* `internal/worklogs` for local CRUD rules, duplicate and overlap checks, tombstones, and selectors
* `internal/issues` for local issue-metadata refresh, joins, and issue-scoped advisory rules
* `internal/plans` for saved-plan creation, scope grouping, classification, review loading, apply, and retry orchestration
* `internal/adapter` for shared adapter capability contracts and family-specific planning or apply helpers
* `internal/adapter/jira_cloud` for Jira Cloud integration
* `internal/adapter/jira_data_center` for Jira Data Center integration
* `internal/adapter/clockify` for Clockify integration
* `internal/store/sqlite` for SQLite stores, migrations, and transactions
* `internal/tui` reserved for a future Bubble Tea and Lip Gloss frontend

Start with packages that match the documented command surface and state model.
Do not split pull, push, or routing into separate top-level packages until the implementation proves that `internal/plans`, `internal/worklogs`, or `internal/adapter` have become too large.
Keep application and domain logic reusable from both the MVP CLI and a future TUI surface.

Frontend boundary rules:

* `cmd/workledger` should open config, open SQLite, run migrations, build stores, build services, and hand those dependencies to the selected frontend
* the CLI frontend must be implemented with Cobra
* `internal/cli` owns Cobra command construction and terminal rendering, but not worklog, planning, or adapter business rules
* `internal/tui` must consume the same reusable services as `internal/cli`
* frontends must depend on services, not on raw SQLite stores
* frontends must not call other commands or parse rendered output from other commands

Preferred dependency direction:

* CLI frontend -> reusable services -> stores and adapters
* TUI frontend -> reusable services -> stores and adapters

### Store guidance

Avoid generic repository abstractions.

Prefer concrete stores such as:

* `WorklogStore`
* `WorklogTombstoneStore`
* `IssueMetadataStore`
* `SavedPlanStore`
* `SavedPlanItemStore`
* `DeliveryAttemptStore`
* `AuditEventStore`

Because adapter instances and adapter-scoped routes are YAML-owned, no SQLite store is needed for mutable adapter or route catalogs.

Keep `delivery_key` and `content_hash` on saved plan items.
Do not add a separate delivery-state table unless execution reads prove that deriving state from saved plan items plus append-only attempts is too expensive.

Define an explicit transaction runner in `internal/store/sqlite`.
Use it for worklog mutations, saved-plan creation, and per-item apply or retry execution so SQLite write boundaries stay deterministic.

### Core types to freeze

Freeze these contracts before implementation:

* `LocalWorklog`
* `LocalWorklogTombstone`
* `LocalIssueMetadata`
* `ImportedWorklog`
* `PlannedWorklog`
* `PushTask`
* `PushResult`
* `SavedPlanRecord`
* `SavedPlanItemRecord`
* `DeliveryAttemptRecord`
* `AuditEventRecord`
* adapter-target reference model
* adapter-routing model
* Jira issue-prefix mapping model
* Jira reporting-target mapping model
* validation status model
* execution-state derivation model from saved plan items plus attempt history
* plan-direction model using `pull` and `push`
* delivery identity model using `delivery_key` and `content_hash` on saved plan items
* planned-action model using `merge`, `create`, `replace`, `delete`, and `none`
* reconcile-scope identity model using target adapter family, target adapter instance, target issue, and saved reconcile window
* canonical merge model using issue key, started time, duration, and normalized description
* configuration schema and validation rules

Use stable identifiers and machine-readable reason codes instead of raw `error` values in user-facing result models.

### Configuration fingerprint

Each saved plan must retain a deterministic `config_fingerprint`.

Rules:

* compute the fingerprint from canonicalized effective config data, not raw YAML bytes
* formatting-only YAML changes must not invalidate a saved plan
* apply and retry must compare the current effective fingerprint with the saved fingerprint before execution

### Storage model

The MVP-local SQLite base remains defined in [data-model.md#sqlite-schema](data-model.md#sqlite-schema).
This section defines only the additive storage model needed beyond that MVP baseline.

Additive tables beyond MVP:

* `issue_metadata`
* `saved_plans`
* `saved_plan_items`
* `delivery_attempts`
* `audit_events`

Additive minimum columns beyond MVP:

* `issue_metadata`: `issue_key`, `max_estimate_seconds`, `source_adapter_family`, `source_adapter_instance`, `refreshed_at`
* `saved_plans`: `id`, `created_at`, `plan_direction`, `adapter_family`, `config_fingerprint`, `window_from_utc`, `window_to_utc`, `aggregate_status`, `applied_at`
* `saved_plan_items`: `id`, `plan_id`, `issue_key`, `target_issue`, `route_profile`, `plan_direction`, `target_adapter_family`, `target_adapter_instance`, `window_from_utc`, `window_to_utc`, `plan_status`, `planned_action`, `comparison_status`, `reason_code`, `reason_detail`, `payload_json`, `inspection_summary_json`, `delivery_key`, `content_hash`, `local_row_count`, `local_total_seconds`, `remote_row_count`, `remote_total_seconds`, `applied_state`, `applied_at`, `apply_message`
* `delivery_attempts`: `id`, `plan_id`, `plan_item_id`, `attempt_state`, `message`, `created_at`
* `audit_events`: `id`, `event_type`, `entity_type`, `entity_id`, `created_at`, `payload_json`

Store scope payload rows and inspection summaries on `saved_plan_items`.
Do not split them into extra child tables in the first implementation because planning, review, apply, and retry all consume them per scope.

Additive indexes beyond MVP:

* unique index on `issue_metadata(issue_key)`
* index on `issue_metadata(refreshed_at)`
* index on `saved_plans(created_at)`
* index on `saved_plan_items(plan_id)`
* index on `saved_plan_items(issue_key, window_from_utc, window_to_utc)`
* index on `saved_plan_items(target_adapter_family, target_adapter_instance, target_issue)`
* index on `saved_plan_items(delivery_key)`
* index on `delivery_attempts(plan_item_id, created_at)`
* index on `delivery_attempts(attempt_state, created_at)`
* index on `audit_events(created_at)`

Persist:

* local issue-level metadata cached from adapters for read-only local decoration
* saved plan metadata including the configuration fingerprint
* scope-level saved plan item snapshots and payloads
* append-only delivery attempts
* audit events

Do not persist:

* mutable adapter instance records
* mutable route records
* remote worklog identifiers as canonical local identity
* background job state
* distributed worker state
* a mutable `execution_state` column on saved plan items when current state can be derived from attempt history

### Adapter registry

Adapters remain YAML-configured rather than SQLite-configured.

Rules:

* resolve adapters by persisted adapter family plus configured target name
* use the configured YAML target name as the stable adapter-instance reference
* do not invent numeric target IDs for adapter instances

### CRUD-specific storage rules

MVP local CRUD schema, tombstone layout, and mutation boundaries remain defined in [data-model.md#sqlite-schema](data-model.md#sqlite-schema).
Deferred features must preserve that MVP local-store contract.

Rules:

* issue-level metadata such as Jira max estimate is stored separately from canonical local worklogs
* local CRUD may render joined issue metadata for display, but CRUD writes must not mutate `issue_metadata`
* tombstones must not require or persist a resolved target adapter family or target adapter instance at delete time
* tombstones must not retain remote worklog identifiers as part of local identity
* a later pull for the same issue and reconcile-window allocation must not recreate a deleted canonical row automatically

Import and remote-observation rules:

* local SQLite rows are the only canonical worklog identity
* pull may observe remote worklog IDs during adapter reads, but those IDs are transient execution handles only
* reconcile correctness must not depend on persisting one stable remote worklog ID per local row
* remote segmentation must not define local worklog identity when the same issue/window allocation is observed again
* pull and apply must not populate or refresh `issue_metadata`; adapter-backed issue decoration is a separate action

### Adapter contract

Define small interfaces at the consumer side.

```go
type WorklogAdapter interface {
    Test(ctx context.Context) error
    ListSourceWorklogs(ctx context.Context, query ListSourceWorklogsQuery) ([]RemoteWorklog, error)
    ListTargetWorklogs(ctx context.Context, query ListTargetWorklogsQuery) ([]RemoteWorklog, error)
    GetWorklogCapability(ctx context.Context, issueKey string) (WorklogCapability, error)
    CreateWorklog(ctx context.Context, input CreateRemoteWorklogInput) (CreateRemoteWorklogResult, error)
    DeleteWorklog(ctx context.Context, remoteWorklogID string) error
}
```

```go
type IssueMetadataAdapter interface {
    GetIssueMaxEstimate(ctx context.Context, issueKey string) (*int64, error)
}
```

Rules:

* keep interfaces at the consumer boundary for the service that uses them
* prefer query structs over positional parameters so issue and reconcile-window scope stay explicit
* keep adapter-family differences out of service code
* keep auth and HTTP details inside adapter-specific packages
* provide enough remote read capability for import and planning diff
* treat returned remote IDs as transient adapter handles discovered during plan or apply, not as persisted local identity
* keep issue-metadata decoration separate from worklog planning and apply contracts

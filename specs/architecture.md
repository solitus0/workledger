# Architecture

This file owns runtime architecture, storage architecture, package layout, and reusable execution patterns.

## Platform Docs Index

This folder owns deferred cross-cutting requirements for configuration, runtime state, storage, and the post-MVP CLI roadmap.
Adapter-specific config plus shared pull and delivery behavior are documented separately under [features/004-adapters-and-reconcile/spec.md](features/004-adapters-and-reconcile/spec.md).

The active MVP contract remains in [product.md](product.md).

Shared terminology and grammar live in [glossary.md](glossary.md).

1. [Configuration and state](architecture.md#configuration-and-state)
2. [Storage and architecture](architecture.md#storage-and-architecture)
3. [CLI roadmap](product.md#cli-roadmap)


## Configuration and State

This document captures deferred cross-cutting requirements for configuration and runtime state.

The current MVP configuration shape, `workledger init`, and `workledger config validate` remain in [api.md#configuration-and-storage](api.md#configuration-and-storage).

### Ownership split

For deferred adapter pull and delivery features:

* YAML remains the source of truth for live operator-managed configuration
* SQLite becomes the source of truth for canonical worklogs, runtime state, and historical state

Use YAML for:

* app defaults
* file paths such as the SQLite path
* local worklog validation defaults such as `worklogs.minimum_duration_seconds`
* adapter configuration and credentials
* future adapter-scoped routing rules
* optional selection timezone

Use SQLite for:

* canonical local worklogs
* local worklog lifecycle state
* tombstones for deleted local worklogs
* saved plans
* saved plan items
* adapter delivery correlation
* delivery attempts
* audit events

The CLI must never mutate YAML automatically.

Deferred routing rules live in shared YAML config, but routing is not one global rule set.

Rules:

* routing config is partitioned by adapter family
* each adapter family owns its own routing rule schema and target resolver behavior
* Jira-family adapters may resolve targets from issue-key prefixes
* adapters that do not route from issue key alone must define another resolver contract or require explicit target-family selection

Deferred Jira routing shape:

```yaml
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token: my-jira-token
      routing:
        profiles:
          default:
            issue_prefixes:
              - AAPP
    reporting:
      routing:
        profiles:
          reporting_b:
            reporting_targets:
              AAPP: REPORT-123
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

* Jira routing is instance-local rather than shared top-level config
* each Jira instance may define one or more named routing profiles under `instances.<name>.routing.profiles`
* `default` is the implicit routing profile when no CLI flag selects another profile
* adapter families without target-instance routing rules, such as Clockify, omit routing and rely on their own adapter-local selection contract
* Clockify project mapping, when configured, lives under the `clockify` section because it resolves a project within one configured Clockify target instead of selecting among multiple Clockify targets
* Jira-family `issue_prefixes` is a list of canonical local issue-key prefixes owned by that target Jira instance and preserves the same issue key on the target
* Jira-family `reporting_targets` maps a canonical local issue-key prefix to one fixed reporting issue on the owning target Jira instance
* a `reporting_targets` rule rewrites delivery scope to the configured reporting issue and may collapse many local issue keys into one remote reporting issue
* one route profile may map many source prefixes into the same reporting issue
* one pure reporting profile may also map different source prefixes to different reporting issues within the same Jira adapter family
* within one route profile, the same source prefix must not appear in both `issue_prefixes` and `reporting_targets`
* one route profile must use exactly one Jira delivery mode: `issue_prefixes` or `reporting_targets`
* the same source prefix may map to different reporting issues in different route profiles because `--route-profile` is explicit operator intent
* the same reporting target issue may be referenced by more than one route profile
* reporting delivery must always require an explicit `--route-profile` selection and must never be activated implicitly through the `default` profile
* reporting delivery is an additive mirror into the reporting target and must not mutate the source Jira instance
* each `reporting_targets` rule assumes the target issue is dedicated to `workledger`-managed reporting worklogs
* reporting delivery may target canonical local prefixes that are issue-preserving on another adapter family; for example, `APPS` may remain owned by `jira_cloud.maxima_lt_jira` for direct Jira Cloud routing while `jira_data_center.ito_jira` mirrors `APPS` rows into `ACIU-123` through an explicit reporting route profile
* a reporting route profile is selected within one Jira adapter family and resolves only to instances in that selected family
* any Jira adapter instance that is used as a reporting target must implicitly exclude those reporting target issues from pull into canonical local worklog storage in that same adapter family
* Jira pull-scope exclusion for reporting-loop prevention is defined by explicit issue keys, not by project-level exclusion or arbitrary Jira query text
* adapter-local `pull.exclude_issues` remains available for extra explicit pull exclusions beyond the implicit reporting-target exclusions
* config validation must fail when more than one Jira instance in the same adapter family owns the same source prefix for the same route profile
* extra Jira pull exclusions that are not currently referenced by any reporting route remain valid config
* config validation must fail when one route profile mixes `issue_prefixes` and `reporting_targets`
* config validation must fail when the implicit `default` route profile uses `reporting_targets`
* when a non-default route profile is requested and does not exist under any instance in the selected adapter family, planning must fail clearly as a validation error

### Saved-plan invalidation

Each saved plan must record a deterministic fingerprint of the effective configuration used for planning.

`plan apply` and `plan retry` must compare the current effective configuration fingerprint with the saved fingerprint before executing.

If the fingerprint differs, the saved plan is invalid for execution and must fail clearly instead of silently applying a plan built under different routing, credentials, or selection settings.

### Selection timezone

Deferred planning features may use `selection.timezone` from YAML.

Rules:

* when configured, it defines local calendar boundaries for date-based selection
* otherwise use the system local timezone
* it affects selection only, not the saved plan payload

### Configuration precedence

For deferred features, keep configuration precedence explicit:

1. defaults
2. YAML config
3. CLI flags overriding YAML where applicable

Do not introduce automatic config mutation or hidden precedence rules.

### Author identity

Sync remains a personal-tool workflow.

Deferred features must not add:

* cross-user push
* author override
* adapter impersonation
* service-account delivery on behalf of another user

### Ownership boundaries

This document owns deferred configuration and runtime-state behavior only.

Base local worklog lifecycle, tombstone visibility, and canonical local CRUD behavior remain defined in:

* [api.md#local-worklogs](api.md#local-worklogs)
* [data-model.md#sqlite-schema](data-model.md#sqlite-schema)

Shared pull authority, canonical-identity, and re-import preservation rules remain defined in:

* [features/004-adapters-and-reconcile/spec.md#shared-pull-rules](features/004-adapters-and-reconcile/spec.md#shared-pull-rules)

### Adapter role

Adapters are optional and independently configurable.

Each adapter may support one or both roles:

* import source for local canonicalization
* delivery target for push-style local-authority delivery

Shared planning and apply rules must not assume that one adapter family is always inbound-only or always the only delivery target.


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
* `saved_plans`: `id`, `created_at`, `plan_direction`, `target_adapter_family`, `config_fingerprint`
* `saved_plan_items`: `id`, `saved_plan_id`, `issue_key`, `target_issue`, `route_profile`, `plan_direction`, `target_adapter_family`, `target_adapter_instance`, `window_start_utc`, `window_end_utc`, `plan_status`, `planned_action`, `reason_code`, `reason_detail`, `payload_rows_json`, `inspection_summary_json`, `delivery_key`, `content_hash`
* `delivery_attempts`: `id`, `saved_plan_item_id`, `attempt_state`, `created_at`, `finished_at`, `reason_code`, `reason_detail`, `execution_result_json`
* `audit_events`: `id`, `event_type`, `entity_type`, `entity_id`, `created_at`, `payload_json`

Store scope payload rows and inspection summaries on `saved_plan_items`.
Do not split them into extra child tables in the first implementation because planning, review, apply, and retry all consume them per scope.

Additive indexes beyond MVP:

* unique index on `issue_metadata(issue_key)`
* index on `issue_metadata(refreshed_at)`
* index on `saved_plans(created_at)`
* index on `saved_plan_items(saved_plan_id)`
* index on `saved_plan_items(issue_key, window_start_utc, window_end_utc)`
* index on `saved_plan_items(target_adapter_family, target_adapter_instance, target_issue)`
* index on `saved_plan_items(delivery_key)`
* index on `delivery_attempts(saved_plan_item_id, created_at)`
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

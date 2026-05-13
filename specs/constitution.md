# Constitution

This file owns the cross-cutting product invariants for `workledger`.

## Docs Index

### Current MVP

The active MVP contract lives in [Product](product.md).

1. [Canonical terms and grammar](glossary.md)
2. [MVP docs index](product.md)

### Deferred Features

Future requirements are organized by feature. Shared cross-cutting terminology and grammar live in [the top-level docs contract](glossary.md) so feature docs can reference one contract instead of redefining it.

1. [Platform, storage, and local-ledger CLI roadmap](architecture.md)
2. [Progress reporting proposals](ux.md#progress-reporting)
3. [Optional adapters, pull, and delivery behavior](features/004-adapters-and-reconcile/spec.md)


## Overview and Goals

### Goal

Build a small Go CLI that helps an operator bootstrap local state, validate config, and manage canonical local worklogs in SQLite.
The MVP operator surface is CLI-only.

The canonical MVP command surface is listed in [Product](product.md).

### Product Decisions Frozen for MVP

* ship a single local CLI binary named `workledger`
* use YAML as the live operator-managed configuration
* use SQLite as the canonical worklog authority in MVP
* keep remote adapter pull and delivery out of MVP
* keep output modes to `table` and `json`
* use Cobra for CLI command construction and keep business logic outside Cobra commands
* keep the implementation small and testable
* allow guarded batch delete only as an extension of `workledger worklogs delete`
* defer any TUI surface until after MVP

### Source of Truth

The MVP has a split source of truth:

* YAML is the source of truth for operator-managed configuration
* SQLite is the source of truth for canonical local worklogs

Use YAML for:

* storage location
* default output mode
* optional selection timezone
* optional adapter configuration and credentials

Use SQLite for:

* active local worklogs
* tombstone records for deleted local worklogs

Canonical stored worklog timestamps use UTC.
Operator-facing `started_at` values and local date selection use the configured selection timezone when present.

`workledger init` bootstraps both the local config and the empty SQLite schema.

Local worklog IDs are opaque UUID strings.
The CLI must treat them as stable local identifiers rather than SQLite row IDs.
They are always generated locally by the CLI and are never operator-supplied or reused.

The MVP config schema is strict:

* accept only MVP-supported sections and keys
* reject unsupported top-level sections
* reject unknown keys inside known sections

### Explicit Non-Goals

Deferred features and commands are listed in [Product](product.md#known-deferred-areas).

The MVP may expose deleted-worklog tombstones through existing `worklogs` commands only.
It does not add tombstone-specific top-level commands.


## Shared Pull Rules

This document defines the shared business rules for adapter worklog import into the canonical local ledger.

Adapter-specific docs should define only:

* configuration and credentials
* target selection or routing rules
* adapter-specific capability differences

Shared reconcile planning and apply behavior lives in [features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review](features/004-adapters-and-reconcile/spec.md#reconcile-planning-and-review), [features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry](features/004-adapters-and-reconcile/spec.md#reconcile-apply-and-retry), and [architecture.md](architecture.md).

### `workledger plan reconcile --pull --adapter=<family>`

Pull is planned through `workledger plan reconcile --pull --adapter=<family>` and executed through `workledger plan apply`.

The pull flow must:

* validate the selected adapter config before any remote call
* read only the operator-visible worklogs within the adapter's configured scope
* normalize imported fields into the shared canonical allocation contract
* persist a saved pull plan in SQLite without mutating local canonical worklogs during reconcile
* apply the saved pull plan into canonical local SQLite state only when the operator runs `workledger plan apply`
* report deterministic results without mutating YAML or another adapter

Pull rules:

* remote adapters never become the canonical source of truth
* local SQLite worklogs are the only canonical worklog identity
* imported adapter worklogs become normal canonical local worklogs after merge
* imported and manual worklogs use the same CRUD path after canonicalization
* issue-level decoration data such as Jira max estimate is not part of pull reconcile and must be refreshed through a separate adapter action
* when one Jira issue is configured as a reporting-delivery target, that issue must be excluded from Jira pull scope so reporting rows are not re-imported into canonical local storage
* Jira reporting-loop prevention uses explicit issue-key exclusions in adapter pull config
* pull may merge observations from multiple adapters when they describe the same issue and time-window allocation
* remote row boundaries and remote row IDs must not define canonical local identity
* pull may coalesce differently segmented remote rows when needed to preserve canonical local allocation
* local canonical values remain authoritative after later pulls for the same issue and reconcile-window allocation
* later pulls must preserve local canonical edits
* later pulls must not overwrite local canonical rows automatically
* later pulls must not recreate deleted local allocation automatically when matching remote work is still visible
* deleted local tombstones must prevent blind recreation of the same issue/window allocation from a later pull


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

Deferred shared shape:

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
      pull:
        exclude_issues:
          - REPORT-123
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
* a reporting route profile is selected within one Jira adapter family and resolves only to instances in that selected family
* any Jira adapter instance that is used as a reporting target must exclude those reporting issues from pull into canonical local worklog storage in that same adapter family
* Jira pull-scope exclusion for reporting-loop prevention is defined by explicit issue keys, not by project-level exclusion or arbitrary Jira query text
* config validation must fail when a reporting target issue is not listed in the owning Jira instance `pull.exclude_issues` set
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

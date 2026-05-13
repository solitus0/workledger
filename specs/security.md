# Security

This file owns local credential handling, permission requirements, authentication boundaries, and destructive-operation safeguards.

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

### Command surface in this slice

Pull uses `workledger plan reconcile --pull --adapter=jira-cloud` followed by `workledger plan apply`.
Push uses `workledger plan reconcile --push --adapter=jira-cloud`, `workledger plan show`, `workledger plan apply`, and `workledger plan retry`.
Jira Cloud routing resolves target adapter instances from the selected family's instance-local route profiles where configured.
Totals use `workledger totals --adapter=jira-cloud`.
Shared adapter status also covers bare `workledger status` and filtered `workledger status --adapter=jira-cloud`.

Shared status rules:

* bare `workledger status` inspects only configured adapter families and every configured instance owned by each family
* `workledger status --adapter=<family>` supports `clockify`, `jira-cloud`, and `jira-data-center`
* when the selected family has zero configured targets, or when no families are configured, the command succeeds with an empty result set
* bare `workledger status` keeps collecting and rendering rows even when one adapter or instance fails; successful rows report `status="OK"`, failed rows report the adapter error message, and the command returns the first deterministic non-zero exit code after rendering
* JSON output uses one normalized payload shape as `{"items":[...]}`
* each `items[]` entry includes `adapter`, `instance`, `status`, `base_url`, `workspace_id`, `user_id`, and `user`; fields that do not apply to that adapter are `null`
* table output uses the shared headers `ADAPTER`, `INSTANCE`, `STATUS`, `BASE_URL`, and `USER`
* in table output, Clockify renders `workspace_id` in `INSTANCE` and `user_id` in `USER`

Issue-metadata refresh rules:

* the command must remain separate from reconcile planning and apply
* `--field=max-estimate` reads Jira issue original estimate and stores it as local `issue_metadata.max_estimate_seconds`
* selection reuses active local-worklog selectors and deduplicates remote reads by `issue_key`
* missing Jira original estimate clears the local cached value to `null`
* `--instance <name>` is required when more than one `jira_cloud` instance is configured

### `workledger totals --adapter=jira-cloud`

`workledger totals` compares canonical local booked time against remote Jira Cloud booked time for one selected date window when filtered with `--adapter=jira-cloud`.

It must:

* validate the deferred Jira Cloud config section before any remote call
* require exactly one selected date window supplied either by `--from` plus `--to` or by exactly one date-window shortcut selector
* require one resolved Jira Cloud instance and require `--instance <name>` when more than one `jira_cloud` instance is configured
* derive managed totals scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* ignore `reporting_targets` when deriving totals scope
* exclude exact issue keys from both local and remote totals when those keys appear in `pull.exclude_issues` or as configured `reporting_targets` target issues on the selected instance
* fail validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* read local totals from active canonical SQLite worklogs only after filtering to that managed routed issue-prefix scope
* exclude deleted tombstones from local totals
* authenticate with the configured Jira Cloud credentials and compare against worklogs authored by the authenticated Jira user only
* discover candidate issues with `worklogAuthor = currentUser() AND worklogDate >= "<from>" AND worklogDate <= "<to>"`
* keep only authenticated-user Jira Cloud worklogs whose issue keys match that same managed routed issue-prefix scope
* compare totals by overlap with the effective selected window rather than by start timestamp alone
* split both local and remote overlapping intervals at local day boundaries in the effective selection timezone before per-day aggregation
* compute aggregate totals and one per-day comparison row from the same sliced interval set
* report `match` only when aggregate totals match exactly and every returned day matches exactly
* report `mismatch` when any aggregate or per-day delta is non-zero
* return exit code `0` for `match` and `mismatch`
* use exit code `2` for validation or config errors, `3` for surfaced `404`, `4` for auth failure, and `5` for remote or connectivity failure
* remain read-only
* render deterministic output in `table` or `json`
* `--details` expands table output to include per-day rows and does not change JSON output


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
```

Rules:

* the entire `jira_data_center` section is optional
* each instance name must be unique within `jira_data_center.instances`
* `base_url` is required for each configured instance
* auth uses `bearer.token` only in this slice
* credentials must stay local-only and must never be written back by the CLI
* `jira_data_center.instances.<name>.routing`, when present, owns route profiles for delivery into that specific target instance
* `jira_data_center.instances.<name>.pull.exclude_issues` is optional and lists extra issue keys that pull must not import
* Jira Data Center reporting target issues are implicitly excluded from pull on the owning instance and do not need to be repeated in `pull.exclude_issues`
* one Jira Data Center route profile may not mix `issue_prefixes` and `reporting_targets`

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
* pull and push use authenticated-current-user scope only
* if a saved push cleanup scope would require touching foreign-authored Jira worklogs, that saved plan item must be classified as `blocked`

### `workledger totals --adapter=jira-data-center`

`workledger totals` compares canonical local booked time against remote Jira Data Center booked time for one selected date window when filtered with `--adapter=jira-data-center`.

It must:

* validate the deferred Jira Data Center config section before any remote call
* require exactly one selected date window supplied either by `--from` plus `--to` or by exactly one date-window shortcut selector
* require one resolved Jira Data Center instance and require `--instance <name>` when more than one `jira_data_center` instance is configured
* derive managed totals scope from the union of all `issue_prefixes` across every routing profile configured on the selected instance
* ignore `reporting_targets` when deriving totals scope
* exclude exact issue keys from both local and remote totals when those keys appear in `pull.exclude_issues` or as configured `reporting_targets` target issues on the selected instance
* fail validation when the selected instance has no routing config or when its routing profiles contribute zero `issue_prefixes`
* read local totals from active canonical SQLite worklogs only after filtering to that managed routed issue-prefix scope
* exclude deleted tombstones from local totals
* authenticate with the configured Jira Data Center bearer token and compare against worklogs authored by the authenticated Jira user only
* discover candidate issues with `worklogAuthor = currentUser() AND worklogDate >= "<from>" AND worklogDate <= "<to>"`
* keep only authenticated-user Jira Data Center worklogs whose issue keys match that same managed routed issue-prefix scope
* compare totals by overlap with the effective selected window rather than by start timestamp alone
* split both local and remote overlapping intervals at local day boundaries in the effective selection timezone before per-day aggregation
* compute aggregate totals and one per-day comparison row from the same sliced interval set
* report `match` only when aggregate totals match exactly and every returned day matches exactly
* report `mismatch` when any aggregate or per-day delta is non-zero
* return exit code `0` for `match` and `mismatch`
* use exit code `2` for validation or config errors, `3` for surfaced `404`, `4` for auth failure, and `5` for remote or connectivity failure
* remain read-only
* render deterministic output in `table` or `json`
* `--details` expands table output to include per-day rows and does not change JSON output

Table output must render only the aggregate `TOTAL` row by default, render per-day rows before `TOTAL` when `--details` is present, and append one final summary line naming the adapter, resolved instance, effective selected range, effective timezone, and overall state.


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

### `workledger totals` and `workledger totals --adapter=clockify`

Bare `workledger totals` compares canonical local booked time against every configured adapter target for one selected date window.
When filtered with `--adapter=clockify`, it compares canonical local booked time against remote Clockify booked time for that same window.

It must:

* require exactly one selected date window supplied either by `--from` plus `--to` or by exactly one date-window shortcut selector
* read local totals from active canonical SQLite worklogs only
* exclude deleted tombstones from local totals
* bare `workledger totals` inspects only configured adapter families and every configured instance owned by each family
* bare `workledger totals` orders rows by family as `clockify`, `jira-cloud`, then `jira-data-center`; Jira instance rows are sorted by instance name ascending
* bare `workledger totals` contributes at most one Clockify row, one Jira Cloud row per configured instance, and one Jira Data Center row per configured instance
* bare `workledger totals` validates and executes each adapter target independently
* bare `workledger totals` keeps collecting and rendering rows even when one target fails
* bare `workledger totals` returns an empty `items` array and exit code `0` when no adapters are configured
* when `--adapter=clockify` is selected, validate the deferred Clockify config section before any remote call
* when `--adapter=clockify` is selected, read remote totals from Clockify entries visible to the configured `clockify.workspace_id` and `clockify.user_id` scope only
* compare totals by overlap with the effective selected window rather than by start timestamp alone
* split both local and remote overlapping intervals at local day boundaries in the effective selection timezone before per-day aggregation
* compute aggregate totals and one per-day comparison row from the same sliced interval set
* report `match` only when aggregate totals match exactly and every returned day matches exactly
* report `mismatch` when any aggregate or per-day delta is non-zero
* report `indeterminate` when a running Clockify entry overlaps the selected effective window
* explicit `--adapter=clockify` returns exit code `0` for `match`, `mismatch`, and `indeterminate`
* bare `workledger totals` returns the first deterministic non-zero per-target exit code after rendering all rows
* use exit code `2` for validation or config errors, `4` for auth failure, and `5` for external or connectivity failure
* remain read-only
* render deterministic output in `table` or `json`
* bare JSON output renders `{"items":[...]}` where each entry includes `adapter`, `instance`, `from`, `to`, `timezone`, and either successful `summary` plus `days` fields or failed `status` plus `message` fields
* bare table output renders one row per adapter target with effective window metadata, comparable summary totals, and deterministic failure text for failed targets

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

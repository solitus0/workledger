# workledger

`workledger` is a local-first CLI for managing canonical worklogs in SQLite and reconciling them with remote time-tracking systems through reviewed, saved plans.

It is built for operators and coding agents that need a reliable workflow for recording work, inspecting gaps, correcting entries, comparing adapter totals, and synchronizing with systems such as Jira and Clockify without making those systems the source of truth.

## Why it exists

Worklogs are easy to scatter across chats, commits, tickets, spreadsheets, and remote trackers. `workledger` keeps the authoritative ledger local, deterministic, and inspectable:

- YAML owns operator-managed configuration.
- SQLite owns canonical local worklogs, tombstones, saved plans, issue metadata, and audit state.
- Remote adapters provide evidence, comparison data, and sync targets, but they do not replace the local ledger.
- Reconcile operations are planned, saved, reviewed, and then applied.
- `table` output is human-friendly; `json` output is stable enough for automation and coding agents.

## Current status

This repository is specs-first. The canonical product and CLI contract lives in:

- [`specs/functional.md`](specs/functional.md)
- [`specs/non-functional.md`](specs/non-functional.md)

The README is an operator guide, not the authority. When behavior differs, the specs win.

Deferred or out of scope in the current spec:

- `workledger tui`
- tombstone-specific top-level commands

## Install

### Homebrew

```sh
brew install solitus0/tap/workledger
```

### GitHub Releases

Download the matching binary from the releases page:

https://github.com/solitus0/workledger/releases

### Go install

```sh
go install github.com/solitus0/workledger/cmd/workledger@latest
```

### From source

```sh
go build -o workledger ./cmd/workledger
./workledger version
```

For local development, you can also run commands through Go directly:

```sh
go run ./cmd/workledger --help
```

## Recommended agent workflow

If you use an agent with `workledger`, install the `session-worklog-creator` skill to turn current coding-session context into local worklogs with minimal prompting.

```sh
npx skills add solitus0/workledger --skill session-worklog-creator -g
```

If you use Codex and want it to write local worklogs to the shared SQLite database outside the current agent session without asking for permission on every add, allow the default storage path in your Codex sandbox config:

```toml
[sandbox_workspace_write]
writable_roots = [
  "~/.local/share/workledger"
]
```

## Quick start

Bootstrap local configuration and SQLite storage:

```sh
workledger init
workledger config validate
workledger doctor
```

Add and inspect local worklogs:

```sh
workledger worklogs add \
  --issue PROJ-123 \
  --started todayT09:00 \
  --duration 2h \
  --description "Implement local worklog reconciliation"

# Supported --started forms:
# 2026-05-14T09:00
# todayT09:00
# yesterdayT09:00
# tomorrowT09:00
# +2dT09:00
# -3dT09:00

workledger worklogs list --today
workledger worklogs context --today --output json
```

Compare local totals with a configured adapter:

```sh
workledger totals --instance clockify --today
```

Create, review, and apply a remote sync plan:

```sh
workledger plan reconcile --push --instance clockify --today
workledger plan show
workledger plan apply
```

## Core concepts

| Concept | Role |
| --- | --- |
| Config | `~/.config/workledger/config.yaml`; source of truth for operator-managed settings and adapter references. |
| SQLite ledger | Canonical local store for active worklogs and deleted-worklog tombstones. |
| Local worklog | One canonical row with issue key, UTC start, duration, description, and generated local ID. |
| Tombstone | Deleted local worklog record retained for restore, pull protection, and delete-only reconciliation. |
| Issue metadata | Locally cached issue context, such as Jira estimate metadata, used for planning and inspection. |
| Saved plan | Durable remote sync contract produced by `plan reconcile` and executed by `plan apply` or `plan retry`. |
| Adapter | A remote integration family such as `clockify`, `jira-cloud`, or `jira-data-center`. |

## Configuration and setup

`workledger init` creates a starter config and provisions SQLite storage. The starter config includes local defaults and commented adapter scaffolds.

Default local settings from the spec include:

```yaml
default_output: table
local_timezone: Europe/Vilnius
storage:
  sqlite_path: ~/.local/share/workledger/worklogs.db
worklogs:
  minimum_duration_seconds: 900
  daily_minimum_quota_seconds: 28800
  daily_lunch: 12:00-13:00
```

Adapter secrets are referenced through environment variable names. Inline tokens and API keys are intentionally invalid configuration.

Useful setup commands:

```sh
workledger config validate
workledger config env
workledger config env --print-export-template
workledger config env --dotenv-template
workledger config summary
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
workledger setup jira-data-center --instance dc --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
workledger setup clockify --workspace-id <workspace-id> --user-id <user-id> --api-key-env CLOCKIFY_API_KEY --project-map PROJ=Engineering
workledger doctor
```

## Local worklog workflow

Use local worklog commands when the canonical SQLite ledger should change.

```sh
workledger worklogs list --today
workledger worklogs search "reconciliation"
workledger worklogs add --issue PROJ-123 --started todayT09:00 --duration 2h --description "Implement reconciliation flow"
workledger worklogs update <id> --duration 1h45m --description "Refine reconciliation flow"
workledger worklogs shift --today --issue PROJ-123 --by 15m --dry
workledger worklogs delete <id>
workledger worklogs delete --today --issue PROJ-123 --dry
workledger worklogs delete --today --issue PROJ-123 --yes
workledger worklogs restore --today --issue PROJ-123 --dry
workledger worklogs restore --today --issue PROJ-123 --yes
```

Write operations validate issue keys, timestamps, minimum duration, duplicate rows, and overlaps. Use `--force` only when you deliberately want to bypass duplicate or overlap rejection on supported write paths.

## Agent-friendly drafting flow

Coding agents should use `worklogs context` and `worklogs apply` instead of inventing ledger state from chat history. The CLI owns validation and persistence; agents provide evidence interpretation, slot selection, and payload drafting.

1. Inspect the selected day or range.

   ```sh
   workledger worklogs context --today --output json
   ```

   Use the context response as the planning source of truth for existing rows, free slots, collisions, quota deltas, issue order, and payload guidance.

2. Draft one raw apply payload with top-level `adds`.

   ```json
   {
     "adds": [
       {
         "issue_key": "PROJ-123",
         "started_at_utc": "2026-05-14T07:00:00Z",
         "duration_seconds": 7200,
         "description": "Implement local worklog reconciliation"
       }
     ]
   }
   ```

   Each add must include `issue_key`, `duration_seconds`, `description`, and exactly one of `started_at` or `started_at_utc`. Prefer `started_at_utc` for deterministic automation; use `started_at` when local civil time input is intentional.

3. Dry-run the full payload.

   ```sh
   workledger worklogs apply --stdin --dry --output json < worklogs.json
   ```

   Successful JSON dry-runs return `dry_run`, `summary`, and `items`. No canonical local state changes during this step.

4. Apply only after validation succeeds and the operator intended a mutation.

   ```sh
   workledger worklogs apply --stdin --output json < worklogs.json
   ```

The CLI owns duplicate detection, overlap detection, timestamp parsing, full-payload validation, atomic writes, and persistence. Agents should focus on evidence interpretation, issue allocation, slot selection, and concise descriptions.

## Routing, metadata, and totals

Routing and metadata commands help explain where worklogs belong and how local totals compare with remote systems.

```sh
workledger routing list
workledger route explain PROJ-123
workledger clockify mappings validate
workledger issue-metadata list --issue PROJ-123
workledger issue-metadata list --current-month
workledger issue-metadata refresh --adapter=jira-cloud --field=max-estimate --current-month
workledger status
workledger status --instance clockify
workledger totals --instance clockify --today
workledger totals --instance main --current-week --details
```

Totals read local time from active canonical SQLite worklogs and compare it with selected adapter targets for one explicit date window.

## Reconciliation workflow

Remote sync is intentionally plan-based:

1. `plan reconcile` inspects local and remote state and saves a plan.
2. `plan show` renders the saved plan without new external requests.
3. `plan apply` executes ready items from one saved plan.
4. `plan retry` retries explicit failed or uncertain scopes from an existing plan.

Examples:

```sh
workledger plan reconcile --push --adapter=clockify --today
workledger plan reconcile --pull --instance main --current-week
workledger plan reconcile --push --instance dc --route-profile reporting --from 2026-05-01 --to 2026-05-14
workledger plan show
workledger plan show <plan-id> --only-ready
workledger plan list --current-month
workledger plan apply <plan-id>
workledger plan retry <plan-id> --only failed
workledger plan retry <plan-id> --only uncertain
```

`plan reconcile` is non-destructive. It creates a saved execution contract; it does not mutate canonical local worklogs or remote systems by itself.

## Output and automation contracts

- User-facing command output goes to stdout.
- Logs, diagnostics, and progress go to stderr.
- `--output json` stdout is valid JSON and is never mixed with logs.
- Output mode precedence is command `--output`, then config `default_output`, then built-in `table`.
- Date-window shortcuts include `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, and `--last-month`.
- Explicit windows use `--from <date> --to <date>`.

Exit codes:

| Code | Meaning |
| ---: | --- |
| 0 | Success |
| 1 | Unexpected failure |
| 2 | Validation or input failure |
| 3 | Not found |
| 4 | Authentication failure |
| 5 | External or connectivity failure |
| 6 | Partial success |

## Command surface

```text
workledger
workledger help
workledger version
workledger init

workledger config validate
workledger config env
workledger config summary
workledger doctor

workledger setup jira-cloud
workledger setup jira-data-center
workledger setup clockify

workledger routing list
workledger route explain <issue-key>
workledger clockify mappings validate

workledger worklogs list
workledger worklogs search <query>
workledger worklogs add
workledger worklogs update <id>
workledger worklogs shift
workledger worklogs apply
workledger worklogs delete [<id>]
workledger worklogs restore
workledger worklogs context

workledger issue-metadata list
workledger issue-metadata refresh
workledger status
workledger totals

workledger plan reconcile
workledger plan show [<plan-id>]
workledger plan list
workledger plan apply [<plan-id>]
workledger plan retry <plan-id>
```

Every root, group, and leaf command supports `-h` and `--help`.

## Security model

`workledger` is local-first and single-operator by design:

- Config and SQLite files use private local permissions.
- Adapter secrets are referenced by env-var names such as `token_env` and `api_key_env`.
- Inline adapter secrets are rejected.
- Sync features do not add cross-user push, author override, adapter impersonation, or service-account delivery on behalf of another user.
- Reporting delivery is an additive mirror and does not mutate source-system worklogs.

## Development notes

The implementation contract reserves these package responsibilities:

- `cmd/workledger`: process startup and dependency wiring
- `internal/cli`: flags, rendering, confirmation, and exit code mapping
- `internal/config`: YAML loading, path resolution, normalization, and validation
- `internal/worklogs`: local CRUD, selectors, tombstones, duplicate and overlap checks
- `internal/issues`: issue metadata refresh and advisory issue context
- `internal/plans`: saved-plan creation, review, apply, retry, and orchestration
- `internal/adapter`: shared adapter contracts and family-specific helpers
- `internal/store/sqlite`: SQLite stores, migrations, and transactions

Business logic should stay outside Cobra commands so the CLI and future frontends can reuse the same services.

## Contributing

Start with the specs before changing behavior:

- Add new functional requirements under the most specific group in `specs/functional.md`.
- Add new non-functional requirements under the most specific group in `specs/non-functional.md`.
- Do not duplicate rules across groups.
- Keep CLI rendering, exit-code mapping, and business logic separated by package responsibility.
- Prefer deterministic JSON contracts for automation-facing behavior.

For user-facing changes, update this README only after the spec contract is updated.

---
name: worklog-orchestrator
description: operate the workledger cli as the bridge between coding agents, canonical local sqlite worklogs, yaml config, and remote adapter reconcile plans. use when chatgpt needs to inspect or mutate local worklogs, draft worklogs from coding-session evidence, run workledger setup or diagnostics, compare adapter totals, refresh issue metadata, or create, review, apply, or retry saved reconcile plans.
---

## Alpha tester behavior

The agent should actively surface opportunities to improve the `workledger` CLI tool.

- Suggest CLI improvements when real usage exposes friction, ambiguity, missing commands, weak validation, confusing output, or awkward workflow edges.

# Worklog Orchestrator

Operate `workledger` as the authority for worklog state. Use reasoning to choose commands and draft payloads, not to replace CLI validation.

## Operating principles

- Treat YAML config as the source of truth for operator-managed settings.
- Treat SQLite local worklogs as the canonical ledger.
- Treat adapter systems, issue metadata, Git history, and session artifacts as supporting evidence.
- Treat saved reconcile plans as the only execution contract for remote sync.
- Inspect before mutating unless the user explicitly asks for a single unambiguous local write.
- Check the effective SQLite path before local mutations when the environment may sandbox writes.
- Use `--output json` when a result will drive another command, payload, or agent decision.
- Keep local worklog CRUD separate from remote reconcile workflows.

## Request routing

| User intent | Use this command family |
| --- | --- |
| Initialize or troubleshoot setup | `init`, `config *`, `setup *`, `doctor` |
| Explain adapter routing | `routing list`, `route explain`, `clockify mappings validate` |
| Inspect or edit canonical local rows | `worklogs list`, `search`, `add`, `update`, `shift`, `delete`, `restore` |
| Draft many local rows from evidence | `worklogs context` plus `worklogs apply` |
| Inspect issue context or compare totals | `issue-metadata *`, `status`, `totals` |
| Sync with remote systems | `plan reconcile`, `plan show`, `plan list`, `plan apply`, `plan retry` |

Load `references/command-contract.md` when exact flags, selectors, output contracts, or command examples matter.

## Safe execution loop

1. Classify the request by command family.
2. Inspect relevant current state with the smallest read command.
3. Draft the command or payload.
4. Use dry-run or review commands where available:
   - `worklogs apply --dry` for batch local adds.
   - `worklogs shift --dry`, selector-based `delete --dry`, and `restore --dry` for broad local mutations.
   - `plan show` before `plan apply` or `plan retry` for remote sync.
5. Execute only the command family the user requested.
6. Summarize what changed, what did not change, and the next concrete command when useful.

## Local worklog drafting loop

Use this path when session notes, coding-agent output, Git evidence, or user notes need to become canonical local worklogs.

1. Resolve target dates, issue keys, duration constraints, and whether mutation is requested.
2. Inspect placement with `workledger worklogs context <date-window> --output json`.
3. Merge supporting evidence into issue-level work bundles. Do not emit one row per chat, commit, or session by default.
4. Draft one raw `worklogs apply` payload with top-level `adds`.
5. Prefer `started_at_utc` for deterministic automation; use `started_at` only when local civil time is the user-facing requirement.
6. Validate the payload with `workledger worklogs apply --dry --output json`; the CLI dry-run is the authoritative payload check.
7. Execute `workledger worklogs apply --stdin --output json` only after the CLI dry-run succeeds and the user intended the write.

Load `references/agentic-flow.md` for allocation, evidence, payload, and response-shaping rules.

## Remote reconcile boundary

- Use `status`, `totals`, and `issue-metadata *` for inspection only.
- Use `plan reconcile` to create one saved plan. It is non-destructive and has no separate dry-run mode.
- Use `plan show` or `plan list` to review saved plans without new external requests.
- Use `plan apply` only for reviewed ready plan items.
- Use `plan retry <plan-id> --only failed` or `--only uncertain` only for explicit retry scopes.
- Never turn `plan reconcile` into a shortcut for local CRUD or remote mutation.

## Required selector habits

- Shared date-window selectors are `--today`, `--yesterday`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, or `--from <date> --to <date>`.
- `worklogs list`, `worklogs restore`, `totals`, and `plan reconcile` require an explicit date window.
- `worklogs shift` requires at least one selector plus `--by <duration>`.
- Selector-based `worklogs delete` requires selectors and `--yes` for execution; use `--dry` for preview.
- `worklogs apply` requires exactly one payload source, either `--stdin` or `--file <path>`.
- `plan reconcile` requires exactly one of `--pull` or `--push`, at least one adapter or instance scope, and exactly one date window.
- `plan retry` requires a plan ID and an explicit scope such as `--only failed` or `--only uncertain`.

## Load on demand

- Exact CLI contracts and examples: `references/command-contract.md`
- Coding-agent drafting workflow: `references/agentic-flow.md`
- Session artifact format and renderer usage: `references/session-artifacts.md`
- Session artifact renderer: `scripts/render_session_artifact.py`

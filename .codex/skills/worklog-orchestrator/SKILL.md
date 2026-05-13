---
name: worklog-orchestrator
description: inspect, create, update, and delete canonical local worklogs through `workledger worklogs *`. use when chatgpt needs to turn codex session evidence into candidate local worklogs, inspect or clean up saved worklogs, or prepare for deferred `workledger status` and `workledger plan *` clockify reconciliation workflows.
---

# Worklog Orchestrator

Use this skill to manage canonical local worklogs first. The primary interface is `workledger worklogs *`, with `list|show|add|update|delete` for direct CRUD. Deferred local batch planning uses `context|apply` after those commands land. Session artifacts and Clockify data are supporting inputs, not the system of record.

## Ownership

- SQLite-backed local worklogs are canonical.
- Clockify entries are never canonical.
- Session artifacts are evidence only.
- Deferred adapter flows must go through saved-plan commands such as `workledger plan reconcile`, `workledger plan show`, and `workledger plan apply`.

## Primary workflow

1. Resolve whether the request is single-entry CRUD, batch local planning, or deferred adapter reconciliation.
2. Inspect existing local state with `workledger worklogs list`, `workledger worklogs show`, or `workledger worklogs context` before proposing mutations.
3. Gather evidence only as needed:
   - current conversation
   - existing session artifact
   - Git or Jira context when helpful
4. If reusable evidence is missing, optionally create or refresh a durable session artifact with `scripts/render_session_artifact.py`.
5. For one-off row creation, use `workledger worklogs add` after inspection.
6. For multi-entry creation, when deferred batch commands are available, prefer `workledger worklogs context` -> external raw payload construction -> `workledger worklogs apply`.
7. Use Codex-side heuristics to turn `context` free slots, issue hints, and session evidence into one raw `apply` payload. If a heuristic conflicts with CLI validation, the CLI contract wins.
8. Reserve `workledger worklogs update` for existing-row corrections and `workledger worklogs delete` for explicit cleanup after inspection.
9. Keep deferred `workledger status --adapter=clockify` and `workledger plan *` flows secondary.

## Read only what you need

- CRUD workflow and deferred flow boundaries: `references/workflow.md`
- Command shapes and deprecated DSL translation: `references/dsl.md`
- Payload-construction heuristics for allocation, rounding, and descriptions: `references/allocation.md`
- Output contracts for inspection, payload construction, and cleanup: `references/output-contracts.md`
- Session artifact and evidence rules: `references/evidence.md`
- Legacy DSL translation helper: `scripts/parse_worklog_dsl.py`
- Payload validator for non-trivial candidate sets: `scripts/validate_worklog_plan.py`
- Session artifact renderer: `scripts/render_session_artifact.py`

## Primary command patterns

Inspect local worklogs:

```text
workledger worklogs list
workledger worklogs list --current-month --issue SBAB-292
workledger worklogs list --from today --to today --issue SBAB-292
workledger worklogs show 42
```

Create one canonical local worklog:

```text
workledger worklogs add --issue SBAB-292 --started 2026-05-02T08:00 --duration 2h --description "Refined worklog drafting skill docs"
workledger worklogs add --issue SBAB-292 --started todayT08:00 --duration 2h --description "Refined worklog drafting skill docs"
workledger worklogs add --issue SBAB-292 --started-utc 2026-05-02T05:00:00Z --duration 2h --description "Refined worklog drafting skill docs"
```

Update a saved local worklog:

```text
workledger worklogs update 42 --duration 2h30m --description "Refined worklog drafting skill docs and validation rules"
```

Delete incorrect local worklogs after inspection:

```text
workledger worklogs delete 42
workledger worklogs delete 42 --hard
workledger worklogs list --last-month --issue SBAB-292
workledger worklogs list --from today --issue SBAB-292
workledger worklogs delete 42
workledger worklogs delete 43
```

Inspect planning context and apply batch additions once the deferred batch flow is implemented:

```text
workledger worklogs context --current-month --issue SBAB-292 --output json
workledger worklogs context --today --issue SBAB-292 --output json
workledger worklogs apply --stdin --dry --output json
workledger worklogs apply --stdin --output json
```

## Session-evidence to CRUD example

When a user asks to turn session evidence into local worklogs:

1. Gather or refresh the session artifact if it improves traceability.
2. Inspect the target day or range with `workledger worklogs context` when placement matters and the deferred batch flow is available.
3. Build one raw `worklogs apply` payload from the returned free slots, planning issue hints, and session evidence whenever the request needs more than one new row.
4. Validate the payload with `workledger worklogs apply --dry`, then execute through `workledger worklogs apply`.
5. Until that flow is implemented, fall back to explicit `workledger worklogs add` commands.

## Payload construction rules

- Treat free-slot derivation, workday and lunch defaults, minimum duration, collision visibility, and payload validation as CLI-owned whenever `context` or `apply` are available.
- Use Codex-side heuristics for slot selection, issue allocation, entry splitting, and description selection based on session evidence and `context.planning`.
- Construct only raw apply payloads with top-level `adds`.
- Each add must include `issue_key`, exactly one of `started_at` or `started_at_utc`, `duration_seconds`, and `description`.
- Prefer `started_at_utc` in generated payloads for deterministic automation.
- Keep descriptions concise and free of Jira keys.
- Do not create artificial fragmentation.
- Ask before using overtime when the proposed total exceeds the primary workday.

## Deferred secondary flow

Use this only when the user explicitly asks for adapter status or reconciliation planning and the deferred adapter flow is in scope:

```text
workledger status --adapter=clockify
workledger plan reconcile --push --adapter=clockify
workledger plan show
workledger plan apply
workledger plan list
workledger plan retry
```

These flows review or apply saved plans. They do not replace local worklog CRUD as the main workflow.

## Legacy note

If the user supplies the old flags-only orchestration DSL, treat it as deprecated compatibility input. Use `scripts/parse_worklog_dsl.py` to translate it, then restate the result as `workledger worklogs *` commands or as deferred `workledger plan *` actions when appropriate.

---
name: session-worklog-creator
description: create canonical local workledger worklog entries from the current chatgpt or coding-agent session context. use when the user directly invokes this skill inside an active work context, or asks to create, add, book, or log worklogs for the current session. do not use for setup, diagnostics, totals, searching, editing or deleting existing worklogs, remote adapter sync, reconcile plans, artifact rendering, or general workledger cli operation.
---

# Session Worklog Creator

Create local `workledger` worklog entries from the current session context with minimal user input.

## Core behavior

- Treat direct skill invocation inside an active session as an intent to create local worklogs, not just draft suggestions.
- Use the current visible conversation, coding-agent transcript, tool output, branch names, commit snippets, issue keys, and user-provided notes already present in the session.
- Do not ask the user to restate information that is already available in the current context.
- Do not use old, unrelated conversation history or broad repository mining to manufacture evidence.
- Do not run setup, diagnostics, totals, search, edit/delete, remote sync, reconcile-plan, or artifact-rendering workflows.
- If `workledger` is unavailable, stop and report that no worklog was created.

## Empty-context stop rule

Before running commands, decide whether the current session contains usable worklog evidence.

The context is empty when it contains only the invocation or a generic request, with no concrete work performed, issue key, task identifier, branch/commit evidence, duration, or summary.

When context is empty:

1. Do not inspect files.
2. Do not run `workledger`.
3. Stop with this message shape:

```text
I don't have enough current session context to create a worklog. Provide the worklog data: date, issue key(s), duration, and a short summary of the work.
```

## Happy-path automation

When the current session contains enough evidence, complete the flow automatically:

1. Extract the worklog facts from current context:
   - date window: explicit date from context, otherwise today
   - issue key(s): explicit keys first; if none are present in session context, look up the current git branch name and derive a key from it when possible; only then fall back to keys from commits, task titles, or session notes
   - work summary: concrete completed work, investigation, implementation, review, testing, or planning supported by context
   - duration: explicit duration, timer output, timestamp span, or other clearly supported duration evidence
2. If a critical field is still missing after context review, ask only for the missing field(s) and stop. Do not draft placeholder worklogs.
3. Run `workledger worklogs context <date-window> --output json` to inspect existing local rows, daily quota, free slots, and collisions.
4. Combine evidence into issue-level bundles. Do not create one row per chat turn, command, commit, or file.
5. Allocate duration across issues using explicit user/context constraints first, then evidence strength, then equal split.
6. Place entries chronologically inside free slots from `worklogs context`; split only when required by slot boundaries or genuinely distinct issue work.
7. Create one JSON payload with top-level `adds`.
8. Validate the payload with `workledger worklogs apply --stdin --dry --output json` or `workledger worklogs apply --file payload.json --dry --output json`.
9. If dry-run fails, inspect the CLI error and try to repair the payload yourself before involving the user. Re-run dry-run after each repair. Ask the user only when the failure cannot be resolved from current context, such as a missing issue key, missing duration evidence, ambiguous date, or a business-rule conflict that requires their choice.
10. If dry-run succeeds, apply with `workledger worklogs apply --output json` without asking for another confirmation.
11. Report the created entries and any warnings from the CLI.

Direct invocation is enough authorization for the local create step. The dry-run is the safety gate; do not add a second approval prompt when the dry-run succeeds.

For one-shot payloads created during the session, prefer stdin over temp files:

- validate and dry-run with `workledger worklogs apply --stdin --dry --output json`
- apply with `workledger worklogs apply --stdin --output json`

Create a temp payload file only when persistence is genuinely needed for debugging or the user explicitly asks for a saved artifact.

## Critical-field rules

- Never invent an issue key.
- If the current session does not contain an explicit issue key, branch name lookup is mandatory before asking the user for the key.
- Never invent duration. If no duration can be inferred from the current session, ask for duration only.
- Default the date to today only when the context is otherwise usable.
- Use concise, action-oriented descriptions.
- Do not claim completed implementation when context only supports investigation, planning, or review.
- Use 15-minute increments unless explicit context or CLI rules require otherwise.
- Prefer fewer entries over noisy fragmentation.
- Prefer `started_at_utc` in payloads when an exact UTC start is known. Use `started_at` for local civil placement such as `todayT09:00` when relying on `worklogs context` local slots.
- Do not create workspace temp files for ordinary single-use payloads when stdin is sufficient.

## Allowed command contract

Use only these `workledger` commands for this skill:

```text
workledger worklogs context --today --output json
workledger worklogs context --yesterday --output json
workledger worklogs context --from YYYY-MM-DD --to YYYY-MM-DD --output json
workledger worklogs context --today --issue ISSUE-123 --output json
workledger worklogs apply --file payload.json --dry --output json
workledger worklogs apply --file payload.json --output json
workledger worklogs apply --stdin --dry --output json
workledger worklogs apply --stdin --output json
```

If the current session lacks an explicit issue key, you must look up the current git branch name before asking the user for the missing key. Use a minimal targeted git command for that lookup.

Load `references/worklog-creation-contract.md` when exact payload shape, placement rules, or response format matters.

## Payload shape

```json
{
  "adds": [
    {
      "issue_key": "PROJ-123",
      "started_at": "todayT09:00",
      "duration_seconds": 3600,
      "description": "Implement session worklog creation flow"
    }
  ]
}
```

Each `adds` item requires:

- `issue_key`
- `duration_seconds`
- `description`
- exactly one of `started_at` or `started_at_utc`

## Response shape

For successful creation, keep the response compact:

```text
Created 2 local worklogs for 2026-05-15.

- PROJ-123: 1h30m, todayT09:00, implement session worklog creation flow
- PROJ-456: 45m, todayT10:30, validate apply payload handling

Dry-run passed. Applied with IDs: abc, def.
```

For an unresolved dry-run or apply failure, report:

- no canonical worklog was created, if true
- the exact CLI error or warning
- the smallest missing correction needed from the user

Before reporting a dry-run failure, first try to resolve it yourself. Common self-repairs include fixing payload shape, removing extra fields, correcting timestamp choice, adjusting to free slots, normalizing 15-minute increments, tightening descriptions, splitting entries at slot boundaries, and replacing invalid local timestamp grammar with a supported form from `worklogs context`. Prompt the user only as a last resort when the missing correction depends on information not present in the current session.

Do not include long reasoning traces. The user wanted a worklog, not a commemorative plaque for every neuron that fired.

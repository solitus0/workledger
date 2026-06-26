---
name: session-worklog-creator
description: create local workledger worklogs from the current chatgpt or coding-agent session only when the user provides an explicit duration. use when the user invokes this skill or asks to create, add, book, or log worklogs for the current session. if duration is missing, stop and ask for duration; never infer it. do not use for setup, diagnostics, totals, search, editing or deleting worklogs, remote sync, reconciliation, artifact rendering, or general workledger cli help.
---

# Session Worklog Creator

Create canonical local `workledger` worklogs from current-session evidence.

## Contract

- Treat direct invocation as permission to create local worklogs only after the duration gate and a passing dry-run.
- Require an explicit user-provided duration in the invocation/request. If absent, stop before all commands and ask for duration.
- Never infer duration from session length, timestamps, tool history, commits, branch age, calendar gaps, or `workledger` context.
- Use only current visible context: chat, agent transcript, tool output, issue keys, branch or commit snippets, and user notes.
- Do not mine unrelated history or broad repository state to fill gaps.
- If `workledger` is unavailable, stop and report that no worklog was created.
- Do not run setup, diagnostics, totals, search, edit/delete, remote sync, reconciliation, or artifact-rendering flows.
- Load `references/worklog-creation-contract.md` before running `workledger`, choosing placement, building payloads, or reporting CLI failures.

## Stop gate

Check duration first. If the user's invocation/request does not include a duration, stop without commands:

```text
Provide the duration to log, for example 45m, 1h30m, or 2h. I won't infer it from the session.
```

After duration is present, classify the remaining context:

- **Empty**: no concrete work, issue/task key, or summary. Stop without commands:

```text
I don't have enough current session context to create a worklog. Provide the issue key(s), date if not today, and a short summary of the work.
```

- **Incomplete**: some usable evidence exists, but a required field is missing. Ask only for the missing field.

Required fields:

- issue key
- user-provided duration
- summary
- date, defaulting to today only when the other fields are usable

## Extraction rules

- Never invent issue keys, durations, or completed work.
- Use only durations explicitly supplied by the user; normalizing `90 minutes` to `1h30m` is allowed.
- If no issue key is visible, run a minimal git branch lookup before asking the user.
- Use concise, action-oriented descriptions.
- Do not claim implementation when evidence only supports investigation, planning, or review.
- Preserve the user's duration. If CLI rules require a different increment or format, ask for the corrected duration unless the conversion is purely syntactic.
- Prefer fewer issue-level entries over fragmented chat-turn, command, commit, or file-level rows.

## Creation flow

1. Extract date, issue key(s), user-provided duration, and summary from current context.
2. If one entry is needed, use `workledger worklogs add`:
   - use `--fit` with a date-window flag when exact start is not required
   - use `--started` or `--started-utc` only when an exact start is supported
   - dry-run first; apply immediately after a clean dry-run
3. If multiple entries are needed:
   - require explicit per-entry durations, or one explicit total duration plus the user's explicit split instruction
   - stop and ask for the missing duration/split detail if any entry duration would need inference
   - run `workledger worklogs context` for the date window
   - bundle evidence by issue
   - build one JSON object with top-level `adds`
   - validate with `workledger worklogs apply --stdin --dry --output json`
   - apply with `workledger worklogs apply --stdin --output json` after a clean dry-run
4. If dry-run fails, repair what the context supports and rerun dry-run. Convert duration only when the user supplied it explicitly and the conversion is purely syntactic. Ask before changing increments, totals, or allocation.
5. Report created entries, IDs, and CLI warnings. If nothing was created, say so.

## Output style

Keep the final response compact:

```text
Created 2 local worklogs for 2026-05-15.

- PROJ-123: 1h30m, todayT09:00, implement session worklog creation flow
- PROJ-456: 45m, todayT10:30, validate apply payload handling

Dry-run passed. Applied with IDs: abc, def.
```

For failures, include only:

- whether a worklog was created
- the exact CLI error or warning
- the smallest correction needed

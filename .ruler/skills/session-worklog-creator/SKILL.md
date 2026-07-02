---
name: session-worklog-creator
description: Create local workledger worklogs from the current ChatGPT or coding-agent session after the user provides explicit duration. Duration is the only mandatory user input. Use the current branch to derive the issue key when needed; ask for issue only when no unambiguous key can be found. Never require started time, log count, date/window, description, or summary unless the user provides those details and they are conflicting or unusable.
---

# Session Worklog Creator

Create canonical local `workledger` worklogs from evidence visible in the current session with minimal user input.

## Boundary

- Direct invocation, or an explicit request to create/book/log a worklog, is permission to apply local worklogs after duration is explicit, an issue key is available, and dry-run succeeds.
- Duration is the only mandatory user-provided field. Never infer it.
- Use only current-session evidence: the request, chat or agent transcript, visible tool output, issue keys, branch/commit/PR snippets, and files or notes uploaded in this conversation.
- Do not search unrelated conversation history, broad repository state, remote trackers, totals, or prior worklogs to fill gaps.
- Do not run setup, diagnostics, totals, search, edit/delete, remote sync, reconciliation, or artifact-rendering flows.
- Load `references/worklog-creation-contract.md` before choosing placement, building payloads, running `workledger`, or explaining CLI failures.
- If `workledger` is unavailable, stop and report that no worklog was created.

## Gate order

1. **Duration only.** If the request lacks explicit duration, run no commands and ask only:

   ```text
   Provide the duration to log, for example 45m, 1h30m, or 2h. I won't infer it from the session.
   ```

2. **Issue key next.** After duration is known, use a visible issue key from the current session. If none is visible, run only `git branch --show-current` and use an unambiguous Jira-style key from the current branch, such as `PROJ-123`. Ask only for the issue key if neither source works.
3. **Defaults are not questions.** Do not ask for started time, date/window, log count, description, summary, or split just because they are absent. Default to one log, `--fill --today`, and no description unless the user provided one or the CLI requires one.
4. **Optional fields stay optional.** Use user-provided started time, date/window, log count, split, and description when present. Ask only when the user provided an optional field but it conflicts, is malformed, or cannot be applied without changing intent.

Never invent duration, issue keys, or completed work. Normalize explicit duration syntax only when meaning is unchanged, such as `90 minutes` to `1h30m`.

## Creation flow

1. Extract explicit duration first.
2. Extract the issue key from current-session evidence, or derive it from `git branch --show-current`; ask only for the issue key if both fail.
3. Extract optional user-provided started time, date/window, log count, description, and split details. Missing optional fields use defaults rather than prompts.
4. Follow `references/worklog-creation-contract.md` for allowed commands, placement, payload shape, and failure handling.
5. Use `worklogs add` for the default single-log path:
   - use `--fill --today` when no exact start or date/window is supplied
   - use a supplied date/window with `--fill` when the user provided one but no exact start
   - use `--fit` only when the user requests one continuous placement or the session clearly calls for a single earliest free slot
   - use `--started` or `--started-utc` only when the user supplies the exact start
   - include `--description` only when the user supplied one or dry-run proves the CLI requires it
6. Use `worklogs apply` with one JSON payload when the user supplies multiple issues, per-entry durations, a log count, explicit split instructions, or payload-shaped evidence. If the user gives one total duration with multiple issues or a log count but no split, divide the explicit total evenly and preserve the exact total.
7. Dry-run first with `--output json`; apply immediately only after a clean dry-run.
8. If dry-run fails, repair only evidence-supported command shape, payload shape, timestamp grammar/exclusivity, slot placement, optional-description handling, or allocation math that preserves the explicit total. Ask only when the smallest safe correction needs a missing duration, a missing issue key, or a change to user-provided intent.
9. If apply fails, do not claim success. Retry once only for a command-shape or transient persistence failure that preserves intent.

## Description and response style

Descriptions are optional. When included, keep them concise, action-oriented, and evidence-matched. Use neutral wording such as `current session work` when the CLI requires a description but the session lacks enough detail for something more specific.

For success, report created count, date/window, issue, duration, placement time when available, description only if used, IDs, and CLI warnings. For failure, report whether anything was created, the exact CLI error or warning, and the smallest correction needed.

```text
Created 1 local worklog for today.

- PROJ-123: 1h30m, filled earliest available slot

Dry-run passed. Applied with ID: abc.
```

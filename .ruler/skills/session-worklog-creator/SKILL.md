---
name: session-worklog-creator
description: Create local workledger worklogs from the current ChatGPT or coding-agent session after the user provides explicit duration. Duration is the only mandatory user input. Use an unambiguous session or branch key, then a clear local metadata title match when needed; ask for issue only when selection remains unresolved. Generate a descriptive worklog from evidence of work performed in the session; create nothing for an empty thread. Never ask the user for a description, even when one is missing or unusable. Never require started time, log count, date/window, or summary unless supplied details conflict or are unusable.
---

# Session Worklog Creator

Create canonical local `workledger` worklogs from evidence visible in the current session with minimal user input.

## Boundary

- Direct invocation, or an explicit request to create/book/log a worklog, is permission to apply local worklogs after duration is explicit, an issue key is available, and dry-run succeeds.
- Duration is the only mandatory user-provided field. Never infer it.
- Use only current-session evidence: the request, chat or agent transcript, visible tool output, issue keys, branch/commit/PR snippets, and files or notes uploaded in this conversation.
- Do not search unrelated conversation history, broad repository state, remote trackers, totals, or prior worklogs to fill gaps.
- Do not run setup, diagnostics, totals, worklog search, edit/delete, remote sync, reconciliation, or artifact-rendering flows. Local `issues list` is allowed only for unresolved issue attribution after the work-evidence and explicit-duration gates.
- Never supply `--overtime` unless the user explicitly authorizes overtime placement in the current request or after a blocked dry-run.
- Load `references/worklog-creation-contract.md` before choosing placement, building payloads, running `workledger`, or explaining CLI failures.
- After the work-evidence and explicit-duration gates, follow the reference's CLI readiness check before any `workledger` domain command. If the CLI is unavailable or incompatible, stop and report that no worklog was created; do not install or upgrade it automatically.

## Gate order

0. **Work evidence first.** If the session contains no evidence of work performed, stop without commands or worklogs and explain that there is no work to log. A worklog request, duration, issue key, branch name, or proposed task alone is not evidence of work. Actual analysis, review, investigation, implementation, validation, and unsuccessful attempts count; do not claim an outcome the evidence does not support.
1. **Duration only.** If the request lacks explicit duration, run no commands and ask only:

   ```text
   Provide the duration to log, for example 45m, 1h30m, or 2h. I won't infer it from the session.
   ```

2. **Issue key next.** Explicit user issue selection wins. Otherwise use only an unambiguous key in current-session evidence; if unresolved, run `git branch --show-current` once and use an unambiguous Jira-style key from that branch. If still unresolved, run local `workledger issues list --output json`, optionally narrowed by an evidence-supported `--search` title substring or `--issue-prefix`. Select a key only when the session's work evidence clearly matches one title. Similar, broad, conflicting, or absent title matches require asking for the issue key. Estimates, remaining budget, and key recency never establish attribution, and estimates never replace explicit duration.
3. **Defaults are not questions.** Do not ask for started time, date/window, log count, description, summary, or split just because they are absent. Default to one issue and activity with `--fill --today` and a descriptive entry generated from session evidence. Fill may create multiple worklog fragments; report the actual returned count. Never ask for a description under any circumstances.
4. **Optional fields stay optional.** Use user-provided started time, date/window, log count, and split when present. Ask only when those supplied fields conflict, are malformed, or cannot be applied without changing intent. Use a supplied description when it is descriptive and evidence-matched; otherwise generate one from the session without asking the user.

Never invent duration, issue keys, or completed work. Normalize explicit duration syntax, then round the supplied total to the nearest minute with 30-second ties upward, following the canonical worklog contract. Starts also normalize to minute boundaries. Preserve the normalized total during placement and report changes from the supplied timing.

## Creation flow

1. Confirm there is evidence of work performed, extract explicit duration, then check CLI readiness as described in the reference.
2. Resolve the issue in the gate order above: explicit selection, unambiguous session or branch key, then a clear single local metadata title match; ask for the key if unresolved.
3. Extract optional user-provided started time, date/window, log count, and split details. Generate a concise description from the work evidence, or use a supplied description that meets the same standard. Missing optional fields use defaults rather than prompts.
4. Follow `references/worklog-creation-contract.md` for allowed commands, placement, payload shape, and failure handling.
5. Use `worklogs add` for the default one-issue activity:
   - use `--fill --today` when no exact start or date/window is supplied
   - use a supplied date/window with `--fill` when the user provided one but no exact start
   - use `--fit` only when the user requests one continuous placement or the session clearly calls for a single earliest free slot
   - use `--started` or `--started-utc` only when the user supplies the exact start
   - include the evidence-based `--description` in the first dry-run and the apply command
6. Use `worklogs apply` with one JSON payload when the user supplies multiple issues, per-entry durations, a log count, explicit split instructions, or payload-shaped evidence. For one total with multiple issues or a log count but no split, round the total once, divide its whole minutes evenly, and assign remainder minutes one at a time in input order. Preserve the normalized total; every fragment must satisfy the effective minimum.
7. Dry-run first with `--output json`; read the command-specific response shape from the reference and verify the normalized timing and total. Apply immediately only after a clean dry-run, then report the actual saved records and IDs.
8. If dry-run fails, repair only evidence-supported command shape, payload shape, timestamp grammar/exclusivity, slot placement, description normalization, or allocation math that preserves the normalized total. When the CLI says placement would require `--overtime`, report that nothing was created and ask permission before retrying with `--overtime`. Otherwise ask only when the smallest safe correction needs a missing duration, a missing issue key, or a change to timing, issue, or allocation intent. Repair description problems from session evidence; never ask the user to provide, clarify, or approve a description.
9. If apply fails, do not claim success. Retry once only for a command-shape or transient persistence failure that preserves intent.

## Description and response style

Every worklog must have a descriptive, evidence-matched entry. Never ask the user for a description, including when a supplied one is blank, vague, conflicting, or rejected by validation. Make the best effort to name the action and its subject from whatever work evidence is available; keep outcome claims within that evidence. Sparse evidence calls for a narrower description, not a generic placeholder. See the description rules and examples in `references/worklog-creation-contract.md`. If there is no evidence of work performed, create nothing.

For success, report created count, date/window, issue, duration, placement time when available, description, IDs, and CLI warnings. For failure, report whether anything was created, the exact CLI error or warning, and the smallest correction needed.

```text
Created 1 local worklog for today.

- PROJ-123: 1h30m, filled earliest available slot

Dry-run passed. Applied with ID: abc.
```

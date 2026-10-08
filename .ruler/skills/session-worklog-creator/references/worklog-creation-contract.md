# Worklog creation contract

Exact operational contract for creating local `workledger` worklogs from the current session.

## Evidence and gates

Use only current-session evidence: user request, chat or agent transcript, visible tool output, visible branch/commit/PR/issue/file context, and notes or uploads in this conversation.

Do not use unrelated conversation search, broad repository mining, remote tracker lookup, prior worklog lookup, or weak issue-key inference. A local metadata title lookup is permitted only for unresolved attribution after the work-evidence and explicit-duration gates.

Minimal-input rule: **explicit duration is the only always-required user input**. After duration is known, ask for only the issue key, and only when explicit selection, an unambiguous session or branch key, and a clear single local metadata title match cannot resolve attribution.

Required gate order:

0. **Work evidence.** Before commands or requests for missing fields, verify that the session contains evidence of work performed. An empty thread or one containing only a worklog request, duration, issue key, branch name, or proposed task produces no worklogs. Explain that no work evidence is available; do not ask for a description. Actual analysis, review, investigation, implementation, validation, and unsuccessful attempts qualify without requiring a completed implementation.
1. **Duration.** If the invocation/request lacks explicit duration, stop before commands and ask for duration only.
2. **No duration inference.** Never derive duration from elapsed time, timestamps, tool history, commit count, branch age, calendar gaps, or `workledger` context.
3. **Issue key.** Explicit user selection wins. Otherwise use an unambiguous key in current-session evidence, then run `git branch --show-current` once if unresolved. If neither identifies one key, inspect local `workledger issue-metadata list --output json`, optionally filtering with an evidence-supported `--search` title substring or `--issue-prefix`. Choose only one clearly matching title supported by the session's work evidence; ask for the issue key for broad, ambiguous, conflicting, absent, or unavailable matches. Issue estimates, remaining budget, and recency never establish attribution or duration.
4. **Optional user input.** Started time, date/window, log count, split, and description need not be supplied by the user. Never ask for a description, even when supplied text is unusable; generate or repair it from session evidence.
5. **Default placement.** If no start or date/window is supplied, create one worklog with `--fill --today`. If a date/window is supplied without a start, use `--fill DATE_WINDOW`.
6. **Default count.** If no log count, multiple issues, per-entry durations, split, or payload-shaped evidence is supplied, create one log.
7. **Description.** Include a descriptive, evidence-matched description from the first dry-run. Use supplied text if it meets that standard; otherwise generate it from work performed in the session. Do not omit it, use a generic placeholder, or prompt the user for it.

Normalize explicit duration syntax, then round the supplied total once to the nearest minute, with 30-second ties upward. Reject a total that rounds to zero. Preserve this normalized total exactly during allocation. UTC starts also round to minute boundaries; dry-run output must show the resulting timing, and success must report any change from the supplied timing.

## Allowed commands

Allowed command families only:

```text
git branch --show-current
workledger issue-metadata list [--search TITLE_TEXT] [--issue-prefix PREFIX] --output json

workledger worklogs add --issue KEY (--fill DATE_WINDOW | --fit DATE_WINDOW) [--overtime] --duration DURATION --description "..." --dry --output json
workledger worklogs add --issue KEY (--fill DATE_WINDOW | --fit DATE_WINDOW) [--overtime] --duration DURATION --description "..." --output json
workledger worklogs add --issue KEY (--started LOCAL_TIME | --started-utc UTC_TIME) --duration DURATION --description "..." --dry --output json
workledger worklogs add --issue KEY (--started LOCAL_TIME | --started-utc UTC_TIME) --duration DURATION --description "..." --output json

workledger worklogs context DATE_WINDOW [--issue KEY] --output json

workledger worklogs apply --stdin --dry --output json
workledger worklogs apply --stdin --output json
workledger worklogs apply --file payload.json --dry --output json
workledger worklogs apply --file payload.json --output json
```

Prefer stdin for `apply`; use files only for debugging or when requested.

## Date and timestamp grammar

`DATE_WINDOW` is one supported selector: `--today`, `--yesterday`, `--tomorrow`, `--mon` through `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, or `--from DATE --to DATE`. `--week-offset N` is valid only with a weekday selector.

`DATE` may be `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, `+Nd`, or `-Nd`.

`LOCAL_TIME` examples: `2026-05-14T09:00`, `todayT09:00`, `yesterdayT09:00`, `tomorrowT09:00`, `+2dT09:00`, `-3dT09:00`.

`UTC_TIME` must be RFC3339, for example `2026-05-15T07:00:00Z`.

## Single-entry path

Use `worklogs add` directly for the default one-issue add after duration and issue key are available.

- Unknown exact start and no date/window: use `--fill --today`.
- Supplied date/window and unknown exact start: use `--fill DATE_WINDOW`.
- Continuous placement requested or clearly preferable: use `--fit DATE_WINDOW`.
- Use `--overtime` only when the user explicitly authorizes automatic placement starting at or after the workday end.
- User-supplied local start: use `--started LOCAL_TIME`.
- User-supplied UTC start: use `--started-utc UTC_TIME`.
- Do not run `worklogs context` before a direct automatic-placement add.
- Include the evidence-based `--description` from the first dry-run.
- Dry-run first; apply only after the dry-run succeeds.

## Multi-entry path

Use `worklogs apply` only when the user supplies multiple issues, per-entry durations, a log count, explicit split instructions, or payload-shaped evidence.

Keep input requirements low:

1. One explicit total duration is enough for the payload total.
2. If multiple issues or a log count are supplied without per-entry durations, round the total once, divide its whole minutes evenly, and assign remainder minutes one at a time in input order. Preserve the normalized total and satisfy the effective fragment minimum.
3. Do not ask for log count, split, started time, or date/window when absent. Never ask for descriptions. Generate an evidence-based description for each entry; splitting one activity across slots may reuse its description without inventing separate activities.
4. Use `worklogs context DATE_WINDOW --output json` only when placement must account for existing slots or a multi-entry payload needs exact starts.
5. Bundle by issue when issues are supplied; otherwise use one issue derived from current evidence or branch.
6. Prefer primary workday free slots before overtime when exact placement is needed.
7. Validate one payload with `workledger worklogs apply --stdin --dry --output json`.
8. Apply the same payload with `workledger worklogs apply --stdin --output json` only after validation succeeds.

## Apply payload

```json
{
  "adds": [
    {
      "issue_key": "PROJ-123",
      "started_at": "todayT09:00",
      "duration_seconds": 3600,
      "description": "validate worklog apply payload handling"
    }
  ]
}
```

Each add requires `issue_key`, positive integer `duration_seconds` in multiples of 60 from the normalized explicit duration, and exactly one timestamp field: `started_at` for local grammar or `started_at_utc` for UTC RFC3339. Every add must include a descriptive, evidence-matched `description`, supplied by the user or generated from current-session work evidence. Do not include extra fields unless the CLI requires them.

## Descriptions

Never ask the user to provide, clarify, or approve a description. A description is required in every generated worklog; supplying one is never a user prerequisite. Do not ask for a summary or work details as a substitute for requesting a description.

Make the best effort to summarize the actual work as an action plus its subject, adding an outcome or scope only when supported. Use specific session details rather than generic placeholders. Sparse evidence still supports a narrow description: a reviewed diff can yield `review worklog duration normalization`; a failed command investigation can yield `investigate worklog dry-run validation failure`. Do not turn an attempted fix into a claim that the problem was resolved.

Use a user-provided description when it is descriptive and evidence-matched. Replace blank, vague, conflicting, or unsupported text with an evidence-based description without asking. If validation rejects the description, repair its wording or normalization from the same evidence.

Never use placeholders such as `current session work`, `session work`, `work done`, or `chatgpt session`, including when the user supplies them. If there is no evidence of work performed, stop without creating worklogs; a placeholder or description prompt cannot substitute for evidence.

Good evidence-supported descriptions: `implement session worklog creation flow`, `validate worklog apply payload handling`, `review skill invocation scope`, `investigate CLI dry-run failure`.

Bad unsupported claims: `implemented payment API`, `fixed deployment outage`, or `completed refactor` when the session shows no such work or outcome.

## Failure handling

Repair dry-run failures only when current evidence supports the fix: command shape, payload shape, description normalization, extra fields, known missing fields, timestamp grammar/exclusivity, slot collisions, boundary splits, or allocation math that preserves the normalized total.

If automatic placement fails with `use --overtime to allow placement starting at or after day_end`, report that nothing was created and ask whether to retry with `--overtime`. Do not infer authorization from the original worklog-creation request.

Ask the user only when the fix needs unavailable mandatory information or would change user-provided intent: missing/invalid duration, missing issue key after branch fallback, incompatible timing or split fields, duration change beyond canonical minute rounding, allocation change beyond even whole-minute split, date conflict, or conflict priority.

Description failures are never a reason to ask the user for a description or its approval. Generate or repair the text from available work evidence; if no work evidence exists, create nothing.

If dry-run succeeds but apply fails, do not claim success. Retry once only for command-shape or transient persistence failures that preserve intent. For storage or SQLite permission errors, say validation passed but local persistence failed.

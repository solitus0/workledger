# Worklog creation contract

Exact operational contract for creating local `workledger` worklogs from the current session.

## Evidence and gates

Use only current-session evidence: user request, chat or agent transcript, visible tool output, visible branch/commit/PR/issue/file context, and notes or uploads in this conversation.

Do not use unrelated conversation search, broad repository mining, remote tracker lookup, prior worklog lookup, or weak issue-key inference.

Minimal-input rule: **explicit duration is the only always-required user input**. After duration is known, ask for only the issue key, and only when no unambiguous Jira-style key is visible in current-session evidence or derivable from the current git branch.

Required gate order:

1. **Duration.** If the invocation/request lacks explicit duration, stop before commands and ask for duration only.
2. **No duration inference.** Never derive duration from elapsed time, timestamps, tool history, commit count, branch age, calendar gaps, or `workledger` context.
3. **Issue key.** If no issue key is visible after duration is known, run `git branch --show-current` once. Use the result only when it contains an unambiguous Jira-style key, such as `PROJ-123`. If no key can be derived, ask only for the issue key.
4. **Optional fields.** Started time, date/window, log count, split, and description are optional. Do not ask for them just because they are absent.
5. **Default placement.** If no start or date/window is supplied, create one worklog with `--fill --today`. If a date/window is supplied without a start, use `--fill DATE_WINDOW`.
6. **Default count.** If no log count, multiple issues, per-entry durations, split, or payload-shaped evidence is supplied, create one log.
7. **Default description.** Omit description when the CLI permits it. If dry-run proves the CLI requires a description, use the user-provided description when present; otherwise use `current session work`.

Normalize duration only when meaning is unchanged, such as `90 minutes` to `1h30m` or `5400 seconds` to `1h30m`. Preserve total duration exactly.

## Allowed commands

Allowed command families only:

```text
git branch --show-current

workledger worklogs add --issue KEY (--fill DATE_WINDOW | --fit DATE_WINDOW) [--overtime] --duration DURATION [--description "..."] --dry --output json
workledger worklogs add --issue KEY (--fill DATE_WINDOW | --fit DATE_WINDOW) [--overtime] --duration DURATION [--description "..."] --output json
workledger worklogs add --issue KEY (--started LOCAL_TIME | --started-utc UTC_TIME) --duration DURATION [--description "..."] --dry --output json
workledger worklogs add --issue KEY (--started LOCAL_TIME | --started-utc UTC_TIME) --duration DURATION [--description "..."] --output json

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
- Omit `--description` unless supplied by the user or required by validation.
- Dry-run first; apply only after the dry-run succeeds.

## Multi-entry path

Use `worklogs apply` only when the user supplies multiple issues, per-entry durations, a log count, explicit split instructions, or payload-shaped evidence.

Keep input requirements low:

1. One explicit total duration is enough for the payload total.
2. If multiple issues or a log count are supplied without per-entry durations, split the explicit total evenly and preserve the exact total; put any indivisible remainder on the final entry.
3. Do not ask for log count, split, started time, date/window, or descriptions when absent.
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
      "description": "current session work"
    }
  ]
}
```

Each add requires `issue_key`, positive whole-number `duration_seconds` from explicit user duration, and exactly one timestamp field: `started_at` for local grammar or `started_at_utc` for UTC RFC3339. Include `description` only when supplied by the user or required by CLI validation. Do not include extra fields unless the CLI requires them.

## Descriptions

Descriptions are optional and must not become a user prompt by default.

Good evidence-supported descriptions: `implement session worklog creation flow`, `validate worklog apply payload handling`, `review skill invocation scope`, `investigate CLI dry-run failure`.

Safe fallback when the CLI requires a description but the session has little evidence: `current session work`.

Bad unsupported specificity: `implemented payment API`, `fixed deployment outage`, `completed refactor`, `chatgpt session`.

## Failure handling

Repair dry-run failures only when current evidence supports the fix: command shape, payload shape, optional-description handling, extra fields, known missing fields, timestamp grammar/exclusivity, slot collisions, boundary splits, or allocation math that preserves the explicit total.

If automatic placement fails with `use --overtime to allow placement starting at or after day_end`, report that nothing was created and ask whether to retry with `--overtime`. Do not infer authorization from the original worklog-creation request.

Ask the user only when the fix needs unavailable mandatory information or would change user-provided intent: missing/invalid duration, missing issue key after branch fallback, incompatible optional fields, non-syntactic duration change, allocation change beyond even split, date conflict, or conflict priority.

If dry-run succeeds but apply fails, do not claim success. Retry once only for command-shape or transient persistence failures that preserve intent. For storage or SQLite permission errors, say validation passed but local persistence failed.

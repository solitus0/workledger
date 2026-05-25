# Worklog creation contract

Exact contract for local `workledger` worklog creation.

## Evidence

Use only current-session evidence:

- user request
- chat, agent transcript, or session summary
- visible tool output
- visible branch, commit, PR, issue, or file context
- notes/uploads in this conversation

No unrelated conversation search. No weak issue-key inference.

## Missing data

- Duration gate comes first: if the user invocation/request lacks explicit duration, stop before commands and ask for duration only.
- Never derive duration from elapsed session time, timestamps, tool history, commit count, calendar gaps, or `workledger` context.
- Normalize user-provided duration formats when safe, for example `90 minutes` to `1h30m` or `5400` seconds.
- Empty context after duration is known: request issue key(s), date if not today, and summary.
- Incomplete context: ask only for the missing required field.
- Date may default to today after issue, duration, and summary are known.
- Without an explicit issue key, run `git branch --show-current` before asking.
- Never invent issue keys, durations, or completed work.

## Command scope

Allowed command families only:

```text
git branch --show-current

workledger worklogs add --issue KEY --snap DATE_WINDOW --duration DURATION --description "..." --dry --output json
workledger worklogs add --issue KEY --snap DATE_WINDOW --duration DURATION --description "..." --output json
workledger worklogs add --issue KEY --started LOCAL_TIME --duration DURATION --description "..." --dry --output json
workledger worklogs add --issue KEY --started LOCAL_TIME --duration DURATION --description "..." --output json
workledger worklogs add --issue KEY --started-utc UTC_TIME --duration DURATION --description "..." --dry --output json
workledger worklogs add --issue KEY --started-utc UTC_TIME --duration DURATION --description "..." --output json

workledger worklogs context DATE_WINDOW --output json
workledger worklogs context DATE_WINDOW --issue KEY --output json

workledger worklogs apply --stdin --dry --output json
workledger worklogs apply --stdin --output json
workledger worklogs apply --file payload.json --dry --output json
workledger worklogs apply --file payload.json --output json
```

`DATE_WINDOW` is a supported date selector such as `--today`, `--yesterday`, or `--from YYYY-MM-DD --to YYYY-MM-DD`. Prefer stdin for apply. Use files only for debugging or when requested.

## Single-entry path

Use `worklogs add` directly only after the user has provided duration. Dry-run first; apply after success.

- Unknown exact start: use `--snap DATE_WINDOW`.
- Known local start: use `--started`, for example `todayT09:00`.
- Known UTC start: use `--started-utc`, for example `2026-05-15T07:00:00Z`.
- Do not run `context` before a `--snap` add.

## Multi-entry path

1. Require user-provided duration coverage before commands:
   - use explicit per-entry durations when supplied
   - use one explicit total duration only when the user also provides an explicit split rule or grants equal split
   - stop and ask when any duration would need inference
2. Run `worklogs context DATE_WINDOW --output json` only after duration coverage is clear.
3. Bundle evidence by issue, not by chat turn, command, commit, or file.
4. Place entries in free slots, preferring primary workday slots before overtime.
5. Split only for user-approved duration splits, slot boundaries, or genuinely distinct issue work.
6. Preserve the user's duration. If CLI rules require a different increment or format, ask for the corrected duration unless the conversion is purely syntactic.
7. Validate with `apply --stdin --dry`; apply after success.

## Apply payload

Use one JSON object with top-level `adds`:

```json
{
  "adds": [
    {
      "issue_key": "PROJ-123",
      "started_at": "todayT09:00",
      "duration_seconds": 3600,
      "description": "implement session worklog creation flow"
    }
  ]
}
```

Each add requires:

- `issue_key`: Jira-style key, for example `PROJ-123`
- `duration_seconds`: positive whole seconds from explicit user-provided duration only
- `description`: concise single-line text; omit issue key unless requested
- exactly one timestamp: `started_at` for local grammar or `started_at_utc` for UTC RFC3339

## Description style

Good: `implement session worklog creation flow`, `validate worklog apply payload handling`, `review skill invocation scope`, `investigate cli dry-run failure`.

Bad: `work on task`, `misc updates`, `chatgpt session`.

## Failure handling

Treat dry-run failures as repairable when context supports the fix. Repair payload shape, extra fields, known missing fields, timestamp grammar/exclusivity, slot collisions, boundary splits, or weak descriptions. Convert duration only when the user supplied it explicitly and the conversion is purely syntactic, such as `90 minutes` to `1h30m`. Rerun dry-run after each evidence-based repair.

Ask the user when the fix needs unavailable information or any duration change beyond syntax: issue key, user-provided duration, duration increment, allocation, date, summary, split rule, or conflict priority.

If dry-run succeeds but apply fails, do not claim success. Retry once only for command-shape or transient persistence failures that preserve intent. For storage or SQLite permission errors, say validation passed but local persistence failed.

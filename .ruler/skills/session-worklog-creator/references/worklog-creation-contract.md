# Worklog creation contract

Use this reference only while creating local `workledger` worklogs from current session context.

## Evidence extraction

Evidence must come from the current visible session:

1. Explicit user instructions in the current request.
2. Current coding-agent transcript or session summary.
3. Tool output already present in the session.
4. Branch names, commit messages, PR titles, issue keys, or file paths already visible in the session.
5. User-provided notes or uploaded files in the same conversation.

Do not search unrelated historical conversations. Do not infer issue keys from weak associations.

## Empty versus incomplete context

Empty context means there is no concrete work evidence beyond the invocation. Stop immediately and request full worklog data.

Incomplete context means there is some usable evidence, but a required field is missing. Ask only for the missing field:

- missing issue key: ask for issue key
- missing duration: ask for duration
- missing summary: ask what work should be logged

Default the date to today when the issue, summary, and duration are known or inferable.

## Placement workflow

Run context inspection before creating rows:

```text
workledger worklogs context --today --output json
workledger worklogs context --from 2026-05-15 --to 2026-05-15 --output json
```

Use the returned free slots and collisions to place candidate entries. Prefer primary workday slots. Use overtime only when the context explicitly supports it.

## Allocation rules

Apply rules in this order:

1. Exact user/session constraints.
2. Existing free slots from `worklogs context`.
3. Evidence strength per issue.
4. Equal split fallback.

Use 15-minute increments. Match the supported total duration exactly. Do not create tiny rows just because many files or messages are mentioned.

## Description rules

Descriptions should be concise and concrete:

- `implement session worklog creation flow`
- `validate worklog apply payload handling`
- `review and narrow skill invocation scope`
- `investigate cli dry-run failure`

Avoid vague descriptions:

- `work on task`
- `misc updates`
- `chatgpt session`

Do not include issue keys in descriptions unless the user explicitly asks.

## Payload validation

Payloads must be one JSON object with top-level `adds`:

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

- `issue_key`: jira-style key such as `PROJ-123`
- `duration_seconds`: positive whole seconds
- `description`: non-empty single-line text
- exactly one timestamp field:
  - `started_at` for local workledger grammar such as `todayT09:00`
  - `started_at_utc` for explicit utc rfc3339 such as `2026-05-15T07:00:00Z`

Authoritative dry-run and validation:

```text
workledger worklogs apply --file payload.json --dry --output json
```

Apply after successful dry-run:

```text
workledger worklogs apply --file payload.json --output json
```

## Failure handling

Treat dry-run failures as repairable by default. Do not immediately ask the user or stop at the first CLI error.

When dry-run validation fails:

1. Read the CLI error and identify the smallest payload or placement change likely to fix it.
2. Repair anything derivable from current context, including JSON shape, required fields, timestamp field exclusivity, local timestamp grammar, duration increment rounding, slot placement, collision avoidance, description length/content, and unnecessary extra fields.
3. Re-run `workledger worklogs apply --file payload.json --dry --output json` or the equivalent stdin command.
4. Repeat only while each attempt has a clear, evidence-based correction. Do not loop blindly.
5. Ask the user only when the remaining failure requires information that is not available in the current session, such as the correct issue key, exact duration, date choice, or which conflicting worklog should take priority.

If dry-run still cannot be repaired, no worklog was created. Report the exact CLI error or warning and the smallest missing correction needed from the user.

If dry-run succeeds but apply fails, do not claim success. If the failure is a transient or command-shape problem that can be fixed without changing user intent, repair and retry once. Otherwise report the CLI error exactly enough for the user to act.

If a storage path or SQLite permission issue appears, state that the payload passed validation but local persistence failed.

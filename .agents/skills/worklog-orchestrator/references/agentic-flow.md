# Coding-agent worklog flow

Use this reference when turning session evidence into canonical local worklogs or proposing multi-row changes.

## Agent role

The agent interprets evidence and drafts commands. `workledger` validates and persists state.

The agent may decide:

- which issue keys are supported by evidence
- how total time should be allocated across issues
- where candidate entries fit in available slots
- how to phrase concise descriptions

The CLI decides:

- timestamp parsing and timezone resolution
- minimum duration validation
- duplicate and overlap validation
- selector expansion
- final payload acceptance
- SQLite persistence

## Evidence precedence

Use evidence in this order:

1. Explicit user instructions in the current request.
2. Existing session artifact files.
3. Current live coding-session summary.
4. User-provided notes or uploaded files.
5. Git branch names, commit messages, and diffs.
6. Jira or issue metadata.
7. Raw conversation mining as a last resort.

Do not auto-include low-confidence historical matches. When multiple sessions support the same issue, merge them into one issue-level evidence bundle before drafting.

## Drafting sequence

1. Resolve the selected date window. Prefer explicit user dates over assumptions.
2. Resolve issue keys using explicit instructions first, then artifact metadata, Git, branch context, and notes.
3. Run `workledger worklogs context <window> --output json` when placement, quotas, collisions, or existing rows matter.
4. Read `summary.until_quota_seconds` as the delta against the configured daily quota. Do not confuse it with available free-slot capacity.
5. Allocate the requested or inferred duration across issue-level bundles.
6. Place entries chronologically inside CLI-reported free slots when possible.
7. Draft one raw `worklogs apply` payload for multi-row creation.
8. Dry-run with `workledger worklogs apply --dry --output json`.
9. Apply only after the dry-run succeeds and the user intended a write.

## Allocation heuristics

Apply in this order:

1. User hard constraints: fixed issue, fixed time, min/max duration, exact entry count.
2. CLI placement constraints from `worklogs context`.
3. Jira estimate metadata as weights or soft caps.
4. Session and Git evidence strength.
5. Equal split fallback.

When some issues have estimates and others do not, assign weight `1` to unestimated issues and redistribute remaining duration when soft caps are reached.

## Entry count heuristic

Use fewer rows for homogeneous work and more rows for genuinely distinct issues or themes.

| Total duration | Default entry count |
| --- | --- |
| Up to 1h | 1 |
| 1h to 2h | 1 to 2 |
| 2h to 4h | 2 to 3 |
| 4h to 6h | 3 to 5 |
| 6h to 8h | 4 to 6 |
| 8h to 10h | 5 to 7 |
| Over 10h | 6 to 8 |

Use 15-minute increments by default. Match total duration exactly. Avoid artificial fragmentation.

## Placement rules

- Prefer primary workday slots reported by `worklogs context`.
- Respect lunch and day-start/day-end settings from the context response.
- Use overtime only when primary slots are insufficient and the user has requested or accepted it.
- Place entries chronologically and without artificial gaps.
- Split at slot boundaries when needed to preserve continuity.
- Prefer `started_at_utc` in generated payloads for deterministic automation.

## Description rules

- Use concise, action-oriented descriptions.
- Prefer concrete completed work over vague activity labels.
- Do not include Jira keys in descriptions unless the user asks.
- Do not claim work was completed when evidence only supports investigation or planning.
- Normalize descriptions to single-line text because the CLI will collapse internal whitespace.

## Response shape

For candidate drafted worklogs, return:

1. Scope and evidence used.
2. Draft strategy.
3. Candidate summary or payload location.
4. Validation result.
5. Exact next command or command already executed.
6. Warnings or assumptions.

For mutations already executed, include changed IDs or counts and any validation warnings. For dry-runs, be explicit that no canonical state changed.

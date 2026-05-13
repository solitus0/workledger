# Drafting heuristics for canonical local worklogs

These rules guide Codex when composing candidate local worklogs from evidence. They are heuristics, not product truth. If local CRUD validation disagrees, the documented `workledger worklogs *` rules win.

## Allocation precedence

Apply in this order:
1. user hard constraints: fixed, min, max, explicit issue-count constraints
2. Jira estimates as weights plus soft caps
3. merged session evidence and Git evidence
4. equal split fallback

## Partial estimate handling

If some issues have estimates and some do not:
- use available estimates as weights
- assign weight `1` to issues without estimates
- treat estimates as soft caps unless user constraints override them
- redistribute remainder when a cap is reached

## Count heuristic

When an explicit count is absent, choose a count from duration and refine it with evidence.

Baseline guidance:
- `<= 1h`: 1 entry
- `1-2h`: 1-2 entries
- `2-4h`: 2-3 entries
- `4-6h`: 3-5 entries
- `6-8h`: 4-6 entries
- `8-10h`: 5-7 entries
- `>10h`: 6-8 entries

Increase count for more distinct issues or themes. Decrease count for homogeneous work. Avoid artificial fragmentation. Default minimum duration is 15 minutes unless continuity at a slot boundary requires a smaller fragment.

## Workday and slotting heuristic

- Default primary workday is `08:00-17:00`.
- Default lunch is `12:00-13:00`.
- Fill primary slots first.
- Use evening overtime before morning overtime when extra capacity is needed.
- Ask before using overtime when total requested duration exceeds the primary workday.

## Placement and rounding

- Place worklogs chronologically and without artificial gaps.
- Worklogs may split at slot boundaries to preserve continuity.
- Use 15-minute increments by default.
- Match total duration exactly.
- Minimize irregular fragments.

## Description generation

Use this precedence:
1. merged session summaries
2. Git evidence
3. Jira issue context
4. honest fallback phrasing

Descriptions must be concise, action-oriented, and free of Jira keys.

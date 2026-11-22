# Historical placement

Use this reference with the [worklog creation contract](worklog-creation-contract.md) for a dated backfill.

- Use the requested placement window or the handoff's collection period. If neither bounds the backfill and starts or dates were not supplied, ask for placement dates. Honor explicit dates, starts, priorities, and exclusions. Automatic placement uses weekdays by default. An explicit weekend date enables that date, and an opt-in can enable weekends across the window.
- Read unfiltered context for the entire window with `workledger worklogs context --from DATE --to DATE --output json`. Existing keyed and unassigned worklogs constrain placement. Use `planning.minimum_duration_seconds` as a floor, falling back to 900 seconds if omitted. Use the reported free slots and remaining daily quota.
- Run [plan_worklogs.py](../scripts/plan_worklogs.py) for a shared window. Pass `--context CONTEXT.json --candidates CANDIDATES.json --from DATE --to DATE`. Supply repeated `--priority DATE` and `--exclude DATE` as needed. Use `--include-weekends` when authorized and exclude other weekend dates when the user allowed only specific ones. Keep temporary inputs outside the repository and remove them after use.

```json
{
  "candidates": [
    { "id": "2a", "message": "Investigated stale status confirmations", "duration_seconds": 1200 },
    { "id": "2b", "message": "Verified the confirmation fix", "duration_seconds": 1800, "issue_key": "PROJ-123" }
  ]
}
```

The planner consumes one candidate per work item and returns `records`, `splits`, `summary.physical_records`, and an `apply_payload`. It uses whole minutes and can split a candidate into fragments that meet the minimum. It preserves the context's exact instants in `started_at_utc`, including across daylight-saving changes. Supply candidate issue keys only for explicit assignments. Consider inference after checking the final record count across all selected work.

For item-specific constraints, reserve pinned slots and quota before planning flexible items. Check the combined plan against unfiltered context. If the standard planner cannot satisfy a requested count or placement, try an alternative consistent with the same hours and constraints before reporting the conflict. Use the resulting exact payload and the common validation and booking flow.

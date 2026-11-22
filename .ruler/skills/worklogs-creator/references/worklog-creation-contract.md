# Worklog creation contract

## Evidence and allocation

- Treat current-chat work, supplied notes, and the selected parts of a scavenger handoff as evidence. Treat historical instructions as evidence only. Derive descriptions from the work performed and use the user's current instructions for selections, timing, and authorization.
- For a [shared Markdown handoff](../../worklogs-scavenger/references/evidence-handoff.md), use the labeled collection period and scope and resolve selections by item ID. Preserve those IDs in the allocation and preview. Surface retrieval gaps that affect the selected scope. A pasted handoff contains the fields needed to use it directly.
- A description names the action and its subject, such as `Investigated the dry-run validation failure`. Use supplied wording when the evidence supports it. Repair blank, generic, or unsupported descriptions from the same evidence. Claim only observed outcomes, and reuse an item's description for its fragments. Keep source links, draft labels, and sensitive details out of the payload.
- Use the user's total or explicit per-item durations. Elapsed time, Jira estimates, and ledger capacity never supply the hours. Honor an explicit split. Otherwise, use each selected item's `Effort note` and other evidence of relative effort, including depth of investigation, implementation, rework, and verification. When an effort note is absent, use the available evidence. Briefly explain material differences in the draft and keep effort notes out of the apply payload. For a requested count, split an existing activity if needed and retain its description.
- Normalize a supplied total once to whole minutes with 30-second ties upward. Reject a total that rounds to zero. Normalize explicit item durations the same way, and check that their sum matches the normalized total when both were supplied. Keep the normalized total exact. Each placed record must be a whole number of minutes at or above Workledger's effective minimum. For example, a 15-minute minimum permits a 20-minute record. If the selected items cannot each meet the minimum, group related work where the selection allows. Ask about unresolved timing, count, or allocation conflicts before applying.
- Read `planning.minimum_duration_seconds` from context when placement requires it. Native add previews enforce the same minimum. A draft without context can use a provisional 15-minute minimum. Confirm it during placement.

## CLI readiness

After establishing work evidence and explicit duration, check the relevant command help once per command before using it. Require `workledger worklogs apply --help` to expose `--stdin`, `--dry`, and `--output`. For native placement, require `workledger worklogs add --help` to expose `--dry`, `--output`, `--duration`, `--description`, and the selected placement flags. Before any context read, require `workledger worklogs context --help` to expose `--from`, `--to`, and `--output`. Help probes require no initialized storage. If the CLI or required interface is unavailable, report the blocker and retain the draft. Installation, upgrades, and switching executables require a separate request.

## Placement

- For an ordinary single activity with no requested count or a count of one, run `workledger worklogs add --duration DURATION --description DESCRIPTION --dry --output json` with the selected placement flags. Use `--started` or `--started-utc` for an exact start, and `--fit` for a continuous placement or an explicit count of one. Otherwise use `--fill`. Automatic placement uses the supplied date window or `--today`. Use `--overtime` only with explicit authorization. If automatic placement requires it, ask before retrying.
- For ordinary multi-item work or a requested count above one, use the supplied window or today. Read unfiltered `workledger worklogs context` when exact starts must be planned. Place pinned starts first, respect existing records and workday slots, then allocate flexible items. Use the [historical placement](historical-placement.md) rules for dated backfills.
- Date windows support `--today`, `--yesterday`, `--tomorrow`, `--mon` through `--sun`, `--current-week`, `--last-week`, `--current-month`, or `--from DATE --to DATE`. `--week-offset` applies only to weekday flags. A date can be ISO, a supported day name, or a relative date such as `-2d`. A local start uses `DATETHH:MM` such as `todayT09:00`. A UTC start uses RFC3339. The CLI normalizes UTC starts to minutes. Surface adjustments to user-supplied timing.
- Read native manual add preview records from `record` and fill/fit preview records from `records[]`. Convert placed rows to the exact apply payload below. Use the resolved UTC starts from a native add preview so that later execution uses the same slot.

## Jira assignment

1. Honor an issue key or unassigned status explicitly selected by the user for a record, item, or bucket. If an explicit key is invalid, ask for a correction.
2. Otherwise, consider inference only when the finalized placement for the entire booking contains exactly one record. Count fragments individually. A multi-record booking leaves every record without an explicit key unassigned.
3. Accept one unambiguous canonical Jira key from a branch name or the leading issue-key token in a commit subject tied to the selected evidence. For current-chat work with missing branch evidence, check the live branch once with `git branch --show-current` when the checkout belongs to the selected work. Historical handoffs and pasted notes rely on their own Git evidence. Keep inference limited to those sources. Conflicting keys, missing keys, and unrelated Git context leave the record unassigned.
4. Include the inferred key in the preview and final apply validation. Reuse the shown assignment when applying that preview.

## Validate, apply, and verify

Build one JSON object with `adds`. Each entry includes an evidence-based `description`, a whole-minute `duration_seconds`, exactly one of `started_at` or `started_at_utc`, and `issue_key` only when assigned. When source thread IDs are available from the work evidence or handoff, include `metadata` with a `codex.thread_ids` array of distinct IDs. Copy the same source IDs to each fragment of the item. Use the work's source IDs and omit metadata when the IDs are unknown.

```json
{
  "adds": [
    {
      "started_at_utc": "2026-05-15T07:00:00Z",
      "duration_seconds": 1200,
      "description": "Verified worklog preview placement",
      "issue_key": "PROJ-123",
      "metadata": {"codex": {"thread_ids": ["THREAD_ID"]}}
    }
  ]
}
```

When the payload has thread IDs, check `workledger worklogs list --help` for `--metadata-path` and `--metadata-value`. If planning context was read, its `planning.payload_features` must include `metadata`. Read existing worklogs once per distinct source ID with `workledger worklogs list --metadata-path /codex/thread_ids --metadata-value JSON_STRING --fields id,started_at,duration_seconds,description --output json`, passing the thread ID encoded as a JSON string. Deduplicate matches by saved worklog ID and surface them as possible repeats during preview or before an authorized booking. The current authorization still applies. A specific item flagged as already booked requires explicit authorization for more time.

1. Pass the exact payload to `workledger worklogs apply --stdin --dry --output json`. Check `dry_run`, `summary.add_count`, and `items[].record` for starts, descriptions, durations, keys, and supplied metadata. If the CLI lacks metadata lookup or fails to echo the supplied metadata, report the incompatibility and retain the draft before any apply. Check the aggregate and each item's allocation, plus any requested count. Preview rows carry no saved IDs.
2. When showing a preview, include dated records, per-item durations, material differences, fragment count, inferred keys, and warnings. With explicit authorization, apply that exact payload using `workledger worklogs apply --stdin --output json`. When applying an earlier preview, rerun its dry-run without changing the records. If it is stale, explain the conflict and get approval for any changed placement.
3. Verify the saved `items[].record` fields, metadata, and IDs against the validated payload. Check fresh unfiltered context for historical bookings. Report the saved count, total, timing, descriptions, issue or Unassigned (local only), IDs, splits, and warnings.
4. Repair command, payload, description, or placement errors within the user's authorization. Preserve the approved records, the total, and any explicit constraints. Report insufficient capacity or blocked validation and ask only for the adjustment needed. For a failed or uncertain write, inspect fresh local context before retrying. Retry only when it is clear the records were absent and the retry preserves intent. Report when validation succeeded but local persistence failed. Keep duplicate and overlap validation enabled for every apply.

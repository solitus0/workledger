# Local worklog context decisions

Frozen decisions:

- `worklogs context` is a read-only follow-up local scope.
- Planning-state inspection remains separate from local mutation commands.
- `worklogs context` may expose machine-readable planning hints for external payload construction without generating adds.
- Cross-midnight worklogs appear once under the local start day in `days[*].worklogs`, but per-day booked time, free-slot analysis, and collision analysis are prorated by the occupied interval slice inside each selected local day.
- `planning.issues[*]` guarantees baseline fields `issue_key` and `max_estimate_seconds`; additive read-only metadata fields are allowed, and surfaced missing metadata stays explicit as `null`.

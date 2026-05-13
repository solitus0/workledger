# Local worklog context

This feature owns read-only planning-state inspection for local worklogs.

Canonical specs:

- [Product](../../product.md)
- [API](../../api.md#local-worklogs)
- [UX](../../ux.md#agent-workflows)
- [Testing](../../testing.md)

Scope summary:

- `workledger worklogs context`
- day snapshots, free slots, collisions, settings, and planning hints output
- read-only planning context over canonical local worklogs
- cross-midnight rows remain owned by their local start day for `days[*].worklogs`, while per-day analysis is prorated by the occupied interval slice inside each selected local day
- `planning.issues[*]` always exposes `issue_key` and `max_estimate_seconds`, with optional additive read-only metadata fields in this scope

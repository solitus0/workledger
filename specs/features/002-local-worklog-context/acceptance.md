# Local worklog context acceptance

Key acceptance coverage:

- `workledger worklogs context` returns read-only planning snapshots.
- `workledger worklogs context` supports the shared date-window shortcut selectors `--current-week`, `--last-week`, `--current-month`, and `--last-month`.
- Repeated `--issue` inputs remain ordered and surface machine-readable planning hints.
- Empty selected days remain visible in the returned scope.
- A selected multi-day range with one cross-midnight worklog lists that row once on the local start day, while the following selected day still reflects the carried-over occupied slice in `booked_seconds`, `free_slots`, and `collisions`.
- `planning.issues[*]` always includes `issue_key` and `max_estimate_seconds`, and may also include additive read-only metadata fields without changing the baseline contract.
- JSON output remains stable and table output remains deterministic.

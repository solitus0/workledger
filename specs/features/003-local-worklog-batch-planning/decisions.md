# Local worklog batch planning decisions

Frozen decisions:

- `worklogs shift` and `worklogs apply` are deferred out of `001-local-worklogs`.
- Batch planning remains local-only and builds on canonical SQLite worklogs.
- The preferred multi-entry workflow is `workledger worklogs context` followed by raw-payload `workledger worklogs apply`.
- Slotting, allocation, and description construction stay outside the CLI and consume `worklogs context` output.

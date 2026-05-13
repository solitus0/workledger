# Local worklog batch planning acceptance

Key acceptance coverage:

- `workledger worklogs shift` and `apply` satisfy the deferred local batch-planning scope.
- Batch validation failures return exit code `2` with no partial writes.
- Raw apply payload and dry-run preview contracts remain stable.

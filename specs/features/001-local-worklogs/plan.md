# Local worklogs plan

Implementation order:

1. Workspace bootstrap: `workledger init`, config validation, SQLite schema bootstrap.
2. Read flows: `worklogs list`, `worklogs show`, deleted tombstone visibility.
3. Write flows: `worklogs add`, `worklogs update`, `worklogs delete`, filtered batch delete.
4. Output hardening: table views, JSON contracts, exit codes, atomic write guarantees.
5. Defer planning and batch-mutation flows to follow-up local scopes.

Reference sections:

- [API / Configuration and storage](../../api.md#configuration-and-storage)
- [API / Local worklogs](../../api.md#local-worklogs)
- [Testing](../../testing.md)

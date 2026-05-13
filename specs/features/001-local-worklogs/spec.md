# Local worklogs

This feature owns canonical local worklog CRUD on SQLite.

Canonical specs:

- [Constitution](../../constitution.md)
- [Product](../../product.md)
- [API](../../api.md#local-worklogs)
- [Data model](../../data-model.md#sqlite-schema)
- [UX](../../ux.md#agent-workflows)
- [Testing](../../testing.md)

Scope summary:

- local-only canonical worklog CRUD
- selectors, tombstones, and batch delete
- UUID local identity and UTC persisted timestamps
- duplicate and overlap validation with `--force` bypass only where explicitly allowed

# Local worklog batch planning

This feature owns deferred batch local planning and mutation flows on top of canonical local worklogs.

Canonical specs:

- [Product](../../product.md)
- [API](../../api.md#local-worklogs)
- [UX](../../ux.md#agent-workflows)
- [Testing](../../testing.md)

Scope summary:

- `workledger worklogs shift`
- `workledger worklogs apply`
- batch timestamp movement and payload-based multi-entry local add mutation
- CLI-first batch workflow: `context` -> `apply`
- external payload generation with optional `apply --dry` preview

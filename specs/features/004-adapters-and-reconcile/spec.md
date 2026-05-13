# Adapters and reconcile

This feature owns deferred adapters, reconcile planning, apply, retry, reporting delivery, and progress-aware remote execution.

Canonical specs:

- [Constitution](../../constitution.md)
- [Product](../../product.md)
- [Architecture](../../architecture.md)
- [API](../../api.md)
- [Security](../../security.md)
- [UX](../../ux.md)
- [Testing](../../testing.md)

Included domains:

- shared pull rules and canonicalization boundaries
- adapter-specific config and command surfaces for Jira Cloud, Jira Data Center, and Clockify
- route profiles, reporting delivery, and saved-plan persistence
- `plan reconcile`, `plan show`, `plan list`, `plan apply`, `plan retry`, `status`, and `totals`
- issue-metadata refresh and progress rendering for long-running remote work

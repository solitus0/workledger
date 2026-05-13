# Adapters and reconcile tasks

- Implemented in the current codebase: adapter-family config validation and adapter-specific auth shapes.
- Implemented in the current codebase: `workledger status` empty-target success rules and adapter health checks.
- Implemented in the current codebase: `issue-metadata refresh` for Jira adapter families.
- Implemented in the current codebase: `plan reconcile` for pull and push with saved-plan persistence and scope classification.
- Implemented in the current codebase: route-profile resolution, reporting-target validation, and no-plan reporting outcomes.
- Implemented in the current codebase: `plan show` and `plan list` as SQLite-only review commands.
- Implemented in the current codebase: `plan apply` using saved payloads, config fingerprints, and per-item attempt persistence.
- Implemented in the current codebase: `plan retry` for explicit failed and uncertain subsets only.
- Implemented in the current codebase: Clockify project mapping and issue-tag behavior.
- Implemented in the current codebase: stderr-only progress reporting aligned to saved plan items and reconcile scopes.

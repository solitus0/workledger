# Adapters and reconcile decisions

Frozen decisions:

- local SQLite remains the only canonical worklog identity
- remote worklog IDs are transient adapter handles, not persisted canonical identity
- reporting delivery is an additive mirror only
- apply and retry execute saved payloads and do not rerun routing
- saved plan items are the concurrency and progress unit for apply
- uncertain outcomes require explicit retry logic and never allow blind recreate

# Adapters and reconcile plan

Implementation order:

1. Phase 4: adapter pull planning, saved pull plans, and local issue-metadata refresh.
2. Phase 5: direction-aware reconcile planning, classification, routing, and saved-plan review.
3. Phase 6: apply engine, cleanup/create execution, attempt persistence, and idempotent reruns.
4. Phase 7: retry and recovery for failed and uncertain items.
5. Progress UX: shared `--progress` flag, plain stderr reporting first, TTY bars second.

Reference sections:

- [Product / CLI roadmap](../../product.md#cli-roadmap)
- [Architecture](../../architecture.md)
- [API](../../api.md)
- [UX / Progress reporting](../../ux.md#progress-reporting)

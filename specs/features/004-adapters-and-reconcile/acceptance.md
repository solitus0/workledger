# Adapters and reconcile acceptance

Key acceptance coverage:

- Pull plans merge into canonical local state without making remote systems authoritative.
- Push plans classify scopes as `ready`, `invalid`, `blocked`, `skipped`, or `check_failed`.
- Reporting reconciles return a deterministic no-plan result when already in sync or when no routes match.
- `plan apply` and `plan retry` refuse saved plans with mismatched config fingerprints.
- Mixed apply outcomes return exit code `6`.
- Progress output stays on stderr and never contaminates JSON stdout.
- Uncertain items are never blindly replayed.

Canonical references:

- [API / Reconcile planning and review](../../api.md#reconcile-planning-and-review)
- [API / Reconcile apply and retry](../../api.md#reconcile-apply-and-retry)
- [UX / Progress reporting](../../ux.md#progress-reporting)
- [Testing](../../testing.md)

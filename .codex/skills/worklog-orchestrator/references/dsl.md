# Primary command interface

Prefer actual `workledger` commands over the legacy orchestration DSL.

## Canonical local worklog commands

Inspection:

```text
workledger worklogs list
workledger worklogs list --current-month --issue SBAB-292
workledger worklogs list --from today --to today --issue SBAB-292
workledger worklogs show 42
```

Creation:

```text
workledger worklogs add --issue SBAB-292 --started 2026-05-02T08:00 --duration 2h --description "Refined local worklog drafting flow"
workledger worklogs add --issue SBAB-292 --started todayT08:00 --duration 2h --description "Refined local worklog drafting flow"
```

Update:

```text
workledger worklogs update 42 --started 2026-05-02T08:15 --duration 1h45m --description "Refined local worklog drafting flow"
workledger worklogs update 42 --started -1dT08:15 --duration 1h45m --description "Refined local worklog drafting flow"
```

Delete:

```text
workledger worklogs delete 42
workledger worklogs delete 42 --hard
```

Filtered cleanup workflow:

```text
workledger worklogs list --from today --to today --issue SBAB-292
workledger worklogs list --last-month --issue SBAB-292
workledger worklogs delete 42
workledger worklogs delete 43
```

## Secondary deferred commands

Use only when the user explicitly asks for adapter inspection or reconcile flows:

```text
workledger status --adapter=clockify
workledger plan reconcile --push --adapter=clockify
workledger plan show
workledger plan apply
workledger plan list
workledger plan retry
```

## Drafting guidance

- Draft one or more local worklogs first.
- Inspect before mutating when records may already exist.
- Prefer `add` for new canonical records.
- Prefer `context` plus one raw `apply` payload for multi-entry creation when the deferred batch flow is available.
- Prefer `update` when the local record already exists and only fields need correction.
- Prefer delete-by-id after inspection instead of speculative cleanup.

## Deprecated translation appendix

The old flags-only DSL is compatibility input, not the main interface.

Legacy modes:
- `--extract-session` or `-x`
- `--inspect` or `-i`
- `--prepare`
- `--sync` or `-s`

Use `scripts/parse_worklog_dsl.py` only to normalize older requests. After parsing:

- map `--inspect` to local inspection with `workledger worklogs list` or `show`
- map `--prepare` to drafted local candidates plus `add` or `update` commands, or to `context` -> raw `apply` payload -> `apply` when the deferred batch flow is available
- map `--sync` to a secondary `workledger plan *` flow when the user is explicitly reconciling downstream systems
- map `--extract-session` to evidence capture only

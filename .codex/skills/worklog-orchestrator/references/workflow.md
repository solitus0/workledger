# CRUD-first workflow

## Primary path

1. Resolve the local intent: one-row CRUD, batch local planning, or deferred adapter reconciliation.
2. Inspect existing local worklogs first when the request might mutate state.
3. Resolve supporting evidence:
   - session artifact
   - current session summary
   - Git or Jira context when helpful
4. Optionally create or refresh a session artifact when durable evidence will help drafting or later review.
5. For one-row creation, inspect with `workledger worklogs list` and then use `workledger worklogs add`.
6. For multi-entry creation, once deferred batch commands are available, inspect with `workledger worklogs context`, build one raw payload outside the CLI, and execute with `workledger worklogs apply`.
7. Use assistant heuristics only for evidence interpretation, allocation policy, and description choice. Let the CLI own free-slot derivation, chronology validation, and payload acceptance when those commands are available.
8. Use `workledger worklogs update` for existing-row corrections.
9. For cleanup, inspect first and then delete by id with `workledger worklogs delete`.

## Drafting heuristics for local worklogs

- Prefer CLI-derived free slots from `workledger worklogs context`.
- Default primary workday is `08:00-17:00` with lunch `12:00-13:00`.
- Use overtime only when primary slots are insufficient.
- Ask before using overtime if the requested total exceeds the primary 8h workday.
- Allocate time with this precedence:
  1. user hard constraints
  2. CLI-supported allocation rules such as explicit `--allocate` or future strategy flags
  3. merged session and Git evidence
  4. equal fallback
- Place worklogs chronologically and without artificial gaps.
- Prefer one raw `worklogs apply` payload over many hand-built `worklogs add` commands for multi-entry creation.
- Generate descriptions from session summaries first, then Git, then Jira, then honest fallback phrasing.

These are assistant heuristics. They help draft local worklogs, but they are not canonical product truth.

## Secondary deferred path

Use this only when the user explicitly asks for future adapter inspection or reconciliation:

1. Read local worklogs or a previously saved plan.
2. Inspect adapter status with `workledger status --adapter=clockify` if needed.
3. Build or review a saved plan with `workledger plan reconcile`, `workledger plan show`, `workledger plan list`, or `workledger plan retry`.
4. Apply a reviewed plan with `workledger plan apply`.

Clockify remains downstream of local worklogs in this path.

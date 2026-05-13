# Output contracts

## Local inspection summary

Use for `workledger worklogs list` or `workledger worklogs show`.

Return these sections in order:

1. Scope
2. Existing local worklogs
3. Gaps, collisions, or cleanup notes when relevant
4. Suggested next command

## Candidate drafted worklogs

Use when turning evidence into canonical local worklog candidates.

Return these sections in order:

1. Scope and evidence used
2. Draft strategy
3. Candidate worklogs or draft summary
4. Validation notes
5. Suggested next command
6. Warnings or assumptions

Prefer `workledger worklogs context` plus one raw `workledger worklogs apply` payload over emitting many `workledger worklogs add` commands when the deferred batch flow is available.

Each candidate should include:
- issue
- started timestamp
- duration
- description

When a raw apply payload exists, summarize the payload and point to the next `apply --dry` or `apply` action instead of restating every add command.

## Suggested update actions

Use when existing local worklogs need correction.

Return these sections in order:

1. Current local worklog summary
2. Proposed field changes
3. Suggested `workledger worklogs update` commands
4. Risks or assumptions

## Suggested delete actions

Use when incorrect local worklogs should be removed.

Return these sections in order:

1. Local worklogs selected for cleanup
2. Why each record should be deleted
3. Suggested `workledger worklogs delete` commands
4. Any safer update alternative when applicable

## Deferred plan-review output

Use only for secondary `workledger status` or `workledger plan *` requests.

Return these sections in order:

1. Scope
2. Saved-plan or adapter summary
3. Recommended next command
4. Warnings

## Formatting guidance

- Keep outputs compact.
- Show durations in human-readable form.
- Keep descriptions concise.
- Make the concrete next command obvious.

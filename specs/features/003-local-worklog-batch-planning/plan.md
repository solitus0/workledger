# Local worklog batch planning plan

Implementation order:

1. Extend `worklogs context` with machine-readable planning hints for external payload builders.
2. Implement `worklogs apply` raw-payload validation and atomic batch local add execution.
3. Implement `worklogs apply --dry` deterministic preview output.
4. Implement `worklogs shift` dry-run and atomic timestamp movement.
5. Harden JSON payload and preview contracts across batch flows.

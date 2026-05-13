# Local worklogs tasks

- Implement config-path bootstrap, strict config validation, and SQLite provisioning.
- Implement `worklogs list` with active, deleted, date, issue, and field selectors.
- Implement `worklogs show`, `add`, `update`, and `delete` around canonical UUID worklogs.
- Implement filtered batch delete with `--dry` preview and `--yes` guard.
- Enforce duplicate, overlap, minimum-duration, issue-key, and timestamp validation rules.
- Preserve tombstones for default deleted rows, allow explicit hard-delete bypass, and keep tombstones visible only through supported `worklogs` commands.

# Troubleshooting

[Documentation](README.md) · [Configuration](configuration.md)

Start with:

```sh
workledger version
workledger status
```

Status checks config, environment variables, routing, storage, and remote connectivity. Diagnose the failing layer before changing unrelated settings.

| Symptom | Inspect | Next step |
| --- | --- | --- |
| Config is missing or setup is ignored | `workledger config` | Run `init` for first setup; inspect the operator YAML for an existing setup |
| Local storage or schema is unavailable | `workledger status` | Check path and permissions; run `init` to provision or update compatible storage |
| Token environment variable is missing | `workledger status` | Set the named variable in the shell that launches Workledger |
| Jira issue routes to the wrong place | `workledger route explain PROJ-123` | Correct ownership prefixes or reporting profiles |
| Clockify project is unresolved | `workledger clockify mappings validate` | Correct project mapping and confirm the remote project exists |
| Authentication or API failure | `workledger status` | Check account credentials, URL, user/workspace identity, and network access |
| A plan partly succeeded | `workledger plan show <plan-id>` | Inspect results and use the [recovery workflow](synchronization.md#handle-partial-results) |

## TUI opens Status or cannot start

Healthy startup opens today’s Worklogs while diagnostics load. Invalid config or unavailable/incompatible storage opens a diagnostics-only Status view. The TUI never initializes or repairs storage itself; exit and use `workledger init` when initialization or migration is required.

A terminal smaller than 100 × 28 shows resize guidance. Piped stdin/stdout and explicit `--output` selection are rejected. Run `workledger tui` directly in an interactive terminal.

## Colors or characters look wrong

Try `NO_COLOR=1 workledger tui` to separate color issues from layout. Verify your terminal background reporting, true-color support, and a font with box-drawing characters. Compare against the [capture conditions](media.md). Include terminal dimensions, emulator, font, theme, and multiplexer when reporting a rendering problem.

## A save reports stale data or a conflict

Another process may have changed the selected worklog after the preview opened. Refresh and review the current entry. Move and edit use revision checks to avoid silently overwriting newer work. Duplicate, overlapping, or unavailable placements require resolving the actual conflict; do not assume repeating save will succeed.

## Upgrading an existing ledger

Run `workledger init` after an upgrade if status reports outdated local storage. Bootstrap preserves supported existing worklogs and issue metadata while provisioning current storage. Do not replace a database merely to silence a schema or integrity error; retain a backup and inspect the reported failure.

Saved plans can become stale as configuration, source records, or required snapshots change. Create a fresh reconciliation plan when the existing one no longer satisfies the execution contract.

## Report a problem

Include the Workledger version, operating system, and agent or shell used. Give the exact command or minimal interaction sequence, expected and actual behavior, exit code, and sanitized stdout/stderr. For optional-TUI issues, add terminal emulator, dimensions, and relevant multiplexer details; include the font, theme, or screenshot when it affects the symptom. Remove credentials, personal worklogs, and operator config secrets before sharing.

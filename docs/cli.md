# CLI reference

[Documentation](README.md) · [Daily workflows](usage.md)

The CLI is Workledger’s primary interface for people, agents, and scripts. Use JSON output, dry runs on supported mutations, and batch payloads for automation. The [optional TUI](keybindings.md) provides interactive review of the same local ledger.

Run `workledger --help` for command groups and `workledger <command> --help` for exact flags and examples. Bare `workledger` displays help. Use `workledger version`, `--version`, or `-v` to inspect the installed version.

## Dates and local time

Use one date selector per command, or the explicit inclusive `--from`/`--to` pair when supported.

| Selector | Window |
| --- | --- |
| `--today`, `--yesterday`, `--tomorrow` | One local day |
| `--mon` through `--sun` | One day in the current local Monday–Sunday week |
| `--current-week`, `--last-week` | Current or previous local Monday–Sunday week |
| `--current-month`, `--last-month` | Current or previous local calendar month |
| `--from <date> --to <date>` | Explicit inclusive local date range |
| `--week-offset <n>` | Shift a weekday selector by whole weeks; valid only with `--mon` through `--sun` |

Dates accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, `mon` through `sun`, and signed day offsets such as `+2d` or `-3d`.

Local start timestamps use those date forms followed by `THH:MM`, for example `2026-11-03T09:00`, `todayT09:00`, or `monT09:00`. Weekdays resolve within the current local week. `--started-utc` uses RFC3339 UTC, such as `2026-11-03T07:00:00Z`.

## Batch worklogs

`worklogs apply` accepts a JSON object with an `adds` array. Supply exactly one of `started_at` or `started_at_utc` per entry:

```json
{
  "adds": [
    {
      "issue_key": "PROJ-123",
      "started_at": "2026-11-03T09:00",
      "duration_seconds": 3600,
      "description": "Implement retry handling"
    }
  ]
}
```

```sh
workledger worklogs apply --file payload.json --dry --output json
workledger worklogs apply --file payload.json --output json
```

Use `--stdin` instead of `--file` to read a payload from stdin. Issue keys may be omitted for unassigned work. Issue metadata uses a separate JSON-array patch format described in [daily workflows](usage.md#issues-epics-and-effort-budgets).

## Output and exit codes

Human output defaults to tables. Use `--output json` for automation:

```sh
workledger worklogs list --today --output json
workledger activity list --source tui --limit 20 --output json
```

Stdout contains command output. Logs, diagnostics, and progress use stderr; JSON output remains parseable without mixed log lines. Commands that support `--fields` can select record fields; consult their help for allowed names. The interactive TUI rejects an explicit `--output` flag.

| Exit | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Unexpected failure |
| `2` | Validation or input failure |
| `3` | Not found |
| `4` | Authentication failure |
| `5` | External or connectivity failure |
| `6` | Partial success |
| `130` | Interrupted or canceled where reported |

Read the result details on partial success; some mutations may have completed. Saved-plan outcomes and uncertainty need the recovery flow in [synchronization](synchronization.md#handle-partial-results).

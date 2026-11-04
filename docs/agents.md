# Agent workflows

[Documentation](README.md) · [CLI reference](cli.md)

Workledger is an agent-friendly CLI. Agents can read local planning context, preview worklog writes, submit JSON batches, and review saved sync plans through ordinary commands. The TUI is an optional interactive view; agent workflows use the CLI directly.

## Use the CLI directly

After the [quick start](../README.md#quick-start), inspect the local ledger and available time:

```sh
workledger worklogs list --today --output json
workledger worklogs context --today --output json
```

Use `--dry` on supported mutations before writing. JSON results stay on stdout while diagnostics and progress go to stderr. [Batch payloads](cli.md#batch-worklogs) accept a file or stdin; [exit codes](cli.md#output-and-exit-codes) distinguish success, input failures, partial results, and other outcomes. Remote changes follow [reconcile → review → apply](synchronization.md).

The optional skills below package guided workflows around these commands.

## Onboarding

Install the onboarding skill for guided setup, routing checks, and status diagnostics:

```sh
npx skills add solitus0/workledger --skill workledger-onboarding -g
```

It explains Jira ownership prefixes, configures optional adapters, checks readiness, and hands off to the mandatory initial pull before the first push. See its [canonical source](../.ruler/skills/workledger-onboarding/SKILL.md).

## Log a coding session

```sh
npx skills add solitus0/workledger --skill session-worklog-creator -g
```

After doing work in a session, invoke the skill in the same agent chat:

```text
$session-worklog-creator 1h
```

**You decide how much time to log.** Duration is the only mandatory user input; the agent never infers it. The skill derives a descriptive entry from actual session evidence, previews it, and applies locally after a clean dry run. You can also ask in plain language, for example: “Log 45m for this session.”

Placement is automatic: by default, the skill uses `--fill --today` to distribute the supplied duration across available slots in your configured workday, respecting existing bookings and reserved lunch. One activity may produce multiple worklog fragments, with the same total duration. A supplied date, exact start time, issue key or split overrides the corresponding default; none is required for the ordinary flow.

An explicit issue selection wins. Otherwise, an unambiguous session or branch key takes precedence, followed by a clear local metadata-title match. Unresolved work is saved unassigned without requiring a key. Estimates and remaining budget never determine attribution or duration. The skill does not invent work for an empty session or infer elapsed time.

See the [skill contract](../.ruler/skills/session-worklog-creator/SKILL.md) for its scope and failure handling.

## Local access and remote changes

The agent needs write access to your chosen local SQLite directory, including SQLite sidecar files. When an agent sandbox blocks the shared storage directory, grant that directory deliberately using the agent’s supported configuration or retain explicit approval per write. Use `workledger status` to diagnose storage writability.

Local creation is separate from tracker synchronization. Keep JSON output for automation, inspect dry runs, and follow the [reviewed synchronization workflow](synchronization.md) for remote changes. A successful local write does not mean a tracker has been updated.

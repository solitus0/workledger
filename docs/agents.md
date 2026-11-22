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
npx skills add solitus0/workledger --skill worklogs-creator -g
```

After doing work in a session, ask the skill to log it in the same chat.

```text
$worklogs-creator Log 1h from this chat.
```

Supply the duration and ask explicitly to log, book, or apply when you want a local write. The creator derives descriptions from evidence in the current chat, pasted notes, or a scavenger handoff. A preview shows the proposed records for review. “Apply that preview” saves those exact records after revalidation.

For ordinary logging, the creator fills free slots today by default, around existing bookings and reserved lunch. One activity may produce several worklog fragments with the same combined duration. You can supply a placement window, exact start, issue key, or split.

You can assign Jira keys explicitly for any number of worklogs. If placement produces exactly one record, the creator can infer an unambiguous key from a relevant branch or a leading Jira key in a commit subject. Other records without an explicit assignment are saved unassigned.

See the [Worklogs Creator skill](../.ruler/skills/worklogs-creator/SKILL.md) for the full workflow.

## Backfill from Codex chats

```sh
npx skills add solitus0/workledger --skill worklogs-scavenger -g
```

Ask the scavenger to collect evidence for a period, then review its editable Markdown handoff. Select work by item ID and give the creator a total or per-item durations. When you supply one total, the creator uses the evidence in the effort notes to propose a split.

```text
$worklogs-scavenger Collect work from my Codex chats last week.
$worklogs-creator Preview 3h across items 2a and 2b.
Apply that preview.
```

You can paste the list into another chat. The creator uses the collection period by default and places historical work on weekdays within free slots and the remaining daily quota. Explicit dates can include weekends. See the [Worklogs Scavenger skill](../.ruler/skills/worklogs-scavenger/SKILL.md).

When the handoff includes Codex thread IDs, the creator saves them as [local worklog metadata](cli.md#local-worklog-metadata) and surfaces existing worklogs from those threads as possible repeat bookings. A thread can contain several distinct work items.

## Local access and remote changes

The agent needs write access to your chosen local SQLite directory, including SQLite sidecar files. When an agent sandbox blocks the shared storage directory, grant that directory deliberately using the agent’s supported configuration or retain explicit approval per write. Use `workledger status` to diagnose storage writability.

Local creation is separate from tracker synchronization. Keep JSON output for automation, inspect dry runs, and follow the [reviewed synchronization workflow](synchronization.md) for remote changes. A successful local write does not mean a tracker has been updated.

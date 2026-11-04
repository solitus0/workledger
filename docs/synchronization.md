# Synchronization

[Documentation](README.md) · [Configure adapters](configuration.md)

Workledger keeps canonical worklogs locally. Jira and Clockify supply comparison evidence and receive explicitly reviewed changes.

The sequence is **reconcile → review → apply**. `plan reconcile` inspects data and saves the proposed changes; it does not apply local or remote worklog changes. `plan show` lets you inspect them. `plan apply` performs them.

## First import

After configuring adapters and successfully checking `workledger status`, pull the complete date window that your first push will cover. To preserve all history, begin at the earliest remote worklog date you need to retain.

Replace the date and plan-ID placeholders below with your actual values:

```sh
workledger plan reconcile --pull --from <start-date> --to <end-date>
workledger plan show <pull-plan-id>
workledger plan apply <pull-plan-id>
```

Omitting `--adapter` and `--instance` selects all configured reconcile-capable targets. Cover every intended source. Review the pull plan and apply it so existing remote records become canonical local worklogs.

**Do not push while an intended source is skipped, failed, or outside the imported window.** Resolve the source and repeat the pull first. A push can classify remote-only records in its window as cleanup and delete them. Pull and apply the complete window again when a later push extends beyond the imported dates.

A no-plan result completes this step only when every intended target was checked successfully. A saved plan with `check_failed` scopes or output reporting skipped targets needs investigation before a push.

## Compare totals

```sh
workledger totals --instance clockify --today
workledger totals --adapter jira-cloud --instance main --current-week
```

Totals are comparison data, not proof that every entry matches or that a plan has been applied. Use reconcile and review to inspect changes at worklog scope.

## Push reviewed local work

Once the initial pull covers the intended push window:

```sh
workledger plan reconcile --push --from <start-date> --to <end-date>
workledger plan show <push-plan-id>
workledger plan apply <push-plan-id>
```

For this first push, use exactly the date bounds successfully pulled and applied above. Use shortcuts such as `--current-week` after confirming that the complete resulting window has been imported from every intended source.

Confirm the direction, inclusive local dates, target instances, route profiles, and entries to create, update, or delete (cleanup) before applying. Unassigned local work counts toward your local day but is skipped by push; [assign an issue](usage.md#work-before-an-issue-exists) when appropriate.

## Handle partial results

Read the actual per-scope result after apply. Completed scopes may remain successful when other scopes fail. Do not assume a nonzero exit means nothing changed.

Retry eligible failed scopes after resolving their errors:

```sh
workledger plan retry <plan-id> --only failed
```

Uncertain outcomes require reconciliation before further execution; inspect the saved plan and its errors instead of blindly repeating a remote mutation. Use `workledger plan retry --help` for supported retry scopes. A stale plan or changed configuration can require a fresh reconcile.

The [CLI reference](cli.md#output-and-exit-codes) explains failure and partial-success exit codes.

## In the TUI

Press `3` for Plans. Browse plans for the selected day or week, press Enter to review a saved plan, and use `?` to discover available actions. A new plan explicitly chooses Push or Pull, dates, target, and any applicable route profile. Review the effective scope before confirming execution.

Plan review opens saved local data without contacting a tracker. Creation and execution show their progress and results, including partial failures. See [Plans controls](keybindings.md#plans).

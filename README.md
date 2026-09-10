# workledger

`workledger` is a local-first CLI for keeping canonical worklogs in SQLite and reconciling them with remote time trackers through reviewed plans.

It is built for operators and coding agents that need one inspectable source of truth for work done, gaps, corrections, totals, and sync decisions.

## Core contract

- YAML stores operator-managed configuration at `~/.config/workledger/config.yaml`.
- SQLite stores canonical local worklogs at `~/.local/share/workledger/worklogs.db` by default.
- Remote adapters such as Jira and Clockify provide evidence, comparison data, and sync targets; they are not the source of truth.
- Reconciliation is plan-based: inspect, save, review, then apply.
- Before the first push for a date window, pull and apply that complete window from every intended remote source so existing remote-only worklogs are preserved locally.
- Human output defaults to `table`; automation should use `--output json`.
- Adapter secrets are referenced by environment variable names. Inline secrets are invalid.

The canonical product contract lives in:

- [`specs/functional.md`](specs/functional.md)
- [`specs/non-functional.md`](specs/non-functional.md)

When this README and the specs disagree, the specs win.

## Install

```sh
brew install solitus0/tap/workledger
```

For local development:

```sh
go run ./cmd/workledger --help
```

## Shell completion

Workledger generates completion scripts for Bash, Zsh, and Fish. PowerShell is not supported.

For Zsh, generate the script once:

```sh
mkdir -p ~/.zfunc
workledger completion zsh > ~/.zfunc/_workledger
```

Then ensure `~/.zshrc` initializes that completion directory before `compinit`:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

For Bash, load completion in the current session:

```bash
source <(workledger completion bash)
```

For Fish, install the generated script permanently:

```fish
mkdir -p ~/.config/fish/completions
workledger completion fish > ~/.config/fish/completions/workledger.fish
```

Regenerate installed scripts after upgrading to a release that changes commands or flags.

## First run

```sh
workledger init
workledger status
```

`init` creates the config file when needed and provisions local SQLite storage. `status` runs setup diagnostics and shows authenticated identity details for successful remote checks.

After configuring remote adapters, make the first reconciliation action a pull. The initial pull workflow is documented under [Configuration](#configuration); complete it before creating any push plan.

Workledger also includes an interactive terminal UI, available through `workledger tui`.

## Daily workflow

Add work locally at an explicit time:

```sh
workledger worklogs add --issue PROJ-123 --started todayT09:00 --duration 2h --description "Implement reconciliation flow"
```

Let `workledger` place one entry in the earliest free slot:

```sh
workledger worklogs add --issue PROJ-123 --fit --today --duration 2h --description "Implement reconciliation flow"
workledger worklogs add --issue PROJ-123 --fit --mon --duration 90m --description "Review pull request"
```

Fill a selected date window, splitting across free slots when needed:

```sh
workledger worklogs add --issue PROJ-123 --fill --from 2026-05-14 --to 2026-05-14 --duration 5h --description "Implement reconciliation flow"
workledger worklogs add --issue PROJ-123 --fill --tue --duration 3h --description "Prepare release notes"
```

Save repeated work as a reusable preset and apply it to any explicit local day:

```sh
workledger presets add daily-standup --issue PROJ-123 --start 09:45 --duration 15m --description "Daily standup"
workledger presets apply daily-standup --date tomorrow
workledger presets apply daily-standup --date mon --start 10:00 --dry
```

Use `workledger presets list`, `show`, `update`, and `delete` to manage presets. Applying a preset creates one ordinary local worklog; one-off overrides do not change the saved preset. Shell completion suggests locally stored preset names.

Automatic placement starts each worklog before the configured workday end by default, although a worklog that starts before the boundary may finish afterward. Use `--overtime` with `--fit` or `--fill` to allow placement to start at or after the workday end:

```sh
workledger worklogs add --issue PROJ-123 --fill --today --overtime --duration 2h --description "Handle release follow-up"
```

### `--fit` vs `--fill`

![Worklog placement modes: --fit vs --fill](docs/images/fill_v_fit.png)

`--started` accepts local timestamps such as `2026-05-14T09:00`, `todayT09:00`, `yesterdayT09:00`, `tomorrowT09:00`, `monT09:00`, `+2dT09:00`, and `-3dT09:00`. Weekday names `mon` through `sun` resolve inside the current local Monday-through-Sunday week.

Inspect the current local day:

```sh
workledger worklogs context --today
```

Preview and delete worklogs that entered the local ledger recently:

```sh
workledger worklogs delete --created-within 15m --dry
workledger worklogs delete --created-within 15m --yes
```

`--created-within` uses positive whole-second Go durations such as `15m`, `2h`, or `90s`. It filters the local `created_at` timestamp rather than the worklog start time. Worklogs inserted by a reconciliation pull receive a new local creation timestamp, so a recent pull can also match this selector. Batch deletion remains recoverable through local trash; use the dry-run before execution to review each matched row and its creation time.

Permanently remove selected trash records or empty all trash:

```sh
workledger trash delete <trash-id> --dry
workledger trash delete <trash-id> --yes
workledger trash delete --scope local --trashed-within 15m --dry
workledger trash clear --dry
workledger trash clear --yes
```

Trash date selectors continue to filter the original worklog start time; `--trashed-within` instead filters when the archive row entered trash. Filtered deletion can target local or remote trash with `--scope`. `trash clear` always removes both recoverable local trash and remote audit evidence. These operations are irreversible within Workledger and do not change active worklogs, saved plans, or remote services. SQLite deletion is not forensic erasure: data may remain in WAL files, filesystem snapshots, or backups.

Compare local time with a configured adapter:

```sh
workledger totals --instance clockify --today
```

After completing the initial pull described under [Configuration](#configuration), create and execute a push plan:

```sh
workledger plan reconcile --push --today
workledger plan show <plan-id>
workledger plan apply <plan-id>
```

### Reconcile -> review -> apply

![Reconcile, review, then apply remote changes](docs/images/main_flow.png)

`plan reconcile` is non-destructive. It saves the execution contract; `plan apply` performs the mutation after review.

## Date selectors

Use one date selector per command unless the command accepts an explicit `--from` and `--to` range.

| Selector | Meaning |
| --- | --- |
| `--today` | Current local day. |
| `--yesterday` | Previous local day. |
| `--tomorrow` | Next local day. |
| `--mon` through `--sun` | One day in the current local Monday-through-Sunday week. |
| `--current-week` | Current local Monday-through-Sunday week. |
| `--last-week` | Previous local Monday-through-Sunday week. |
| `--current-month` | Current local calendar month. |
| `--last-month` | Previous local calendar month. |
| `--from <date> --to <date>` | Explicit inclusive local date range. |
| `--week-offset <n>` | Shift one weekday selector by whole weeks; valid only with `--mon` through `--sun`. |

`--from` and `--to` accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, weekdays `mon` through `sun` in the current local week, and signed day offsets such as `+2d` or `-3d`.

## Recommended agent workflow

If you use an agent with `workledger`, install the `workledger-onboarding` skill for guided first-time setup and `status` diagnostics before any worklog entry or remote sync workflow.

```sh
npx skills add solitus0/workledger --skill workledger-onboarding -g
```

Install the `session-worklog-creator` skill to turn current coding-session context into local worklogs with minimal prompting.

```sh
npx skills add solitus0/workledger --skill session-worklog-creator -g
```

If you use Codex and want it to write local worklogs to the shared SQLite database outside the current agent session without asking for permission on every add, allow the default storage path in your Codex sandbox config:

```toml
[sandbox_workspace_write]
writable_roots = [
  "~/.local/share/workledger"
]
```

## Configuration

Run setup commands to add adapters to the local YAML config:

```sh
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
workledger setup jira-data-center --instance internal --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
workledger setup clockify
```

For Jira, `--issue-prefix` is the project key at the start of an issue key: use `PROJ` for issues such as `PROJ-123`. Workledger needs it for routing—it tells Workledger which configured Jira instance owns those issues. Without that mapping, Workledger cannot safely decide where a local worklog for `PROJ-123` should be read from or sent.

Pass `--issue-prefix` once for each Jira project handled by the instance:

```sh
workledger setup jira-cloud ... --issue-prefix PROJ --issue-prefix APP
```

This does not filter Jira during setup and it is not a display label. It creates the instance's default routing rules. Choose only prefixes that belong to that instance; the same prefix cannot be owned by two instances in the same Jira family.

Each setup command can prompt interactively or accept flags. Use command help for exact inputs:

```sh
workledger setup jira-cloud --help
```

### Initial remote pull

After `workledger status` succeeds, the first reconciliation action must pull existing worklogs from every configured remote source into the local ledger. Choose a window that covers the complete date range of the first push; use the earliest remote worklog date when importing all history.

```sh
workledger plan reconcile --pull --from <earliest-date-to-preserve> --to <end-of-first-push-window>
workledger plan show <plan-id>
workledger plan apply <plan-id>
```

Omitting `--adapter` and `--instance` selects all configured reconcile-capable targets. Review the pull plan before applying it. Do not create or apply a push plan if any intended source was skipped or failed; resolve the source and repeat the pull first. A push may otherwise classify remote-only worklogs in its date window as cleanup and delete them. If a later push uses a date outside the imported range, pull and apply that complete window before pushing it.

## Output and failure model

- stdout is reserved for command output.
- stderr is used for logs, diagnostics, and progress.
- `--output json` writes valid JSON to stdout without mixed logs.
- Exit code `0` means success.
- Exit code `1` means unexpected failure.
- Exit code `2` means validation or input failure.
- Exit code `3` means not found.
- Exit code `4` means authentication failure.
- Exit code `5` means external or connectivity failure.
- Exit code `6` means partial success.

## Contributing

Start with the specs before changing behavior. Keep implementation, tests, and documentation aligned with the current contract in the same change.

Use standard Go checks:

```sh
go test ./...
```

Business logic belongs outside Cobra command handlers. Keep CLI rendering, exit-code mapping, storage, adapter work, and domain behavior separated.

# workledger

`workledger` is a local-first CLI for keeping canonical worklogs in SQLite and reconciling them with remote time trackers through reviewed plans.

It is built for operators and coding agents that need one inspectable source of truth for work done, gaps, corrections, totals, and sync decisions.

## Core contract

- YAML stores operator-managed configuration at `~/.config/workledger/config.yaml`.
- SQLite stores canonical local worklogs at `~/.local/share/workledger/worklogs.db` by default.
- Remote adapters such as Jira and Clockify provide evidence, comparison data, and sync targets; they are not the source of truth.
- Reconciliation is plan-based: inspect, save, review, then apply.
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

## Activity history

Workledger keeps the newest 500 CLI commands and foreground TUI operations as diagnostic activity in the local SQLite store. It records safe command identifiers and selectors, outcomes, and timing without storing raw command lines, descriptions, payloads, credentials, or other free-text inputs. Logging is silent and best-effort, so it never changes command output or exit status and is unavailable until configuration and SQLite storage are valid.

Inspect recent activity from the CLI:

```sh
workledger activity list
workledger activity list --source cli --state failed --limit 20
workledger activity list --output json
```

In the TUI, press `g` to open the global Activity drawer. It shows meaningful worklog, trash, preset, and plan actions from the CLI and TUI while omitting reads, refreshes, setup and maintenance commands, and the TUI launcher. Use `j`/`k` or the arrow keys to select entries, Page Up/Page Down to page, Home/End to jump, and `g` to close it. The complete persisted history remains available through `workledger activity list`; activity history is intended for diagnosis and is not an immutable audit log.

The `3 Plans` tab defaults to the 100 newest plans created during the selected local Monday-through-Sunday week. Press `d` or `w` to switch between the selected day and week, use `h`/`l` to move by one day or week, and press `t` to return to today. Its rail summarizes visible unapplied, failed, and uncertain scopes, while the list keeps immutable planning status separate from derived execution state and shows open and succeeded scope counts. Press `n` to inspect remote worklogs for the selected day, week, or a custom inclusive date range and save a plan; target and Jira route-profile selection are available in the form. Press `Enter` to review a saved plan, then use the displayed `A`, `f`, or `u` actions when unapplied, failed, or uncertain scopes are available. Planning never applies changes, every apply or retry requires explicit confirmation, and a plan becomes `succeeded` only after every actionable scope succeeds.

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

Use `workledger presets list`, `show`, `update`, and `delete` to manage presets. Applying a preset creates one ordinary local worklog; one-off overrides do not change the saved preset. Shell completion suggests locally stored preset names. In the TUI, press `a` for a blank worklog or uppercase `A` for the searchable preset picker; the `4 Presets` tab provides full preset management.

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

Compare local time with a configured adapter:

```sh
workledger totals --instance clockify --today
```

Create and execute a remote sync plan:

```sh
workledger plan reconcile --today
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

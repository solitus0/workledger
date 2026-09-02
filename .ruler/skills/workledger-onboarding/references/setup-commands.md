# Workledger setup command reference

Use only for onboarding, configuration, routing checks, and diagnostics.

## Core commands

```text
workledger init
workledger config
workledger status
```

`workledger config` validates the effective config. `workledger status` checks config, local storage, env vars, routing, and adapter connectivity.

## Shell completion

Workledger generates completion scripts for Bash, Zsh, and Fish. PowerShell is unsupported. Completion generation writes to stdout and does not require valid config or initialized SQLite storage.

For Zsh, install the script and initialize its completion directory before `compinit`:

```zsh
mkdir -p ~/.zfunc
workledger completion zsh > ~/.zfunc/_workledger
```

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

For Bash, load completion in the current session:

```bash
source <(workledger completion bash)
```

Add that command to the user's Bash startup file when persistent completion is wanted.

For Fish, install the script in the standard user completion directory:

```fish
mkdir -p ~/.config/fish/completions
workledger completion fish > ~/.config/fish/completions/workledger.fish
```

Regenerate installed scripts after upgrading to a release that changes commands or flags.

## Adapter setup

### Jira Cloud

```text
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
```

Use for Atlassian Cloud with email + API token.

`--issue-prefix` is required for routing. It is the project key before the dash in a Jira issue key: `PROJ` for `PROJ-123`. It tells Workledger that issues beginning with `PROJ-` belong to the `main` Jira instance, so later reads, totals, and sync plans can select the correct Jira destination. It does not filter anything during setup and is not a display label.

If one instance handles several Jira projects, repeat the flag:

```text
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ --issue-prefix APP
```

### Jira Data Center

```text
workledger setup jira-data-center --instance dc --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
```

Use for self-hosted Jira with token auth and no Cloud email/API-token pairing.

The same routing rule applies: `--issue-prefix OPS` tells Workledger that issue keys such as `OPS-42` belong to the `dc` Jira instance. Repeat `--issue-prefix` for every Jira project owned by that instance.

### Clockify

```text
workledger setup clockify --workspace-id <workspace-id> --user-id <user-id> --api-key-env CLOCKIFY_API_KEY --project-map PROJ=Engineering
```

Validate project mappings before any push workflow.

## Environment variables

Use placeholders, never real token values:

```text
export JIRA_TOKEN=...
export JIRA_DC_TOKEN=...
export CLOCKIFY_API_KEY=...
```

## Routing checks

```text
workledger routing list
workledger route explain PROJ-123
workledger clockify mappings validate
```

Use a real issue key for `route explain` when possible.

After Jira setup, explain the expected result in plain language and verify it:

```text
workledger route explain PROJ-123
```

The result should identify the Jira family and instance configured with `--issue-prefix PROJ`. If it is unmatched, add the missing project prefix to the correct instance. If it is ambiguous, remove the duplicate ownership rather than guessing which instance should receive worklogs.

## Troubleshooting map

| Symptom | Layer | First command |
| --- | --- | --- |
| Config exists but setup seems ignored | config | `workledger config` |
| Missing token error | env | `workledger status` |
| Issue prefix routes incorrectly | routing | `workledger route explain PROJ-123` |
| Clockify project not selected | routing | `workledger clockify mappings validate` |
| SQLite/local DB write failure | storage | `workledger status` |
| Auth or API failure | connectivity | `workledger status` |

## Boundary

After onboarding succeeds, hand off to the worklog workflow for entries, totals, metadata, reconcile plans, or sync.

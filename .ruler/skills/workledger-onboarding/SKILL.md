---
name: workledger-onboarding
description: guide users through first-time workledger cli setup and tool configuration. use when chatgpt needs to help initialize workledger, configure yaml settings, create jira cloud, jira data center, or clockify adapter instances, prepare token environment variables, validate configuration, explain routing setup, run doctor diagnostics, or troubleshoot onboarding before any worklog entry or remote sync workflow.
---

# Workledger Onboarding

Help the user get `workledger` configured safely before they create, edit, or sync worklogs. Stay focused on setup and diagnostics. Do not draft worklog entries, mutate local worklogs, or run reconcile plans unless another skill or explicit user request takes over.

## Scope

Use this skill for:

- First-time `workledger init` setup.
- YAML configuration validation and summaries.
- Environment-variable/token setup for Jira Cloud, Jira Data Center, and Clockify.
- Adapter setup commands.
- Routing and Clockify project mapping checks.
- Local storage and connectivity diagnostics with `doctor`.
- Explaining what the next setup command should be.

Do not use this skill for normal worklog CRUD, coding-session worklog drafting, totals comparison, issue metadata refresh, or saved reconcile-plan execution.

## Onboarding flow

1. Identify the target integration: Jira Cloud, Jira Data Center, Clockify, or multiple adapters.
2. Collect only missing setup inputs: base URL, instance name, issue prefix, token environment-variable name, Clockify workspace/user IDs, and project mappings.
3. Prefer token environment-variable names over raw token values. Never ask the user to paste secrets unless their environment absolutely requires it.
4. Run or propose `workledger init` if no config exists.
5. Generate the narrowest `workledger setup ...` command for the chosen adapter.
6. Validate structure with `workledger config validate` before connectivity checks.
7. Show required environment variables with `workledger config env` or templates from `workledger config env --print-export-template` / `--dotenv-template`.
8. Run targeted diagnostics with `workledger doctor --local`, `--env`, `--routing`, or `--connectivity`; use `--all` only when broad troubleshooting is requested.
9. End with the current setup status and the next concrete command.

Load `references/setup-commands.md` when exact command syntax, selectors, or diagnostic boundaries matter.

## Safety rules

- Keep secrets out of chat and command history wherever possible. Refer to token variable names such as `JIRA_TOKEN`, not token values.
- Use `config validate` for structural checks. Use `doctor --connectivity` only after credentials are present.
- Treat YAML config as operator-managed. Explain changes before editing config files.
- Keep stdout clean when asking for JSON or machine-readable output; logs and progress belong on stderr.
- Prefer targeted diagnostics over noisy all-in-one checks.
- When the environment is sandboxed, check local storage paths and parent-directory writability before blaming adapter configuration.

## Response shape

For setup help, respond with:

1. Detected setup goal.
2. Missing inputs, if any.
3. Commands to run in order.
4. Validation or diagnostic interpretation.
5. Next concrete action.

For troubleshooting, include the failing command, likely failure layer (`config`, `env`, `routing`, `storage`, or `connectivity`), and the smallest follow-up diagnostic command.

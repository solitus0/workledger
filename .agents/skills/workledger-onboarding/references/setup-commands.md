# Workledger setup command reference

Use this reference for onboarding, configuration, and diagnostics only.

## Initialize and inspect configuration

```text
workledger init
workledger config validate
workledger config summary
workledger config env
workledger config env --print-export-template
workledger config env --dotenv-template
```

`config validate` checks structural config without requiring adapter connectivity. Use it before remote checks. `config env` shows required environment variables for configured adapters.

## Configure adapters

### Jira Cloud

```text
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
```

Use Jira Cloud when the base URL is an Atlassian Cloud site and authentication uses email plus API token.

### Jira Data Center

```text
workledger setup jira-data-center --instance dc --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
```

Use Jira Data Center when the organization hosts Jira itself and authentication is token-based without the Jira Cloud email/API-token pairing.

### Clockify

```text
workledger setup clockify --workspace-id <workspace-id> --user-id <user-id> --api-key-env CLOCKIFY_API_KEY --project-map PROJ=Engineering
```

Repeat or extend project mappings according to the CLI's supported syntax when multiple issue prefixes must route to Clockify projects. Validate mappings before any push workflow.

## Environment variable guidance

Prefer commands that reference env var names:

```text
export JIRA_TOKEN=...
export JIRA_DC_TOKEN=...
export CLOCKIFY_API_KEY=...
```

For user-facing setup instructions, use placeholders instead of real token values. For shell-specific output, prefer the CLI templates:

```text
workledger config env --print-export-template
workledger config env --dotenv-template
```

## Diagnostics

```text
workledger doctor
workledger doctor --local
workledger doctor --env
workledger doctor --routing
workledger doctor --connectivity
workledger doctor --all
```

Use targeted checks first:

- `--local`: local storage validation for `storage.sqlite_path`, DB-file writability, parent-directory writability, and SQLite sidecar creation viability.
- `--env`: missing or malformed environment variables.
- `--routing`: adapter routes, instance resolution, issue-prefix routing, and project mapping shape.
- `--connectivity`: credential and remote reachability checks.
- `--all`: broad investigation after targeted checks are insufficient.

## Routing inspection

```text
workledger routing list
workledger route explain PROJ-123
workledger clockify mappings validate
```

Use routing commands when the target adapter, instance, route profile, issue prefix, or Clockify project mapping is unclear. Use a real issue key for `route explain` when possible.

## Troubleshooting map

| Symptom | Likely layer | First follow-up command |
| --- | --- | --- |
| Config file exists but setup seems ignored | config | `workledger config summary` |
| Command complains about missing token | env | `workledger config env` |
| Issue prefix goes to wrong adapter | routing | `workledger route explain PROJ-123` |
| Clockify project not selected | routing | `workledger clockify mappings validate` |
| SQLite or local DB write failure | storage | `workledger doctor --local` |
| Auth or remote API failure | connectivity | `workledger doctor --connectivity` |

## Boundaries

Do not use onboarding commands as a shortcut for worklog creation or remote sync. After onboarding succeeds, hand off to the broader worklog workflow for local entries, totals, issue metadata, or reconcile plans.

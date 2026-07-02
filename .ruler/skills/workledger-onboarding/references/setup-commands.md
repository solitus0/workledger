# Workledger setup command reference

Use this reference for onboarding, configuration, and diagnostics only.

## Initialize and inspect configuration

```text
workledger init
workledger config
workledger status
```

`workledger config` validates the effective local config and reports the current configuration summary. `workledger status` runs config, local storage, env-var, routing, and adapter connectivity diagnostics.

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

For user-facing setup instructions, use placeholders instead of real token values. For shell-specific output, write explicit placeholder exports for the env var names configured through setup commands.

## Diagnostics

```text
workledger status
```

`workledger status` is the single onboarding diagnostic command. It reports:

- local storage validation for `storage.sqlite_path`, DB-file writability, parent-directory writability, and SQLite sidecar creation viability.
- missing or malformed environment variables.
- adapter routes, instance resolution, issue-prefix routing, and project mapping shape.
- credential and remote reachability checks.

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
| Config file exists but setup seems ignored | config | `workledger config` |
| Command complains about missing token | env | `workledger status` |
| Issue prefix goes to wrong adapter | routing | `workledger route explain PROJ-123` |
| Clockify project not selected | routing | `workledger clockify mappings validate` |
| SQLite or local DB write failure | storage | `workledger status` |
| Auth or remote API failure | connectivity | `workledger status` |

## Boundaries

Do not use onboarding commands as a shortcut for worklog creation or remote sync. After onboarding succeeds, hand off to the broader worklog workflow for local entries, totals, issue metadata, or reconcile plans.

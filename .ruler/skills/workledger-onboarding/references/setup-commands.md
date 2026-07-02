# Workledger setup command reference

Use only for onboarding, configuration, routing checks, and diagnostics.

## Core commands

```text
workledger init
workledger config
workledger status
```

`workledger config` validates the effective config. `workledger status` checks config, local storage, env vars, routing, and adapter connectivity.

## Adapter setup

### Jira Cloud

```text
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
```

Use for Atlassian Cloud with email + API token.

### Jira Data Center

```text
workledger setup jira-data-center --instance dc --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
```

Use for self-hosted Jira with token auth and no Cloud email/API-token pairing.

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

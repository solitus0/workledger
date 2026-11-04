# Configuration

[Documentation](README.md) · [Synchronization](synchronization.md)

## Initialize local storage

```sh
workledger init
workledger config
workledger status
```

`init` writes a starter configuration when missing and provisions or updates local SQLite storage. It reuses a valid existing config. `config` validates and summarizes the effective configuration; `status` checks config, storage, token environment variables, routing, and adapter connectivity.

| File | Default location | Owner |
| --- | --- | --- |
| Configuration | `~/.config/workledger/config.yaml` | Operator-managed YAML |
| Canonical worklogs and saved plans | `~/.local/share/workledger/worklogs.db` | Local SQLite storage |

## Set your workday

The starter config includes these active defaults, plus commented adapter examples:

```yaml
default_output: table
local_timezone: Europe/Vilnius
storage:
  sqlite_path: ~/.local/share/workledger/worklogs.db
worklogs:
  minimum_duration_seconds: 900
  daily_minimum_quota_seconds: 28800
  day_start: "08:00"
  day_end: "17:00"
  daily_lunch: "12:00-12:45"
```

Choose your actual IANA timezone before entering local timestamps. The quota informs context and daily remaining time. Workday boundaries and lunch guide automatic placement. See [placement rules](usage.md#place-work-into-free-time) for the distinction between fitting an interval and allowing a late start.

Set `storage.sqlite_path` to move storage deliberately. Keep the database and its parent directory writable; SQLite also needs access for WAL and sidecar files. Back up the database before moving or replacing it.

## Add Jira Cloud

```sh
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ
```

Create a token using Atlassian’s [API token guide](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/). The site URL shown above uses the guide’s token-without-scopes flow; scoped tokens use Atlassian’s API gateway URL instead.

Set the `JIRA_TOKEN` environment variable in the shell that launches Workledger. The config stores the variable’s name, not its value. Do not put token values in YAML or commit operator config.

`PROJ` tells Workledger that an issue such as `PROJ-123` belongs to the `main` Jira instance. This is an ownership routing rule, not a setup-time filter or display label. Repeat `--issue-prefix` for every project owned by that instance:

```sh
workledger setup jira-cloud --instance main --base-url https://example.atlassian.net --email user@example.com --token-env JIRA_TOKEN --issue-prefix PROJ --issue-prefix APP
```

The repeated-prefix example is an alternative to the first setup command. Do not run it to create the same instance twice; edit the existing operator-managed config when extending an instance.

## Add Jira Data Center

```sh
workledger setup jira-data-center --instance internal --base-url https://jira.example.com --token-env JIRA_DC_TOKEN --issue-prefix OPS
```

Create a personal access token using Atlassian’s [Data Center token guide](https://confluence.atlassian.com/enterprise/using-personal-access-tokens-1026032365.html), then set `JIRA_DC_TOKEN` in the launching shell. `OPS` routes keys such as `OPS-42` to the `internal` instance. Repeat the prefix flag for additional projects. Cloud uses email and API-token authentication; Data Center uses bearer-token authentication.

## Add Clockify

For interactive prompts:

```sh
workledger setup clockify
```

Or provide the required identifiers and mapping:

```sh
workledger setup clockify --workspace-id <workspace-id> --user-id <user-id> --api-key-env CLOCKIFY_API_KEY --project-map PROJ=Engineering
```

Generate a key in Clockify’s profile settings using the official [API key instructions](https://clockify.me/help/getting-started/manage-your-profile-settings), then set `CLOCKIFY_API_KEY` in the launching shell.

The [Clockify API reference](https://docs.clockify.me/) documents `GET https://api.clockify.me/api/v1/user`. Authenticate with the `X-Api-Key` header. Use the response’s `id` as `--user-id`, and the appropriate `activeWorkspace` or `defaultWorkspace` ID as `--workspace-id`. Confirm that the workspace is the one you intend to use. Workledger checks the authenticated user and the configured workspace in `status`.

`Engineering` in `--project-map PROJ=Engineering` is the **project name**, not its ID. It must exactly match one project in that workspace, including case. Multiple projects with that name are ambiguous; resolve the duplicate name before mapping validation or reconciliation. Project mappings select the Clockify project for issue prefixes.

Clockify is optional. Jira-only setups do not need Clockify mappings, and mapping warnings are reported only when Clockify is active.

## Verify routing and connectivity

```sh
workledger routing list
workledger route explain PROJ-123
workledger status
```

For Clockify, also run:

```sh
workledger clockify mappings validate
```

The Jira route result should identify the intended adapter family and instance. Fix unmatched or ambiguous ownership instead of guessing a destination. The same prefix cannot be owned by two instances in one Jira family. Reporting profiles can route reporting copies to configured target issues; inspect the starter YAML reference and `route explain` before using them.

Each setup command supports interactive prompts and flags. Use `workledger setup <adapter> --help` for its exact inputs.

## Import before pushing

When status succeeds, complete the [first import](synchronization.md#first-import) from every intended source. Pull, review, and apply the entire date window before the first push. Remote-only rows outside the imported history can otherwise be treated as cleanup by push reconciliation.

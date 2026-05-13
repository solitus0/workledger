# Glossary

This file owns shared terminology and grammar.

## Canonical Terms and Grammar

This document defines shared terminology and grammar used across MVP and deferred docs.

### Adapter family naming

The same adapter family appears in both YAML config and CLI surfaces.

Rules:

* YAML section keys use `snake_case`, for example `jira_cloud`, `jira_data_center`, and `clockify`
* CLI command groups and `--adapter` values use `kebab-case`, for example `jira-cloud`, `jira-data-center`, and `clockify`
* docs must use the YAML form when describing config keys and the CLI form when describing commands or flag values

### Adapter target identity

Current and deferred docs use the adapter family plus configured target name from YAML as the stable identifier for a remote target.

Rules:

* use `target adapter instance` when referring to configured remote targets
* `configured adapter instance` means a named target that exists in YAML config for the selected adapter family
* multi-instance adapters such as Jira Cloud and Jira Data Center expose explicit instance names
* singleton adapters such as Clockify expose one configured target owned by that adapter section
* do not refer to `target instance ID` unless the config schema explicitly adds one later
* do not refer to an `active` adapter instance or target unless the config schema explicitly adds enable or disable state later

### Issue-key grammar

Canonical issue keys use:

* `<PROJECTKEY>-<NUMBER>`

Rules:

* `PROJECTKEY` starts with an uppercase ASCII letter and may then contain uppercase ASCII letters or digits
* `NUMBER` is one or more decimal digits and must not start with `0`
* examples: `AAPP-1`, `ITO2-15`

### Adapter-scoped routing

Deferred routing is adapter-scoped.

Rules:

* routing config is grouped by adapter family
* each adapter family owns its routing rule shape and validation rules
* a selected reconcile flow chooses the adapter family before routing resolution runs
* do not assume every adapter family resolves targets from issue keys alone

### Jira routing-prefix grammar

Jira-family routing prefixes use:

* an uppercase ASCII letter followed by zero or more uppercase ASCII letters or digits

Rules:

* Jira routing prefixes are matched exactly
* Jira routing values refer to configured Jira adapter targets within the selected Jira adapter family

### Health checks with zero configured targets

When a health-check command such as `workledger status` resolves an empty target set after successful config validation:

* the command succeeds with exit code `0`
* the output is an empty result set in the selected format

This keeps health commands deterministic for valid but not-yet-configured MVP setups.

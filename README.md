# workledger

`workledger` is a small Go CLI for managing canonical local worklogs in SQLite.
It is designed to keep local worklog state authoritative while supporting inspection, correction, planning, and adapter reconciliation workflows.

## What it does

Current documented capabilities include:

* bootstrap local config and SQLite storage
* validate configuration
* create, list, search, inspect, update, delete, and restore local worklogs
* inspect planning context and apply local batch worklog changes
* inspect adapter status and compare local totals with remote systems
* refresh local issue metadata from Jira adapters
* create, inspect, apply, and retry reconcile plans for remote adapter sync

## Command surface

Core commands:

* `workledger init`
* `workledger config validate`
* `workledger version`

Local worklogs:

* `workledger worklogs list`
* `workledger worklogs search <query>`
* `workledger worklogs show <id>`
* `workledger worklogs add`
* `workledger worklogs update <id>`
* `workledger worklogs delete <id>`
* `workledger worklogs restore`
* `workledger worklogs context`
* `workledger worklogs shift`
* `workledger worklogs apply`

Planning and adapter workflows:

* `workledger status`
* `workledger totals`
* `workledger issue-metadata list`
* `workledger issue-metadata refresh`
* `workledger plan reconcile`
* `workledger plan show`
* `workledger plan list`
* `workledger plan apply`
* `workledger plan retry`

## Design intent

* YAML config is the source of truth for operator-managed configuration
* SQLite is the source of truth for canonical local worklogs and runtime state
* local worklogs remain authoritative even when adapter sync is used
* output modes are `table` and `json`

## Status

This repository is specs-first.
The canonical product and CLI contract lives under [`specs/`](specs/), especially:

* [`specs/product.md`](specs/product.md)
* [`specs/api.md`](specs/api.md)
* [`specs/constitution.md`](specs/constitution.md)
* [`specs/ux.md`](specs/ux.md)

Deferred or out-of-scope areas currently include:

* `workledger tui`
* delivery correlation

## Development

There is no full build pipeline committed yet.
Useful doc-oriented commands:

```sh
rg -n "workledger|worklog|route" specs
sed -n '1,120p' specs/api.md
git diff -- specs README.md
```

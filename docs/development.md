# Development

[Documentation](README.md)

Workledger’s primary interface is an agent-friendly CLI. The optional TUI shares its services and local ledger; keep command workflows usable independently of interactive terminal rendering.

## Build and run

Install the Go toolchain specified by [go.mod](../go.mod), then:

```sh
git clone https://github.com/solitus0/workledger.git
cd workledger
go build -o bin/workledger ./cmd/workledger
./bin/workledger version
```

For a quick command without a separate build:

```sh
go run ./cmd/workledger --help
```

Normal commands use the operator’s configuration and database. Initialize those intentionally before running `tui`. The documentation capture uses its own temporary database and never loads operator configuration.

## Package boundaries

| Location | Responsibility |
| --- | --- |
| `cmd/workledger` | Process startup and dependency wiring |
| `internal/cli` | Cobra commands, flag parsing, output, and exit codes |
| `internal/tui` | Bubble Tea, Bubbles, and Lip Gloss presentation |
| `internal/config` | YAML loading, validation, and routing configuration |
| `internal/worklogs` | Canonical local worklogs, issue catalog, context, and placement rules |
| `internal/issues` | Remote issue metadata refresh |
| `internal/presets` | Reusable worklog presets |
| `internal/reconcile` | Saved plans, review, apply, and retry orchestration |
| `internal/adapter` | Shared capabilities and Jira/Clockify adapters |
| `internal/store/sqlite` | Storage, schema bootstrap, and transactions |
| `specs` | Canonical functional and non-functional contracts |
| `docs` | Task-focused user and development documentation |
| `.ruler` | Canonical repository guidance and agent skills |

The TUI consumes narrow service interfaces. It does not parse CLI output or query SQLite directly. Keep business behavior reusable between the CLI and TUI.

## Checks

CI runs module tidiness, vet, and tests:

```sh
go mod tidy
git diff --exit-code -- go.mod go.sum
go vet ./...
go test ./...
```

Run focused packages while iterating, then the relevant broader checks. Use the race detector for changes to concurrent paths when supported. Keep tests adjacent to the packages they cover.

Do not run dependency updates merely to change documentation. Use the versions already pinned in the repository.

## Contracts and generated guidance

Start at [functional](../specs/functional.md) and [non-functional](../specs/non-functional.md) requirements when changing behavior. Keep affected help, examples, docs, and tests aligned in the same change.

Repository guidance is authored in [.ruler/AGENTS.md](../.ruler/AGENTS.md). Ruler generates `AGENTS.md`, `CLAUDE.md`, and `.github/copilot-instructions.md`. Agent skill sources live in `.ruler/skills`; keep onboarding and session-worklog skills aligned with their contracts.

Edit `.ruler/AGENTS.md`, then regenerate the consumer guidance from the repository root using the installed Ruler CLI:

```sh
ruler apply --agents codex,claude,copilot --no-skills --no-mcp --no-gitignore --no-backup --local-only
```

This scoped command regenerates repository guidance. The consumer files are gitignored in this repository; commit the canonical source and any tracked generated updates together, following the repository’s tracking policy.

## Documentation media

[Media and branding](media.md) explains the finalized Ledger W assets, screenshot fixture, and opt-in capture command. The capture build tag excludes its generator from ordinary builds and tests.

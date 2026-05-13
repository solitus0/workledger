# Repository Guidelines

## Project Structure & Module Organization

This repository is currently specs-first. The product contract lives under `specs/`, with canonical top-level docs for product, architecture, API, UX, security, testing, and glossary plus feature bundles under `specs/features/`. Start at `specs/product.md` and `specs/constitution.md`. There is no committed `cmd/`, `internal/`, or `tests/` tree yet; when implementation lands, keep the Go CLI small and map package names to the documented command surface such as `workledger plan` and `workledger worklogs`.

## Build, Test, and Development Commands

There is no build pipeline checked in yet. For now, contributors mainly edit and review Markdown:

- `rg -n "workledger|worklog|route" specs` finds affected specs quickly.
- `sed -n '1,120p' specs/api.md` reads a targeted section without loading full files.
- `git diff -- specs AGENTS.md` reviews contract changes before commit.

When Go code is added, prefer standard Go commands over custom wrappers: `go test ./...` for tests and `go run ./cmd/workledger version` for a quick CLI smoke test.

## Coding Style & Naming Conventions

Keep prose deterministic and specific. Reuse the canonical terms from `specs/glossary.md`; for example, write issue keys as `<PROJECTKEY>-<NUMBER>` and refer to configured targets by Jira instance name. Use short sections, flat bullet lists, and sentence case headings inside docs. For future Go code, follow `gofmt`, keep business logic outside Cobra command handlers, and prefer small packages over broad utility layers.

## Testing Guidelines

For documentation changes, verify internal links, command names, and consistency with the canonical contracts in `specs/`. Avoid introducing commands or exit codes that contradict `specs/api.md` and `specs/testing.md`. For future Go tests, use table-driven tests and keep them adjacent to the package under test with `_test.go` suffixes.

## Contract Discipline

Never allow implementation logic and the canonical specification in `specs/` to drift.
If behavior, flags, output, validation, or workflow semantics change, update the relevant spec in the same task.
Do not add or preserve trivial, self-evident spec detail that does not protect a meaningful contract.

## Skill Alignment

Future CLI work around local worklogs, planning, status, or reconcile flows must use the supporting skill at `.codex/skills/worklog-orchestrator/SKILL.md`.
Do not let the skill drift from `specs/` or the Mermaid sequence diagrams under `sequence_diagrams/`.
If one changes, update the other in the same task.

## Commit & Pull Request Guidelines

Current history uses short, imperative subjects such as `plan refinement`. Keep commit titles concise, present tense, and scoped to one change. Pull requests should explain which docs section changed, why the contract changed, and whether the change affects MVP or deferred scope. Include sample command output only when the change alters CLI behavior or JSON/table output.

## Security & Configuration Tips

Do not commit real Jira credentials or local config files. Treat `~/.config/workledger/config.yaml` as an operator-local path referenced by docs, not a tracked artifact.

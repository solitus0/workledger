# Repository guidelines

## Start with the contract

Read `specs/functional.md` and `specs/non-functional.md` before changing behavior. Keep implementation, help, examples, and canonical specs aligned in the same task. Do not preserve trivial spec detail that protects no meaningful contract.

Workledger is maintained by a single author. The README is the product entry point. Task-focused user guides live under `docs/`; begin at `docs/README.md`. Build, test, package ownership, and maintenance are documented in `docs/development.md`.

## Code organization and checks

`cmd/workledger` owns startup and wiring. `internal/cli` owns Cobra parsing, output, and exit mapping; `internal/tui` owns the terminal frontend. Keep business rules in shared services and SQLite ownership in `internal/store/sqlite`. The TUI must not query SQLite or parse CLI output directly.

Use standard Go tools and follow the complete verification sequence owned by `docs/development.md#checks`.

Follow `gofmt`, use small packages and narrow consumer-owned interfaces, and keep tests adjacent to their packages. Prefer table-driven tests when the cases protect observable behavior. Do not update dependencies for documentation-only changes.

## Documentation and media

Use canonical command names, issue keys in `<PROJECTKEY>-<NUMBER>` form, and configured Jira instance names. Write deterministic prose with short sections and clear examples. Give each fact one owner; link from the README to detailed guides.

Keep published terminal captures faithful to production rendering. Use the fictional fixture and isolated capture flow documented in `docs/media.md`; never capture personal worklogs or point the showcase generator at operator storage. Preserve the finalized Ledger W geometry and its theme/size treatments.

Verify local links, anchors, commands, and flags after documentation changes. Add screenshots for visible TUI changes when the published image no longer represents the interface.

## Skill alignment

CLI work around local worklog creation from the current session must use `.ruler/skills/session-worklog-creator/SKILL.md`. CLI work around setup or status diagnostics must use `.ruler/skills/workledger-onboarding/SKILL.md`.

Keep these skills and the relevant specs aligned in the same task when either contract changes. Duration remains explicit for session worklogs; estimates never determine logged duration. Import the complete intended remote window before the first push.

## Changes and credentials

Use short, imperative commit subjects scoped to one change. Record the problem, resulting behavior, and relevant verification; identify changed contracts and include before/after images for interface changes.

Do not commit real Jira credentials, operator configuration, or personal databases. Adapter configuration stores token environment-variable names. `~/.config/workledger/config.yaml` is an operator-local path, not a tracked artifact.

## Collaboration boundary

When the user asks questions, requests suggestions, or asks for an opinion, answer directly without implementing unless explicitly asked to implement, apply, or make the changes.

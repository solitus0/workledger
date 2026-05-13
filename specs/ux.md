# UX

This file owns operator workflows, command interaction patterns, output rendering, and progress presentation.

## Agent Workflows

This file defines safe human and agent command sequences for the MVP local-only workflow.

### Single-entry creation

Use this flow when creating one new local worklog in the current MVP:

1. Run `workledger worklogs list` for the target day or range.
2. Run `workledger worklogs add` with explicit issue, start time, duration, and description.
3. Re-run `workledger worklogs show <id>` or `workledger worklogs list` for the same target day or range to verify the final state.

Multi-entry planning and batch local adds are deferred to follow-up local scopes.

### Deferred multi-entry planning

Use this flow when the request needs more than one new local worklog after the deferred batch commands land:

1. Run `workledger worklogs context` for the target day or range.
2. Have the operator or AI agent build one raw `worklogs apply` payload from the returned free slots and planning hints.
3. Optionally run `workledger worklogs apply --dry` to validate the chosen payload without writing.
4. Run `workledger worklogs apply` only after the payload matches the intended local result.

Prefer this flow over emitting many `worklogs add` commands when slotting and allocation matter.
`worklogs context` is inspection-only; slotting, splitting, and description selection happen outside the CLI before `apply`.

### Existing-row correction

Use this flow when the target worklog already exists and only one row needs correction:

1. Run `workledger worklogs show <id>` to inspect the current canonical record.
2. Run `workledger worklogs update <id>` with explicit patch flags.
3. Re-run `workledger worklogs show <id>` or `workledger worklogs list` for the affected day or range to verify the final state.

Use `update` for field changes to one row.
Do not use `draft` or `apply` for existing-row corrections in MVP.

### Delete workflow

Use this flow for safe delete operations:

1. For single delete, inspect the row with `workledger worklogs show <id>`.
2. For filtered batch delete, inspect the target set with `workledger worklogs list`.
3. Run `workledger worklogs delete --dry` for filtered batch delete preview.
4. Execute the real delete only after the previewed set is correct.

Filtered batch delete requires `--yes`.
`--dry` is only for filtered batch delete mode.

Use this flow for safe restore operations:

1. Inspect tombstones with `workledger worklogs list --only-deleted`.
2. Run `workledger worklogs restore --dry` with the same selector scope.
3. Execute `workledger worklogs restore --yes` only after the previewed restore set is correct.

### Command selection rules

Prefer:

* `worklogs list` with an explicit time selector for current-state inspection before add or delete
* `worklogs add` for one local add
* `worklogs update` for one-row corrections
* `worklogs delete` for local removal, with tombstone retention by default and `--hard` as explicit bypass
* `worklogs restore` for selector-based tombstone recovery back into active local rows


## Progress Reporting

This document defines deferred progress-reporting requirements for long-running commands that perform remote batch HTTP work.

The current MVP is local-only and does not require progress reporting.
Shared stdout, stderr, output-mode, and exit-code rules remain in [api.md#cli-and-acceptance](api.md#cli-and-acceptance).

### Goals

Progress reporting exists to:

* show that a long-running remote command is still making forward progress
* give the operator a rough sense of remaining work without changing the command result contract
* keep machine-readable stdout stable

### Applicable commands

Deferred progress reporting may be used by commands that perform multiple remote HTTP operations, including:

* `workledger plan reconcile`
* `workledger plan apply`
* `workledger plan retry`
* `workledger totals`
* future adapter-scoped health or import commands when they perform batch remote work

Single fast requests should not render progress UI.

### Output contract

Live progress output must go to stderr only.
It must never be mixed into stdout.

Rules:

* final command results still render only in the selected stdout output mode
* `--output json` must remain valid JSON on stdout with no progress fragments
* progress rendering must be best-effort presentation only and must not affect command success, failure, or exit code
* when stderr is not writable, the command still runs without progress UI

### CLI surface

Deferred remote commands should share one common flag:

* `--progress=auto|bar|plain|off`

Mode rules:

* `auto` is the default
* `auto` renders an animated progress bar only when stderr is a TTY; otherwise it stays silent
* `bar` forces interactive bar rendering and falls back to `plain` when stderr is not a TTY
* `plain` emits line-oriented progress summaries to stderr without cursor control
* `off` disables live progress output

This keeps one stable operator-facing control across planning, apply, and retry flows.
`workledger totals` uses the same flag and stderr-only behavior.

### Progress model

Progress should track logical work units, not raw transport attempts.

Rules:

* retries caused by `429` or transient transport failures must not create fake forward progress
* when a command has a stable work-set before execution starts, progress totals must use that stable work-set
* when the exact remote request count is unknown up front because of pagination, replace cleanup, or safety re-checks, progress may render without a percentage bar and should instead show phase plus completed-unit counts

Recommended logical units by command:

* `plan apply` and `plan retry`: saved plan items
* `plan reconcile --push`: resolved reconcile scopes or target-instance fetch tasks
* `plan reconcile --pull`: remote page or source-scope fetch tasks when page count is not known in advance
* `totals`: configured adapter targets for bare totals and discovered Jira issues for explicit Jira totals after search

Operator-visible progress should prefer stable scope counts over volatile HTTP-call counts.

### Rendering requirements

Interactive bar mode should show at minimum:

* current phase
* completed logical units
* total logical units when known
* failed logical units so far
* elapsed time

Plain mode should emit:

* one start line
* periodic summary lines only when counts change materially or a time threshold is crossed
* one final completion line with succeeded, failed, and uncertain counts when relevant

Progress text should use deterministic wording so operators can understand logs quickly.

### Phase guidance

Commands may expose coarse phases such as:

* `discovering`
* `fetching`
* `applying`
* `finalizing`

One logical unit may internally perform more than one HTTP request.
For example, a reporting `replace` apply item may re-list remote rows, delete them, and then create replacement rows.
The progress model should still advance by saved plan item completion rather than by each sub-request.

### Failure handling

Progress UI must tolerate mixed outcomes.

Rules:

* per-unit failures increment failure counts without stopping unrelated eligible work
* a command that ends in partial success still renders its normal final stdout payload and exit code `6` where already defined
* interrupted or crashed commands do not need resumable progress state; persisted attempt history remains the source of truth

### Non-goals

This feature does not:

* change concurrency rules
* expose per-request debug logs in the progress UI
* guarantee an exact remaining-time estimate
* replace normal structured logs or diagnostics on stderr

### Detailed proposal

1. [Progress bars for `plan reconcile` and `plan apply`](ux.md#progress-bars-for-plan-reconcile-and-plan-apply)


## Progress Bars For `plan reconcile` and `plan apply`

This document applies the shared contract from [Progress reporting](#progress-reporting) to deferred reconcile flows.

### Goal

Define operator-visible progress reporting for deferred reconcile flows with two useful views:

* outer progress by reconcile scope, which usually maps to one issue and window
* inner progress by worklog rows or row operations inside that scope

One issue may contain multiple worklogs.

### Constraints From Existing Docs

The proposal must follow the existing deferred reconcile contract:

* `plan reconcile` groups rows into reconcile scopes, computes one result per scope, and persists one saved plan item per scope
* `plan apply` executes at the saved-plan-item level
* goroutines are allowed only for remote adapter I/O during reconcile planning
* push reconcile must not spawn per-scope goroutines after remote reads complete
* apply concurrency units are saved plan items, not sub-steps within one plan item
* at most one active goroutine may execute remote I/O for a given resolved target issue key at a time
* user-facing output stays on stdout
* logs, diagnostics, and progress stay on stderr
* `--output json` must keep stdout as valid JSON with no mixed progress lines

### Core Model

Use `scope` as the internal top-level progress unit.

Why:

* one saved plan item represents one reconcile scope, not one worklog
* apply concurrency is already defined per saved plan item
* one scope usually aligns with one issue plus one selected reconcile window
* reporting flows may collapse multiple source issues into one target issue, so `scope` is more accurate than `issue`

UI wording:

* show `issues` when every scope maps to exactly one visible issue
* show `scopes` when one target scope aggregates multiple local issues

### Output Contract

Progress rendering writes to stderr only.

Recommended reporters:

* `noop` for `--progress=off`
* `plain` for `--progress=plain`
* `tty` for `--progress=bar` or `--progress=auto` on TTY stderr

Rules:

* never write progress to stdout
* reporter choice depends on `--progress` and stderr TTY detection, not on stdout output mode
* workers emit structured progress events through a channel
* one renderer goroutine owns terminal output
* workers never print directly

### Event Model

Use one in-memory event stream:

```go
type Event struct {
    Kind          EventKind
    Phase         string
    ScopeID       string
    ScopeLabel    string
    PlannedAction string
    ScopeDone     int
    ScopeTotal    int
    WorkDone      int
    WorkTotal     int
    Message       string
}
```

Notes:

* `ScopeLabel` is usually an issue key or target issue plus window label
* `WorkTotal` may be unknown at first and filled later for apply cleanup steps
* progress state remains in memory only and is never persisted

### `plan reconcile`

#### Shared behavior

Render progress by phase instead of forcing one misleading percentage across the full command.

Use:

* spinner or status line for phases with unknown totals
* bars only when totals are known

#### Push reconcile

Recommended phases:

1. local selection and scope grouping
2. remote fetch and advisory checks by target adapter instance
3. single-threaded diff, classification, and saved-plan persistence

Progress behavior:

* phase 1 uses a spinner because selected scope totals are not final yet
* phase 2 reports target-instance fetch progress, not per-scope progress
* phase 3 shows two counters:
  * scopes classified / total scopes
  * local worklogs processed / total selected local worklogs

Reasoning:

* remote work happens concurrently only per resolved target adapter instance
* local grouping, diffing, classification, and persistence remain single-threaded
* per-scope goroutine progress would conflict with the existing planning rules

#### Pull reconcile

Recommended phases:

1. remote fetch from the selected adapter source scope
2. normalization into canonical candidate rows
3. scope build, merge classification, and saved-plan persistence

Progress behavior:

* phase 1 uses a spinner because remote total visibility may be unknown
* phase 2 shows normalized rows / fetched remote rows when that count is known
* phase 3 shows two counters:
  * scopes classified / total scopes
  * normalized candidate rows processed / total normalized rows

### `plan apply`

#### Aggregate view

Always show two aggregate counters when work exists:

* scopes completed / total executable saved plan items
* worklogs completed / total row operations

Eligible work is built only from `ready` items with `execution_state=not_attempted`.

#### Worklog accounting by action

Inner worklog totals should map to real row operations:

* `merge`: payload row count
* `create`: payload row count
* `replace`: remote rows deleted at apply time plus payload rows created
* `delete`: remote rows deleted at apply time

This matters because push apply may need to rediscover current remote rows inside the saved issue and window scope before cleanup.

#### Concurrency alignment

Apply progress must match the existing execution model:

* schedule concurrency only at saved-plan-item level
* never split one saved plan item into concurrent sub-work
* keep target-issue serialization intact
* treat per-item failures as result data while other eligible items continue

That means:

* one scope moves through states such as queued, running, succeeded, failed, or uncertain
* inner worklog progress advances only from that scope's active worker
* aggregate progress is the sum of completed scope and worklog events across all workers

### TTY Layout

Example interactive layout:

```text
phase     apply plan 42
scopes    5/12  [##########------]
worklogs  17/61 [#####-----------]

active
AAPP-123  replace  delete 3/5  create 2/4
BAPP-91   merge    6/6
```

Layout rules:

* keep active scope rows sorted deterministically by scope label
* redraw only from the renderer goroutine
* cap visible active rows to a small fixed number if needed

### Non-TTY Layout

For non-interactive stderr, emit periodic checkpoints instead of redraws.

Example:

```text
[progress] plan apply 42 scopes=3/12 worklogs=11/61 active=2
[progress] running AAPP-123 action=replace delete=3/5 create=1/4
```

This keeps logs readable and avoids terminal control sequences in CI.

### Suggested Implementation Shape

Small package layout:

* `internal/progress/reporter.go`
* `internal/progress/tty.go`
* `internal/progress/plain.go`
* `internal/progress/event.go`

Suggested interfaces:

```go
type Reporter interface {
    Start(ctx context.Context, meta Meta)
    Event(Event)
    Finish(Result)
}
```

Guidelines:

* use stdlib plus ANSI escape sequences first
* avoid a dependency unless terminal behavior proves difficult
* keep SQLite writes serialized through the existing writer path
* keep progress state out of persistence models

### Recommendation

Implement progress in two steps:

1. add `plain` progress on stderr with aggregate scope and worklog counters
2. add `tty` redraw support once reconcile and apply flows are stable

This gives immediate operator value without coupling early command behavior to terminal-specific rendering.

---
name: grill-me
description: relentlessly interview the user about a plan, architecture, implementation design, migration, product idea, or technical decision until shared understanding is reached. use when the user asks to be grilled, stress-test a plan, review a design tree, resolve decisions branch by branch, or wants recommended answers alongside each question. when filesystem access is available, persist session decisions in the analyzed project root rather than the skill directory, and use bundled codex hook templates when the user asks for a hooks-enabled version.
---

# Grill Me

Interrogate the user's plan until the decision tree is explicit, contradictions are removed, and the remaining unknowns have owners or validation steps.

## Operating mode

For each turn:
1. Identify the next unresolved decision that blocks the most downstream work.
2. Explain briefly why it matters and what depends on it.
3. If the answer can be discovered from the codebase, inspect the codebase instead of asking.
4. Ask one focused question.
5. Provide a recommended answer immediately after the question.
6. Record the accepted answer, rejected alternatives, assumptions, evidence, and downstream implications when filesystem access is available.

Do not ask multiple unrelated questions at once. Do not jump to low-level details before resolving higher-level dependencies. Continue until the plan can be summarized without contradictions.

## Question format

Use this structure by default:

```markdown
## Branch: [decision area]

**Why this matters:** [one or two sentences]

**Question:** [one focused question]

**Recommended answer:** [specific recommendation, including tradeoffs]

**Depends on / unlocks:** [dependencies and downstream choices]
```

After the user answers, either accept the answer and move to the next branch, or challenge it if it conflicts with prior decisions or codebase evidence.

## Codebase-first rule

When a question can plausibly be answered by repository inspection, inspect before asking. Examples:
- existing framework, language, package versions, or architecture style
- current authentication, persistence, queue, cache, or deployment patterns
- established naming, testing, error handling, observability, or API conventions
- prior ADRs, docs, migrations, tests, or CI workflows

Summarize codebase evidence in the answer and distinguish it from assumptions.

## Persistence rules

When filesystem access is available, persist grill artifacts in the project being analyzed, never in the skill installation directory.

Default private output directory:

```text
<project-root>/.grill/
```

Use this team-visible directory only if the user asks for committed/shared records:

```text
<project-root>/docs/decisions/grill/
```

Maintain these files when possible:
- `.grill/current-session.md` - human-readable live state
- `.grill/decisions.jsonl` - append-only accepted decisions, rejected alternatives, assumptions, and validation tasks
- `.grill/open-questions.md` - unresolved branches and blockers
- `.grill/codebase-findings.md` - evidence discovered from repository inspection

If the repository is unavailable, keep the same structure in the conversation and offer a concise exportable summary.

## Decision ledger schema

Append one JSON object per line to `.grill/decisions.jsonl`:

```json
{"type":"decision","topic":"storage","decision":"use postgres","rationale":"existing infrastructure and transactional requirements","rejected":["sqlite","document store"],"confidence":"medium","depends_on":["deployment topology"],"unlocks":["migration design","backup policy"]}
```

Allowed `type` values:
- `decision`
- `assumption`
- `open_question`
- `codebase_finding`
- `validation_task`

Keep entries factual and compact. Do not invent acceptance as a decision; mark it as `recommended` or `open_question` until the user confirms.

## Codex hooks support

If the user asks for the hooks-enabled version, Codex hook setup, or persistent grill sessions, use the templates in `references/codex-hooks.md` and the scripts in `scripts/`.

Recommended hook behavior:
- `SessionStart`: load `<project-root>/.grill/current-session.md` into context when it exists.
- `PostToolUse`: append tool-use evidence to `<project-root>/.grill/tool-events.jsonl` and optionally update `codebase-findings.md`.
- `Stop`: ensure `.grill/` exists and remind the agent to update the decision ledger before the next turn.
- `PreToolUse`: optionally restrict write operations to project-root `.grill/` or `docs/decisions/grill/` when the session is in grill mode.

Hooks are support infrastructure. The skill still owns the interrogation logic and must explicitly produce decision-ready summaries.

## Completion criteria

Finish only when:
- all blocking decisions are resolved or explicitly deferred
- assumptions are named and have validation paths
- codebase-derived answers are separated from guesses
- the user has a final brief containing decisions, tradeoffs, risks, and next actions

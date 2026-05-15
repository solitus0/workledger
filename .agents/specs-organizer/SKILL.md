---
name: specs-organizer
description: organize messy software specification notes, requirements documents, acceptance criteria, plans, decisions, tasks, or pasted feature specs into a stupid-simple flat requirements structure with exactly specs/functional.md and specs/non-functional.md. use when the user wants to clean up, regroup, deduplicate, flatten, audit, or rewrite specs for coding agents such as codex, especially when they ask for atomic checkboxes, logical groups, strictdoc-inspired markdown, or reduced documentation redundancy.
---

# Specs Organizer

Transform messy product/specification material into two flat Markdown files:

```text
specs/
  functional.md
  non-functional.md
```

Prioritize simplicity, auditability, low context size, and coding-agent usefulness.

## Alpha tester behavior

Treat the agent as an alpha tester for the specification workflow.

- Report actual usage of the `workledger` CLI tool when that usage informs the organized specification output.
- Report bugs, contradictions, missing constraints, or awkward workflow edges discovered while organizing the specs.
- Keep usage notes and bug reports separate from the canonical requirements so the output files stay clean and auditable.

## Core output contract

Always produce only these two target files unless the user explicitly asks otherwise:

- `specs/functional.md`
- `specs/non-functional.md`

Use logical `##` groups inside each file. Do not create nested feature folders, separate acceptance files, technical files, business files, data files, ADR files, matrices, or traceability docs unless explicitly requested.

## Requirement format

Use one checkbox per auditable rule:

```md
- [ ] FUNC-001: Single functional rule.
- [ ] NFR-001: Single non-functional rule.
```

Rules:

- Write exactly one rule per checkbox.
- Make each rule standalone and testable.
- Use stable IDs and never intentionally duplicate an ID.
- Use `FUNC-###` for functional requirements.
- Use `NFR-###` for non-functional requirements.
- Preserve important domain terms, command names, flags, exit codes, file names, and data names exactly.
- Avoid rationale, background prose, stakeholder analysis, and acceptance paragraphs.
- Do not duplicate the same rule across both files.

## Classification

Put a rule in `functional.md` when it describes a user-visible capability or behavior:

- commands the tool provides
- operations users can perform
- flags or selectors users can pass
- returned records or visible command behavior
- create, list, search, show, update, delete, restore, import, export, sync, validate flows
- explicit out-of-scope behavior if needed to constrain implementation

Put a rule in `non-functional.md` when it describes a system guarantee, implementation constraint, or quality rule:

- storage and persistence
- identity rules
- configuration rules
- data normalization
- validation rules
- conflict detection
- output contracts
- exit codes
- atomicity and transaction guarantees
- determinism
- security, performance, availability, compatibility, observability, logging

If a sentence mixes both types, split it.

Example:

Input:

```text
Local worklogs feature owns canonical local worklog CRUD on SQLite.
```

Output:

```md
# functional.md
- [ ] FUNC-001: Workledger shall provide canonical local worklog CRUD.

# non-functional.md
- [ ] NFR-001: SQLite shall be the canonical local storage for worklogs.
```

## Grouping rules

Use fixed, flat groups. Add a new requirement under the most specific existing group. Create a new group only when no existing group fits.

Recommended functional groups:

```md
## Product Scope
## Workspace Bootstrap
## Configuration Commands
## Worklog Listing
## Worklog Search
## Worklog Details
## Worklog Creation
## Worklog Update
## Worklog Delete
## Batch Delete
## Worklog Restore
## Out of Scope
```

Recommended non-functional groups:

```md
## Storage
## Identity
## Configuration
## Time Handling
## Input Validation
## Conflict Validation
## Tombstones
## Output Contracts
## Exit Codes
## Atomicity
## Determinism
```

Only include groups that contain at least one requirement.

## Deduplication rules

When multiple source files say the same thing:

- Keep one canonical requirement.
- Prefer the clearest and most specific wording.
- Preserve stricter constraints over weaker duplicates.
- Merge near-duplicates only when they express the same atomic rule.
- Do not merge separate conditions into one checkbox.

Bad:

```md
- [ ] FUNC-001: Add validates issue, time, duration, description, duplicates, overlaps, and returns the created record.
```

Good:

```md
- [ ] FUNC-001: The add worklog command shall require an issue key.
- [ ] FUNC-002: The add worklog command shall require a duration.
- [ ] FUNC-003: The add worklog command shall require a description.
- [ ] NFR-001: Duplicate detection shall evaluate all active local worklogs.
- [ ] NFR-002: Overlap detection shall evaluate all active local worklogs.
- [ ] FUNC-004: The add worklog command shall return the created canonical worklog.
```

## Output style

When returning reorganized requirements in chat, provide:

1. A short note listing assumptions and any skipped ambiguous items.
2. `specs/functional.md` in one fenced Markdown block.
3. `specs/non-functional.md` in one fenced Markdown block.

When writing files for the user, create both files under a `specs/` directory and link them.

Keep language direct and boring. Do not add explanatory documentation inside the generated files except:

```md
# Functional Requirements

Status: Draft

Rule format:
- [ ] FUNC-000: Single auditable functional rule.

Placement rule:
- Add new requirements under the most specific existing group.
- Create a new group only when no existing group fits.
- Do not duplicate a rule across groups.
```

Use the analogous header for non-functional requirements.

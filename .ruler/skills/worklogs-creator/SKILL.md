---
name: worklogs-creator
description: Allocate, preview, and create local Workledger worklogs from the current session, user-provided notes, or a Worklogs Scavenger handoff. Use explicit user-supplied time and save only when the user asks to log, book, or apply.
metadata:
  version: "1.2.1"
---

# Worklogs Creator

## Establish the work and time

- Use evidence of work performed from the current chat, user-provided notes, or a scavenger handoff. Reviews, investigations, prototyping, tests, and unsuccessful attempts count. A proposed task, Jira key, or time estimate alone supplies no work evidence. With no work evidence, explain that there is nothing to log and stop.
- Use the items the user selects. For a scavenger handoff, use its stable IDs to resolve selections and its effort notes to inform the split. For one clearly referenced bucket, default to all its items. Ask which items or bucket the user means when the scope is ambiguous. For ordinary current-chat logging without a supplied list or split, default to one activity.
- Require an explicit duration to allocate, preview placement, or book. Ask only for the duration when that is the sole missing input. Generate or improve descriptions from the evidence without asking the user to supply or approve wording. Keep source links and item labels in the draft.
- Follow the [worklog creation contract](references/worklog-creation-contract.md) for allocation, CLI readiness, placement, Jira attribution, validation, and reporting. Use [historical placement](references/historical-placement.md) for dated backfills.

## Preview and book

- Treat invocation, time allocation, and preview requests as planning. Write locally only when the user asks to log, book, or apply. The user can authorize time and booking in the same request. If a selected item is flagged as already booked, proceed only when the user explicitly asks to add more time to it. Otherwise pause the whole booking and ask whether to include it.
- Plan placement before inferring a Jira key. Explicit assignments and requests to stay unassigned apply at any record count. Infer only for a booking with exactly one placed record, using a relevant branch name or a leading Jira key in a commit subject.
- Preserve known Codex source thread IDs in local metadata and show existing worklogs from those threads as possible repeats. Follow the creation contract for lookup and preview validation.
- Validate the exact payload before applying. An instruction to apply a shown preview authorizes those exact records. Report the actual saved records or state clearly when nothing was created. Remote synchronization and changes to existing worklogs are outside this workflow.

# Evidence resolution

## Ownership boundary

- Local worklogs are canonical state.
- Session artifacts support drafting and later review.
- Session artifacts do not define canonical worklog state.
- Downstream adapter data does not override local worklogs.

## Preferred evidence sources

Use supporting evidence in this order:
1. extracted session artifact files
2. current live session summarized in memory
3. explicit user-provided notes or uploads
4. Git or Jira context
5. raw conversation mining only as a last resort for the current conversation

## Canonical artifact structure

Persist extracted session artifacts under:

- `.local/codex-task-extractor/{date}/{jira-key}/{session-id}.md`
- `.local/codex-task-extractor/{date}/_unlinked/{session-id}.md` when no Jira key resolves

When multiple Jira keys are linked, write one copy into each Jira-key folder.

Use this structure:

```markdown
---
session_id: <stable session identifier>
created_at: <iso8601 utc timestamp>
updated_at: <iso8601 utc timestamp>
jira_keys:
  - <JIRA-KEY>
project: <optional project>
---

# Session Worklog

## Jira Worklog
- <jira-friendly completed work>

## Completed
- <completed work>

## In Progress
- <in-progress work>

## Decisions
- <decisions>

## Artifacts
- <artifacts>

## Open Items
- <open items>
```

Keep every section present. Use `- none` when empty.

## Session-to-issue linking

Resolve issue keys with this precedence:
1. explicit issue keys in the request
2. issue keys stored in session artifact metadata
3. issue keys in commit messages
4. issue keys in branch context
5. issue keys mentioned in session text

Do not auto-include low-confidence historical matches.

## Multi-session same-issue rule

When multiple sessions support the same issue, merge them into one issue-level evidence bundle before drafting local worklogs. Do not emit one worklog per session by default.

# Session artifacts

Use session artifacts when a coding-session summary should be preserved for later worklog drafting or audit review. Artifacts support drafting; they are not canonical worklog state.

## Storage layout

Persist extracted session artifacts under `.local/codex-task-extractor/`:

```text
.local/codex-task-extractor/<MM-DD>/<JIRA-KEY>/<session-id>.md
.local/codex-task-extractor/<MM-DD>/_unlinked/<session-id>.md
```

When multiple issue keys are linked to one session, write one copy into each issue-key directory. Use `_unlinked` when no issue key is resolved.

## Artifact format

```markdown
---
session_id: <stable session identifier>
created_at: <iso8601 utc timestamp>
updated_at: <iso8601 utc timestamp>
jira_keys:
  - PROJ-123
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

## Renderer usage

`scripts/render_session_artifact.py` creates or updates an artifact atomically while preserving unmanaged frontmatter.

```text
python scripts/render_session_artifact.py --session-id session-123 --jira-key PROJ-123 --content-file session.md
python scripts/render_session_artifact.py --session-id session-123 --content-file session.md
```

The renderer owns `session_id`, `created_at`, and `updated_at`. It preserves additional frontmatter such as `jira_keys` and `project` from the content file when present.

## Linking precedence

Resolve issue keys with this precedence:

1. Explicit issue keys in the user request.
2. Issue keys in artifact metadata.
3. Issue keys in commit messages.
4. Issue keys in branch names.
5. Issue keys mentioned in session text.

Do not infer issue keys from weak historical associations.

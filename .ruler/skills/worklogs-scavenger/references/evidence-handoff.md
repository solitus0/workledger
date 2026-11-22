# Worklog evidence handoff

## Collect work items

- Group actions that served the same purpose, including repeated work across chats. Fold useful supporting work into its parent item. Omit routine administration as a standalone item, and retain small items that produced a meaningful separate result.
- Draft one concise action and subject per distinct item. Use chat and tool evidence for the work performed and its outcomes. When a clear Jira key and Jira MCP are available, read the issue title and description once per key for terminology, scope, and intended behavior. Continue with Codex evidence if lookup is unavailable.
- Add a short `Effort note` when the chat or tool output shows facts that help compare work items. Capture observable scope, investigation, rework, or verification, such as reproducing a bug in two environments and ruling out three causes. Omit the note when the available evidence adds little to the description. Leave durations and estimates out of the effort note.

## Write the shared Markdown

- Start with `Collection period`, `Scope`, and `Retrieval gaps`. Use inclusive local dates for a bounded period and `Unspecified` when the collection was scoped only by chat. Record the project, topics, or chats used to scope the collection. Report observed gaps or write `None encountered`.
- Group items under topic headings. Give each item a unique ID within the handoff, a draft description, and a `Source` line with relevant chat links. Add `Effort note`, `Git evidence`, or `Prior booking` when relevant. Preserve branch names and commit subjects with leading Jira keys when tied to the item. Flag prior booking only when earlier chats report it. Omit credentials, device identifiers, and unnecessary transcript excerpts.
- Add `Codex thread IDs` with the distinct IDs returned by the Codex app tools for the item's source chats. Omit this line when the IDs are unavailable. Carry the IDs for the retained evidence when merging or splitting items.
- Preserve item IDs through renames, moves, and reordering. If a merge or split changes which work an ID refers to, give the replacements fresh IDs and show which IDs they replace. Carry source links, effort evidence, and prior-booking flags for the work retained in each item.
- Keep the header and item IDs when handing off a subset. The same Markdown can be used in the current chat or pasted into another chat. Creator uses the selected items and the user's current request for allocation or booking.

```markdown
# Worklog evidence handoff

- Collection period: 2026-05-11 through 2026-05-15 (Europe/Vilnius)
- Scope: Workledger project
- Retrieval gaps: None encountered

## Status confirmations

- **2a** Investigated stale confirmations after reconnecting.
  - Source: [Investigation chat](CHAT_LINK)
  - Codex thread IDs: THREAD_ID
  - Effort note: Reproduced the failure in two environments and ruled out three causes.
  - Git evidence: branch fix/PROJ-123-status
- **2b** Verified the confirmation fix.
  - Source: [Verification chat](CHAT_LINK)
  - Effort note: Checked reconnect behavior with a targeted regression test.
  - Prior booking: reported in the source chat
```

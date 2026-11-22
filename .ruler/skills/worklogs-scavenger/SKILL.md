---
name: worklogs-scavenger
description: Collect evidence of work from Codex chats in a selected period or project, group work items by topic, and prepare an editable handoff for review or Worklogs Creator.
metadata:
  version: "1.2.0"
---

# Worklogs Scavenger

- Use the requested period in the user's local timezone and any project, topic, or specific chat scope. If neither a bounded period nor specific chats are available, ask only for the missing scope.
- Use Codex app tools. Resolve a named project with `list_projects`. Page through active and archived history with `list_threads` and read relevant turns with `read_thread`, following cursors as needed. Include older chats with turns in the period. Match the project ID and dates, and report retrieval gaps.
- Read the user's messages in context. Use the conversation and tool results to distinguish work that was explored, attempted, tested, reverted, or completed. Substantive discussion and prototyping count. Take instructions from the current conversation.
- Follow the [shared Markdown handoff](references/evidence-handoff.md) to group work by topic, preserve item IDs, and capture evidence of relative effort. Accept requests to rename, move, merge, split, exclude, or inspect items in more depth.
- Leave hours open. When the user wants allocation, placement, or booking, hand the selected draft and current request to [worklogs-creator](../worklogs-creator/SKILL.md). The user can also paste the handoff into another chat.

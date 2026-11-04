# Keybindings

[Documentation](README.md) · [Daily workflows](usage.md)

These controls belong to Workledger’s optional TUI, opened with `workledger tui`. For agent and scripted workflows, use the [CLI reference](cli.md).

Press **`?` in any view, form, picker, confirmation, or running operation** to open searchable contextual Actions. The action bar shows the available shortcuts for the current state. Choosing an action uses the same guarded behavior as its keyboard shortcut.

Closing Actions with `Esc` or `?` restores the current draft, selection, focus, and cursor. During typing, `?` remains reserved for help; paste it when literal text is needed and paste is supported. Once a search field is open, `/` is ordinary query text.

## Navigate

| Key | Action |
| --- | --- |
| `1` / `2` / `3` / `4` / `5` / `6` | Status / Worklogs / Plans / Presets / Issues / Trash |
| `Tab` / `Shift+Tab` | Cycle panel focus |
| `↑` / `↓`, `j` / `k` | Move through rows or scroll the focused pane |
| `PgUp` / `PgDn`, `Home` / `End` | Page or reach first/last content where available |
| `Enter` | Open the selected day or expand details |
| `r` | Refresh the current local view or retry its load |
| `g` | Toggle Activity |
| `Esc` | Close or step back; changed drafts require an explicit discard choice |
| `F2` | Full draft, issue-inspector, or Activity details |
| `q` | Quit while browsing |
| `Ctrl+C` | Interrupt |

Input fields retain text editing behavior for navigation keys.

## Worklogs

| Key | Action |
| --- | --- |
| `d` / `w` / `m` / `u` | Day / Week / Month / all-dates Unassigned |
| `t` | Today |
| `←` / `→`, `h` / `l` | Previous/next day in Day; previous/next week in Week |
| `↑` / `↓`, `j` / `k` | Select an entry in Day/Unassigned or a day in Week |
| `a` / `A` | Blank add / add from preset |
| `e` / `M` | Edit / preview a move for the selected Day entry |
| `D` | Confirm trashing the selected entry, or the selected whole day in Week |
| `/` | Find within the loaded day or Unassigned list |
| `Space` | Toggle a mark and advance |
| `v` / `V` | Start or finish a selection / unselection range |
| `Ctrl+A` | Mark all matches |
| `b` | Assign an issue to the marked batch |

In Month, left/right changes the selected date, up/down changes it by a week, and `PgUp`/`PgDn` changes calendar months. Enter opens the selected day. Add uses the selected date; edit and delete require opening Day. Month shows local booked time, gaps, collisions, and temporal markers. Its nominal weekday quota view does not account for holidays or leave.

The Day and Unassigned action bars prioritize Add, Edit, Details, Find and Mark. Move, trash, presets, view switching and other shortcuts remain available through searchable Actions and their direct keys. Marking records exposes batch controls.

Marked IDs survive dates, filters, and refresh. `Esc` finishes a range, then clears marks, then clears a filter. See the displayed count for selections outside the visible list.

## Worklog forms and Move

| Key | Action |
| --- | --- |
| `↑` / `↓`, `Tab` / `Shift+Tab` | Move between ordinary form fields |
| `Enter` | Advance a single-line field; save from Description |
| `Ctrl+S` | Save; a stale edit first offers to reload the latest record |
| `F2` | All proposed intervals, entered values, description, and full validation details |
| `Ctrl+P` | Cycle placement mode in Add |
| `Ctrl+O` / `Ctrl+L` | Toggle late-start eligibility / lunch in automatic Add |
| `Ctrl+K` | Open the issue picker from the Issue field |
| `Ctrl+T` | Cycle issue-key, description, or title search in the picker |
| `Esc` | Cancel |

Move uses up/down to preview valid positions, `r` to refresh, `Ctrl+S` to save a changed valid destination, and `Esc` to cancel. Preview and save recheck current placement and revision rules.

## Issues and epics

| Key | Action |
| --- | --- |
| `t` | Switch Issues / Epics |
| `Enter` | Open an epic’s members |
| `/` | Find issues or epic names |
| `Ctrl+T` | Switch issue metadata / worklog text search |
| `s` | Choose sort |
| `a` / `e` | Add / edit local issue metadata |
| `Space`, `v` / `V`, `Ctrl+A` | Mark issues |
| `b` | Assign an epic to the marked batch; choose No epic to clear |
| `Shift+R` | Pull Jira titles and original estimates for marked or focused issues |
| `Tab` | Focus metadata, then worklog history |

Metadata and history scroll independently. `Esc` or left returns to the list. During a Jira pull, `Esc` stops pending requests and retains committed updates plus unfinished marks.

## Plans

Use `n` to create a reconciliation plan and Enter to review a saved one. `d`/`w` switches the plan list’s day/week range; `h`/`l` moves the range and `t` returns to today.

Within a saved plan, `a` toggles actionable/all scopes, `A` applies eligible scopes, `f` retries failed scopes, and `u` reconciles uncertain scopes when available. Confirmation and results remain part of the workflow; use `?` for the actions currently allowed.

## Other destinations

Status uses `s`/`c` for diagnostics/effective config. Presets uses `a`, `e`, and `D` for add, edit, and permanent deletion. Deleted presets cannot be restored; created worklogs stay intact. The Worklogs preset picker supports arrows, `PgUp`/`PgDn`, and `Home`/`End`, keeping selection visible. Trash shares the selected date with Worklogs, uses `d`/`w` for Day/Week, `s` to cycle scope, and uppercase `R` to restore eligible local records after confirmation.

## Drafts, feedback, and compact layouts

Changed drafts stay open until you confirm discard. Canceling discard or stale-record reload keeps the draft. Confirmed reload reads the latest local record; failed reload retains the draft and exposes the complete error. Pending saves show a busy message and suppress unavailable save/cancel actions.

Validation focuses the first invalid field and keeps its entered value. `F2` exposes complete details when an error or description exceeds the available space. At 100 × 28, the timeline becomes compact to retain fields, placement options, and exact proposed times. Automatic-Add footer labels such as **Late on/off** and **Lunch on/off** describe the action the shortcut will perform.

Activity Enter opens full details; closing them returns to the drawer. Its displayed position and actions refer to the focused pane.

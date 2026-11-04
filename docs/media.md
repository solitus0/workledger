# Media and branding

[Documentation](README.md) · [Development](development.md)

## Product positioning

**An agent-friendly CLI for local worklogs, daily planning, and reviewed sync with Jira and Clockify.**

Lead introductions, quick starts, and package descriptions with the CLI. Make the agent-friendly claim concrete through structured JSON, dry runs, batch payloads, explicit exit codes, and saved reconciliation plans. Agents call the CLI directly; optional skills package onboarding and coding-session workflows.

Present the TUI as an optional interactive workspace over the same local ledger and services. Keep the Worklogs screenshot directly below the README logo and introduction, with a caption identifying the optional TUI. Keep the quick start focused on CLI commands. The Ledger W mark, colors, and geometry remain the visual identity for both interfaces.

## Ledger W

The finalized icon is a compact W with two horizontal divisions, flat color, and transparent cutouts. The README uses the divided primary mark with theme-aware color. The [asset inventory](assets/branding/ledger-w-refined/README.md) includes SVG and PNG files, monochrome treatments, small-size variants, and the padded avatar.

| Placement | Treatment |
| --- | --- |
| 16–24 px | Solid W |
| 32 px and larger | Divided W; divisions are most comfortable at 64 px and larger |
| Light background | Violet `#6D4BC3` |
| Dark background | Lavender `#B9A2F4` |
| Avatar or circular crop | Padded avatar, approximately 76% mark width, on `#1E1E2E` |

Keep the mark’s geometry and proportions, leave clear space, and use flat fills. Pair it with neutral sans-serif text. Use a genuine TUI view to demonstrate the product.

## Worklogs screenshot

The README’s optional-TUI images are rendered from the production Worklogs view and production context/issue services. They use fictional Atlas workspace data in a temporary SQLite database. They do not read or write operator configuration, personal worklogs, or remote adapters.

The [fixture](examples/showcase.json) fixes the selected date to **3 November 2026**, in `Europe/Vilnius`. The selected day contains seven worklogs totaling 6h30m, a 45-minute lunch, and three available windows. The selected API issue shows its all-time local effort and optional estimate.

| Setting | Value |
| --- | --- |
| Interface | Worklogs, Day mode |
| README hero grid | 140 columns × 36 rows |
| Compact reference grid | 110 columns × 34 rows |
| Font | Menlo, 16 px; Consolas or monospace fallback |
| Line height | 22 px for current canvas exports |
| Hero image format | Lossless PNG, exported at 2× native canvas size |
| Dark palette | Ledger W lavender focus with Mocha-derived semantic colors; `#1E1E2E` terminal background |
| Light palette | Ledger W violet focus with Latte-derived semantic colors; `#EFF1F5` terminal background |
| Frame | Thin terminal border and `workledger tui` title bar |

These captures use the production renderer with a fixed selected day and fictional data. Generated record IDs and creation timestamps can vary between runs. The terminal canvas preserves the actual frame’s text, cell positions, and ANSI colors; it adds terminal chrome only. No interface elements or data summaries are painted over the frame. No special production feature flags are required.

The README’s 140-column composition keeps the timeline, seven worklogs, available windows, and the selected worklog’s description and all-dates issue effort visible. Open the full-resolution hero PNG: [Dark](assets/screenshots/worklogs-wide-dark.png) · [Light](assets/screenshots/worklogs-wide-light.png).

The compact captures use the same branding and renderer: [Dark](assets/screenshots/worklogs-dark.png) · [Light](assets/screenshots/worklogs-light.png).

Layout references also cover the minimum 100 × 28 viewport ([Dark](assets/screenshots/worklogs-minimum-dark.png) · [Light](assets/screenshots/worklogs-minimum-light.png)) and a spacious 180 × 48 terminal ([Dark](assets/screenshots/worklogs-spacious-dark.png) · [Light](assets/screenshots/worklogs-spacious-light.png)).

## TUI state gallery

These examples use the same isolated capture flow. They cover minimum-size placement, long-list selection, themed dates, draft protection, deletion consequences, Activity, and diagnostic failure. Failure states are controlled fixtures; no remote operation is performed.

| Window | Dark | Light |
| --- | --- | --- |
| Fill at 100 × 28 | [Capture](assets/screenshots/add-fill-minimum-dark.png) | [Capture](assets/screenshots/add-fill-minimum-light.png) |
| Long preset picker | [Capture](assets/screenshots/preset-picker-minimum-dark.png) | [Capture](assets/screenshots/preset-picker-minimum-light.png) |
| Custom plan dates | [Capture](assets/screenshots/plan-dates-dark.png) | [Capture](assets/screenshots/plan-dates-light.png) |
| Permanent preset deletion | [Capture](assets/screenshots/preset-delete-dark.png) | [Capture](assets/screenshots/preset-delete-light.png) |
| Discard a changed draft | [Capture](assets/screenshots/draft-discard-dark.png) | [Capture](assets/screenshots/draft-discard-light.png) |
| Worklogs with Activity | [Capture](assets/screenshots/worklogs-activity-minimum-dark.png) | [Capture](assets/screenshots/worklogs-activity-minimum-light.png) |
| Failed diagnostics | [Capture](assets/screenshots/status-failure-dark.png) | [Capture](assets/screenshots/status-failure-light.png) |

## Terminal design

The TUI uses the finalized violet `#6D4BC3` and lavender `#B9A2F4` for focus, with separate semantic colors for booked time, proposed changes, warnings and errors. The plain Workledger terminal wordmark keeps the interface usable without image support or a special icon font.

The default semantic tokens are defined in `internal/tui/theme.go`:

| Role | Light | Dark |
| --- | --- | --- |
| Selected-row text | `#4C4F69` | `#CDD6F4` |
| Selected-row background | `#DED5F1` | `#45375C` |
| Focus and brand | `#6D4BC3` | `#B9A2F4` |
| Booked time and information | `#006F73` | `#94E2D5` |
| Progress | `#0B63CE` | `#89B4FA` |
| Proposed changes | `#0969A2` | `#74C7EC` |
| Warning | `#925500` | `#F9E2AF` |
| Success | `#287A1D` | `#A6E3A1` |
| Destructive action and error | `#D20F39` | `#F38BA8` |
| Secondary text | `#5C5F6F` | `#BAC2DE` |
| Muted text and placeholders | `#686B7A` | `#9399B2` |
| Borders | `#737686` | `#6C7086` |

Selected rows use neutral text on a subdued violet fill, while the active destination stays accented without a competing cursor. Day lists show their entry count, right-align durations, and keep available windows directly after the visible rows. The inspector presents selected time and description before one all-dates issue-effort summary. Selectors share a filled, bracketed treatment; tables and totals use compact durations such as `15m`, `2h`, and `1h30m`. Only the foreground dialog receives selection emphasis. At the minimum viewport, timeline chrome yields to fields, placement controls and exact times. Expanded details expose full text and a line-position indicator. [Keybindings](keybindings.md) owns interaction guidance; the canonical contracts remain in `specs/`.

Editor titles use bold focus color. Field labels share a 14-cell column and an explicit focus marker; section cues separate editable controls from calculated values. Warning and destructive dialogs carry their severity through the title, border, and confirmation chord. Monochrome output preserves brackets, markers, state words, and layout.

## Reproduce the screenshots

From the repository root, with the Go toolchain from `go.mod` and Python 3:

```sh
WORKLEDGER_CAPTURE_DIR="$PWD/docs/assets/screenshots" go test -tags docs ./internal/tui -run '^TestDocumentationCapture$' -count=1
python3 scripts/docs/render-terminal.py
python3 -m http.server 7861 --bind 127.0.0.1 --directory docs/assets/screenshots
```

For an isolated reference directory, pass `--directory /path/to/captures` to the Python renderer.

Open the generated HTML pages on localhost; each ANSI frame has a matching page. After each canvas finishes rendering, click **Save PNG** to export it directly without JPEG compression. Move the README captures to `docs/assets/screenshots/worklogs-wide-dark.png` and `worklogs-wide-light.png`; the README switches them using `prefers-color-scheme`. The saved image includes the canvas only, without the page controls or browser UI.

Save each canvas under its matching filename, including the wide and state-gallery PNGs. The generator keeps the corresponding ANSI frames and HTML pages alongside the images.

The `docs` build tag enables only the opt-in capture harness. Its temporary database is removed by the Go test runner. Generated ANSI files and HTML pages are kept alongside the final images so the content and rendering conditions are inspectable.

## Visual references

The repository presentation combines the agent-friendly CLI positioning with the Audit TUI Repos recommendation: restrained branding, installation beside the first useful command, focused documentation with one owner per topic, and a real interface image for the optional TUI.

The references used for that direction were [Helix](https://github.com/helix-editor/helix) for restraint, [Zellij](https://github.com/zellij-org/zellij) for navigation, [lazygit](https://github.com/jesseduffield/lazygit) for task-oriented demonstrations, and [Neovim](https://github.com/neovim/neovim) for documentation ownership. The Ledger W mark and Workledger’s interface remain original project assets.

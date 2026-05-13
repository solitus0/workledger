# Codex Hooks Setup for Grill Me

Use these templates when a user wants persistent grill sessions in Codex.

## Recommended project output

Default local/private directory:

```text
<repo>/.grill/
```

Team-visible alternative:

```text
<repo>/docs/decisions/grill/
```

Never write grill session artifacts into the skill installation directory.

## Example `.codex/config.toml`

Place this in the project or user Codex config, adjusting script paths as needed.

```toml
[features]
codex_hooks = true

[[hooks.SessionStart]]
matcher = "startup|resume"

[[hooks.SessionStart.hooks]]
type = "command"
command = '/usr/bin/python3 "$HOME/.codex/skills/grill-me/scripts/grill_session_start.py"'
timeout = 10
statusMessage = "Loading grill state"

[[hooks.PostToolUse]]
matcher = "^Bash$|^apply_patch$|^Read$|^Edit$|^Write$"

[[hooks.PostToolUse.hooks]]
type = "command"
command = '/usr/bin/python3 "$HOME/.codex/skills/grill-me/scripts/grill_post_tool_use.py"'
timeout = 10
statusMessage = "Recording grill evidence"

[[hooks.Stop]]

[[hooks.Stop.hooks]]
type = "command"
command = '/usr/bin/python3 "$HOME/.codex/skills/grill-me/scripts/grill_stop.py"'
timeout = 15
statusMessage = "Updating grill session state"
```

## Environment variables

The hook scripts support these optional environment variables:

- `GRILL_OUTPUT_DIR`: override output directory relative to project root or as an absolute path.
- `GRILL_TEAM_VISIBLE=1`: write to `docs/decisions/grill/` instead of `.grill/`.

## Expected files

```text
.grill/current-session.md
.grill/decisions.jsonl
.grill/open-questions.md
.grill/codebase-findings.md
.grill/session-events.jsonl
.grill/tool-events.jsonl
```

## Usage guidance

The hooks persist state, but they do not replace explicit reasoning in the skill. The agent should still state decisions and recommendations clearly in each response before writing them into the ledger.

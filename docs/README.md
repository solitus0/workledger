# Workledger documentation

Workledger is an agent-friendly CLI for local worklogs, daily planning, and reviewed tracker sync. Start with the [quick start](../README.md#quick-start) to create your first local entry, or [agent workflows](agents.md) for structured CLI use. Remote trackers and the interactive TUI are optional.

## Use Workledger

| Guide | What you will find |
| --- | --- |
| [Agent workflows](agents.md) | Direct CLI use, structured output, and optional skills for onboarding and coding sessions |
| [CLI reference](cli.md) | Date selectors, payloads, JSON output, and exit codes |
| [Installation](installation.md) | Homebrew, release archives, source builds, terminal requirements, and shell completion |
| [Configuration](configuration.md) | Local files, workday settings, tracker setup, and routing |
| [Daily workflows](usage.md) | Worklogs, placement, issues and epics, presets, and trash |
| [Synchronization](synchronization.md) | Initial pull, saved plans, review, apply, and failure recovery |
| [Keybindings](keybindings.md) | Navigation, forms, selection, and controls in the optional TUI |
| [Troubleshooting](troubleshooting.md) | Config, storage, authentication, routing, and terminal problems |

## Development and maintenance

[Development](development.md) covers the code layout, checks, and maintenance workflow. [Media and branding](media.md) describes the Ledger W assets and reproducible screenshots.

The [functional](../specs/functional.md) and [non-functional](../specs/non-functional.md) requirements own the product contract. User guides explain how to accomplish tasks; command-specific `--help` owns the discoverable command and flag surface. Update affected sources together when behavior changes.

---
name: workledger-onboarding
description: guide first-time workledger cli setup, shell completion, configuration, adapter onboarding, token environment variables, routing checks, and diagnostics. use when chatgpt needs to help initialize workledger, configure command completion, configure jira cloud, jira data center, or clockify, inspect effective yaml config, validate local storage, explain setup command syntax, interpret workledger status output, or troubleshoot onboarding before worklog creation, editing, reconciliation, or remote sync.
---

# Workledger Onboarding

Help users get `workledger` configured before worklog entry or sync workflows. Stay on setup, config inspection, routing, storage, env vars, and adapter connectivity.

## Flow

1. Identify the target adapter: Jira Cloud, Jira Data Center, Clockify, or multiple adapters.
2. Before asking for a Jira issue prefix, explain why it is required: Workledger uses the project-key portion before the dash (`PROJ` in `PROJ-123`) to route each issue to the configured Jira instance that owns it. Clarify that this is a routing rule, not a setup-time filter or a display label.
3. Ask only for missing setup inputs: base URL, instance name, issue prefix, token env var name, Cloud email, Clockify workspace/user IDs, or project mappings. Ask for every Jira project prefix handled by the instance and use repeated `--issue-prefix` flags when there is more than one.
4. Prefer env var names over raw token values. Do not ask users to paste secrets unless unavoidable.
5. Use `workledger init` when no config exists.
6. Generate the narrowest `workledger setup ...` command.
7. Validate with `workledger config`, then diagnose with `workledger status`. For Jira, use `workledger route explain <real-issue-key>` to confirm that the configured prefix resolves to the intended family and instance.
8. Make the first reconciliation action after setup a pull from every intended remote source. Cover the complete date window of the first push, review the saved pull plan, and apply it before creating any push plan. If an intended source is skipped or fails, resolve it and repeat the pull before pushing. Explain why: push reconciliation may treat remote-only worklogs in its window as cleanup and delete them.
9. Identify the user's shell and include the matching completion setup for Bash, Zsh, or Fish. Explain that PowerShell is unsupported and that installed scripts should be regenerated after upgrades that change commands or flags.
10. Interpret failures by layer: `config`, `env`, `routing`, `storage`, or `connectivity`.
11. If the user uses Codex and wants `workledger` to write local worklogs into the shared SQLite database outside the current agent session without repeated sandbox prompts, optionally recommend adding the default local storage path to the Codex sandbox config:

   ```toml
   [sandbox_workspace_write]
   writable_roots = [
     "~/.local/share/workledger"
   ]
   ```

   Explain the purpose clearly: this grants Codex write access to the default shared local Workledger storage directory so repeated `workledger worklogs add` operations can update the SQLite database without asking for permission each time. Also explain when to skip it: users who do not use Codex for local worklog writes, or who prefer explicit approval per write, do not need this change.
12. End with current setup status and the next concrete command. After successful adapter setup, that command is the initial pull unless it has already been completed for the intended first-push window.

Load `references/setup-commands.md` only when exact command syntax, shell completion installation, routing commands, or diagnostic boundaries matter.

## Boundaries

Do not handle normal worklog CRUD, coding-session worklog drafting, totals comparison, metadata refresh, reconcile plans, or remote sync beyond the mandatory initial-pull handoff unless the user explicitly changes scope or another skill handles it.

## Safety

- Keep secrets out of chat and command history; use names like `JIRA_TOKEN`, not values.
- Treat YAML config as operator-managed. Explain edits before changing files.
- Use `workledger status` as the single setup diagnostic command.
- Never recommend a first push until a pull covering the same complete date window has succeeded for every intended remote source and any saved pull plan has been reviewed and applied.
- When sandboxed, check storage paths and parent-directory writability before blaming adapter config.

## Response shape

For setup, include: detected goal, missing inputs if any, commands in order, matching shell completion setup, validation interpretation, initial-pull safety status, and next action.

For Jira setup, always include one plain-language routing example, such as: "`PROJ` tells Workledger that an issue like `PROJ-123` belongs to this Jira instance." If the instance handles multiple Jira projects, show one repeated `--issue-prefix` flag per project.

For troubleshooting, include: failing command, likely failure layer, evidence from output, and smallest follow-up command.

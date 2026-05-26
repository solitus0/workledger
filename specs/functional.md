# Functional Requirements

Status: Draft

Rule format:
- [ ] FUNC-000: Single auditable functional rule.

Placement rule:
- Add new requirements under the most specific existing group.
- Create a new group only when no existing group fits.
- Do not duplicate a rule across groups.

## Product Scope
- [ ] FUNC-001: Workledger shall ship as one local CLI binary named `workledger`.
- [ ] FUNC-002: Workledger shall provide local configuration bootstrap, configuration validation, and canonical local worklog management from the CLI.
- [ ] FUNC-003: Workledger shall provide adapter status, totals comparison, issue metadata, reconcile planning, plan review, plan apply, and plan retry command surfaces from the CLI.

## Root and Help
- [ ] FUNC-005: Bare `workledger` shall render root help to stdout and exit `0`.
- [ ] FUNC-006: `workledger -h` shall render root help to stdout and exit `0`.
- [ ] FUNC-007: `workledger --help` shall render root help to stdout and exit `0`.
- [ ] FUNC-008: `workledger help` shall render root help to stdout and exit `0`.
- [ ] FUNC-009: Every root, group, and leaf command shall accept `-h` and render command-specific plain-text help to stdout with exit code `0`.
- [ ] FUNC-010: Every root, group, and leaf command shall accept `--help` and render command-specific plain-text help to stdout with exit code `0`.
- [ ] FUNC-010a: Help for commands that accept date or time input shall name the accepted grammar for each relevant flag.
- [ ] FUNC-010b: Help for commands that accept date or time input shall include at least one concrete accepted example per command.
- [ ] FUNC-011: `workledger version` shall return the application version without requiring config presence.
- [ ] FUNC-012: `workledger --version` shall behave like `workledger version`.
- [ ] FUNC-013: `workledger -v` shall behave like `workledger version`.

## Workspace Bootstrap
- [ ] FUNC-014: `workledger init` shall prepare the local config path.
- [ ] FUNC-015: `workledger init` shall create the config directory when it is missing.
- [ ] FUNC-016: `workledger init` shall write a starter YAML config when the config file does not already exist.
- [ ] FUNC-017: `workledger init` shall succeed as a no-op when a valid config file already exists.
- [ ] FUNC-018: `workledger init` table output shall explicitly say when a valid config file already existed and was reused.
- [ ] FUNC-019: `workledger init` shall fail clearly when the existing config file is invalid.
- [ ] FUNC-020: `workledger init` shall write the active local base config plus a full commented adapter reference scaffold.
- [ ] FUNC-021: `workledger init` shall bootstrap `default_output: table` in a new starter config.
- [ ] FUNC-022: `workledger init` shall bootstrap `local_timezone: Europe/Vilnius` in a new starter config.
- [ ] FUNC-023: `workledger init` shall bootstrap `storage.sqlite_path: ~/.local/share/workledger/worklogs.db` in a new starter config.
- [ ] FUNC-024: `workledger init` shall bootstrap `worklogs.minimum_duration_seconds: 900`, `worklogs.daily_minimum_quota_seconds: 28800`, `worklogs.day_start: 08:00`, `worklogs.day_end: 17:00`, and `worklogs.daily_lunch: 12:00-12:45` in a new starter config, with comments that `daily_minimum_quota_seconds`, `day_start`, `day_end`, and `daily_lunch` are for `workledger worklogs context`.
- [ ] FUNC-025: `workledger init` shall write commented adapter reference scaffolds for `clockify`, `jira_cloud`, and `jira_data_center`.
- [ ] FUNC-026: The commented Clockify reference scaffold shall include a `project_mapping` example.
- [ ] FUNC-027: The commented Jira Cloud reference scaffold shall include `pull.exclude_issues` and a non-default reporting route profile example.
- [ ] FUNC-028: The commented Jira Data Center reference scaffold shall include `pull.exclude_issues` and a non-default reporting route profile example.
- [ ] FUNC-029: `workledger init` shall create the SQLite parent directory for the validated configured `storage.sqlite_path` when it is missing.
- [ ] FUNC-030: `workledger init` shall create the SQLite file when the database does not exist.
- [ ] FUNC-031: `workledger init` shall initialize the full empty local schema when the database does not exist.
- [ ] FUNC-032: `workledger init` shall leave an existing compatible SQLite file unchanged.
- [ ] FUNC-033: `workledger init` shall repair an existing SQLite file additively when required local tables are missing.
- [ ] FUNC-034: `workledger init` shall fail clearly when an existing SQLite file is incompatible or corrupt and cannot be repaired additively, identifying local storage corruption or incompatibility, naming the configured `storage.sqlite_path`, and telling the operator to inspect, replace, or restore the SQLite file before rerunning init.
- [ ] FUNC-035: `workledger init` shall attempt SQLite path provisioning and schema bootstrap from configured `storage.sqlite_path` even when a valid config file already exists.
- [ ] FUNC-036: `workledger init` shall support `table` output.
- [ ] FUNC-037: `workledger init` shall support `json` output.

## Configuration Commands
- [ ] FUNC-038: `workledger config validate` shall validate the effective local config without requiring adapter connectivity.
- [ ] FUNC-039: `workledger config validate` shall support `table` output.
- [ ] FUNC-040: `workledger config validate` shall support `json` output.
- [ ] FUNC-041: `workledger config validate` shall print an explicit success line to stdout in table output.
- [ ] FUNC-042: `workledger config validate` shall return a JSON success payload on stdout in JSON output.
- [ ] FUNC-042a: `workledger config validate --output json` effective `worklogs` payload shall include `day_start`, `day_end`, and `daily_lunch`.
- [ ] FUNC-043: `workledger config validate` shall return a JSON error payload with all discovered validation errors on failure in JSON output.
- [ ] FUNC-044: `workledger setup jira-cloud` shall append one Jira Cloud instance block to an existing valid local config.
- [ ] FUNC-045: `workledger setup jira-cloud` shall accept `--instance`, `--base-url`, `--email`, `--token-env`, and repeated `--issue-prefix`.
- [ ] FUNC-046: `workledger setup jira-cloud` shall fail when the target instance name already exists.
- [ ] FUNC-047: `workledger setup jira-cloud` table output shall print an `export <token-env>=...` hint.
- [ ] FUNC-048: `workledger setup jira-cloud` table output shall print `workledger status --adapter jira-cloud --instance <instance> --explain` after the export hint.
- [ ] FUNC-049: `workledger setup jira-data-center` shall append one Jira Data Center instance block to an existing valid local config.
- [ ] FUNC-050: `workledger setup jira-data-center` shall accept `--instance`, `--base-url`, `--token-env`, and repeated `--issue-prefix`.
- [ ] FUNC-051: `workledger setup jira-data-center` shall write `jira_data_center.instances.<instance>.auth.bearer.token_env`.
- [ ] FUNC-052: `workledger setup jira-data-center` shall write `jira_data_center.instances.<instance>.routing.profiles.default.issue_prefixes`.
- [ ] FUNC-053: `workledger setup jira-data-center` table output shall print an `export <token-env>=...` hint.
- [ ] FUNC-054: `workledger setup jira-data-center` table output shall print `workledger status --adapter jira-data-center --instance <instance> --explain` after the export hint.
- [ ] FUNC-055: `workledger setup clockify` shall append one active Clockify block to an existing valid local config.
- [ ] FUNC-056: `workledger setup clockify` shall accept `--workspace-id`, `--user-id`, `--api-key-env`, and repeated `--project-map PREFIX=PROJECT`.
- [ ] FUNC-057: `workledger setup clockify` shall default `--api-key-env` to `CLOCKIFY_API_KEY`.
- [ ] FUNC-058: `workledger setup clockify` shall fail when an active `clockify` block already exists.
- [ ] FUNC-059: `workledger setup clockify` shall write `clockify.auth.api_key_env`.
- [ ] FUNC-060: `workledger setup clockify` may write `clockify.project_mapping.issue_prefixes`.
- [ ] FUNC-061: `workledger config env` shall report env vars referenced by the effective config.
- [ ] FUNC-062: `workledger config env --print-export-template` shall print one `export NAME=` line per unique referenced env var.
- [ ] FUNC-063: `workledger config env --dotenv-template` shall print one `NAME=` line per unique referenced env var.
- [ ] FUNC-064: `workledger config summary` shall report config path, effective settings, configured-adapter counts, env-var counts, routing counts, reporting-target counts, and Clockify mapping counts.
- [ ] FUNC-064a: `workledger config summary` shall expose the effective `day_start`, `day_end`, and `daily_lunch` settings in table and JSON output.
- [ ] FUNC-065: `workledger doctor` shall run local config validation, env-var checks, and routing checks when invoked without a check-group flag.
- [ ] FUNC-066: `workledger doctor --local` shall enable local checks.
- [ ] FUNC-067: `workledger doctor --env` shall enable env-var checks.
- [ ] FUNC-068: `workledger doctor --routing` shall enable routing checks.
- [ ] FUNC-069: `workledger doctor --connectivity` shall enable adapter connectivity checks.
- [ ] FUNC-070: `workledger doctor --all` shall enable all check groups.
- [ ] FUNC-070a: Local `doctor` checks shall validate the effective `storage.sqlite_path`, including DB-file writability when present, parent-directory writability, and whether SQLite sidecar files can be created.
- [ ] FUNC-070b: Bare `workledger doctor` shall include the local storage writability check because bare `doctor` includes local checks by default.

## Routing Commands
- [ ] FUNC-071: `workledger routing list` shall emit configured Jira routing inventory across all configured Jira families and instances.
- [ ] FUNC-072: Each `workledger routing list` output row shall include adapter family, instance name, route profile, mode, source prefix, and target issue when present.
- [ ] FUNC-073: `workledger route explain <issue-key>` shall inspect routing matches for one issue key across all configured Jira profiles.
- [ ] FUNC-074: `workledger route explain <issue-key>` shall report all ownership matches without guessing.
- [ ] FUNC-075: `workledger route explain <issue-key>` shall report all reporting-target matches without guessing.
- [ ] FUNC-076: `workledger route explain <issue-key>` shall report the effective Clockify project mapping resolution.
- [ ] FUNC-077: `workledger route explain <issue-key>` shall report default-project fallback when Clockify fallback is used.
- [ ] FUNC-078: `workledger route explain <issue-key>` shall return an explicit `owned`, `reporting`, `unmatched`, or `ambiguous` result.
- [ ] FUNC-079: `workledger clockify mappings validate` shall validate configured Clockify project mappings against local Jira routing and live Clockify projects.
- [ ] FUNC-080: `workledger clockify mappings validate` shall validate configured mapping prefixes against known Jira routing inventory.
- [ ] FUNC-081: `workledger clockify mappings validate` shall validate mapped project names against live Clockify projects in the configured workspace.

## Status Commands
- [ ] FUNC-082: `workledger status` shall inspect every configured adapter family and configured adapter instance owned by each family.
- [ ] FUNC-083: `workledger status --adapter=<family>` shall limit inspection to one adapter family.
- [ ] FUNC-084: `workledger status --adapter=<family>` shall support `clockify`, `jira-cloud`, and `jira-data-center`.
- [ ] FUNC-084a: Clockify shall expose one implicit configured adapter instance named `clockify` at runtime without changing the YAML `clockify:` block shape.
- [ ] FUNC-085: `workledger status --adapter=jira-cloud --instance <name>` shall inspect only the selected configured Jira Cloud instance.
- [ ] FUNC-086: `workledger status --adapter=jira-data-center --instance <name>` shall inspect only the selected configured Jira Data Center instance.
- [ ] FUNC-086a: `workledger status --adapter=clockify --instance clockify` shall inspect the implicit configured Clockify instance.
- [ ] FUNC-087: `workledger status --adapter=clockify` shall verify Clockify API reachability and credential acceptance.
- [ ] FUNC-088: `workledger status --adapter=clockify` shall confirm the authenticated Clockify user `id` exactly matches configured `clockify.user_id`.
- [ ] FUNC-089: `workledger status --adapter=clockify` shall confirm the configured `clockify.workspace_id` is visible through `activeWorkspace` or `defaultWorkspace`.
- [ ] FUNC-090: `workledger status --adapter=clockify` shall confirm the configured Clockify workspace and user are readable.
- [ ] FUNC-091: `workledger status` shall return an empty result set when no adapter families are configured.
- [ ] FUNC-092: `workledger status --adapter=<family>` shall return an empty result set when the selected family has zero configured targets.

## Totals Commands
- [ ] FUNC-093: `workledger totals` shall compare canonical local booked time against every configured adapter target for one selected date window.
- [ ] FUNC-094: `workledger totals --adapter=clockify` shall compare canonical local booked time against Clockify remote booked time for one selected date window.
- [ ] FUNC-095: `workledger totals --adapter=jira-cloud` shall compare canonical local booked time against Jira Cloud remote booked time for one selected date window.
- [ ] FUNC-096: `workledger totals --adapter=jira-data-center` shall compare canonical local booked time against Jira Data Center remote booked time for one selected date window.
- [ ] FUNC-097: `workledger totals` shall require exactly one selected date window supplied by `--from` plus `--to` or by exactly one date-window shortcut selector.
- [ ] FUNC-098: `workledger totals` shall support `--progress=auto|bar|plain|off`.
- [ ] FUNC-098a: `workledger totals --adapter=clockify --instance clockify` shall select the implicit configured Clockify instance.
- [ ] FUNC-099: `workledger totals --adapter=jira-cloud` shall require `--instance <name>` when more than one `jira_cloud` instance is configured.
- [ ] FUNC-100: `workledger totals --adapter=jira-data-center` shall require `--instance <name>` when more than one `jira_data_center` instance is configured.
- [ ] FUNC-101: `workledger totals --adapter=jira-cloud` shall discover candidate issues with `worklogAuthor = currentUser() AND worklogDate >= "<from>" AND worklogDate <= "<to>"`.
- [ ] FUNC-102: `workledger totals --adapter=jira-data-center` shall discover candidate issues with `worklogAuthor = currentUser() AND worklogDate >= "<from>" AND worklogDate <= "<to>"`.
- [ ] FUNC-103: `workledger totals --details` shall expand explicit single-result table output to include per-day rows.
- [ ] FUNC-104: `workledger totals --details` shall not change JSON output.

## Worklog Selectors
- [ ] FUNC-105: Date-window selectors shall support `--today`, `--yesterday`, `--mon`, `--tue`, `--wed`, `--thu`, `--fri`, `--sat`, `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`.
- [ ] FUNC-105a: Shared date-window selectors shall support `--week-offset <int>` only as a modifier for exactly one selected weekday selector from `--mon` through `--sun`.
- [ ] FUNC-106: Active-worklog selectors shall support `--issue`, `--issue-prefix`, `--today`, `--yesterday`, `--mon`, `--tue`, `--wed`, `--thu`, `--fri`, `--sat`, `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`.
- [ ] FUNC-107: Active-worklog selectors shall support at most one `--issue` filter value per invocation.
- [ ] FUNC-107a: Active-worklog selectors shall support at most one `--issue-prefix` filter value per invocation.
- [ ] FUNC-108: `--only-deleted` shall switch `worklogs list` from active worklogs to deleted tombstones.
- [ ] FUNC-109: `--fields` shall accept a comma-separated ordered subset of the selected record shape.
- [ ] FUNC-110: Planning issue selectors shall allow repeated `--issue <KEY>` values.
- [ ] FUNC-111: Planning issue selectors shall preserve operator-supplied issue order.

## Worklog Listing
- [ ] FUNC-112: `workledger worklogs list` shall require at least one explicit time selector from the shared date-window selector family.
- [ ] FUNC-113: `workledger worklogs list` shall render active local worklogs within the selected time scope by default.
- [ ] FUNC-114: `workledger worklogs list --only-deleted` shall render deleted tombstones within the selected time scope instead of active worklogs.
- [ ] FUNC-115: `workledger worklogs list` shall support active-worklog selectors.
- [ ] FUNC-116: `workledger worklogs list` shall support the deleted-worklog selector.
- [ ] FUNC-117: `workledger worklogs list` shall support the field selector.
- [ ] FUNC-118: `workledger worklogs list` shall return the full filtered result set without pagination.
- [ ] FUNC-119: `workledger worklogs list` shall expose active worklog JSON items using the default active-worklog record shape when `--fields` is not set.
- [ ] FUNC-120: `workledger worklogs list` shall expose deleted tombstone rows using only `id`, `issue_key`, and `deleted_at`.

## Worklog Search
- [ ] FUNC-121: `workledger worklogs search <query>` shall require one positional `<query>` argument.
- [ ] FUNC-122: `workledger worklogs search <query>` shall search canonical stored normalized `description` values by partial, case-insensitive substring match.
- [ ] FUNC-123: `workledger worklogs search <query>` shall treat `<query>` as a literal substring rather than wildcard syntax.
- [ ] FUNC-124: `workledger worklogs search <query>` shall search active local worklogs by default across all stored dates.
- [ ] FUNC-125: `workledger worklogs search <query> --only-deleted` shall search deleted tombstones instead of active worklogs.
- [ ] FUNC-126: `workledger worklogs search <query>` shall reuse active-worklog selectors.
- [ ] FUNC-127: `workledger worklogs search <query>` shall reuse the deleted-worklog selector.
- [ ] FUNC-128: `workledger worklogs search <query>` shall reuse the field selector.
- [ ] FUNC-129: `workledger worklogs search <query>` shall return the full filtered result set without pagination.
- [ ] FUNC-130: `workledger worklogs search <query>` shall return exit code `0` when zero matches are found.

## Worklog Creation
- [ ] FUNC-136: `workledger worklogs add` shall require `--issue <KEY>`.
- [ ] FUNC-137: `workledger worklogs add` shall require exactly one of `--started <LocalTimestamp>`, `--started-utc <RFC3339UTC>`, or `--snap`.
- [ ] FUNC-138: `workledger worklogs add` shall require `--duration <GoDuration>`.
- [ ] FUNC-139: `workledger worklogs add` shall require `--description <text>`.
- [ ] FUNC-140: `workledger worklogs add` shall accept description input through a flag.
- [ ] FUNC-141: `workledger worklogs add --force` shall allow the operator to bypass duplicate or overlap rejection explicitly.
- [ ] FUNC-141a: `workledger worklogs add --dry` shall validate and preview one would-be local worklog without writing it.
- [ ] FUNC-141b: `workledger worklogs add --dry` shall use the same normalization, duplicate validation, overlap validation, and `--force` behavior as executed `worklogs add`.
- [ ] FUNC-141c: `workledger worklogs add --snap` shall reuse the `worklogs context` date-window selectors and workday-analysis inputs: `--today`, `--yesterday`, `--mon`, `--tue`, `--wed`, `--thu`, `--fri`, `--sat`, `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, `--to`, `--day-start`, `--day-end`, `--lunch`, and `--no-lunch`.
- [ ] FUNC-141d: `workledger worklogs add --snap` shall apply `--week-offset` with the same weekday-only validation and week-shift semantics as `worklogs context`.
- [ ] FUNC-141d: `workledger worklogs add --snap` without an explicit date selector shall search the current local day.
- [ ] FUNC-141e: `workledger worklogs add --snap` shall search selected dates in ascending order and choose the earliest free fitting start in that window.
- [ ] FUNC-141f: `workledger worklogs add --snap` shall split at the effective lunch window when the requested duration crosses lunch, shall preserve the exact requested total duration across the created fragments, and shall allow that split only when every created fragment is greater than or equal to the effective configured minimum local worklog duration.
- [ ] FUNC-141g: `workledger worklogs add --snap` may create one or two active worklogs atomically and shall not cross local midnight.
- [ ] FUNC-141h: `workledger worklogs add --snap` may extend the final fragment past the effective `day_end` with a warning when no later active worklog blocks that overflow.
- [ ] FUNC-142: `workledger worklogs add` shall auto-generate the created worklog `id`.
- [ ] FUNC-143: `workledger worklogs add` shall return the created canonical worklog on success.
- [ ] FUNC-143a: non-snap `workledger worklogs add` success output shall keep the single-record contract.
- [ ] FUNC-143b: snap `workledger worklogs add` success output shall return one `records` item per created worklog and shall expose optional success warnings.

## Worklog Update
- [ ] FUNC-144: `workledger worklogs update <id>` shall use patch-style flags `--issue`, `--started`, `--started-utc`, `--duration`, and `--description`.
- [ ] FUNC-145: `workledger worklogs update <id>` shall require at least one patch flag.
- [ ] FUNC-146: `workledger worklogs update <id>` shall reject an invocation that supplies both `--started` and `--started-utc`.
- [ ] FUNC-147: `workledger worklogs update <id>` shall validate the full resulting record after patching.
- [ ] FUNC-148: `workledger worklogs update <id>` shall succeed and return the canonical record when normalization makes the patch a semantic no-op.
- [ ] FUNC-149: `workledger worklogs update <id> --force` shall allow the operator to bypass duplicate or overlap rejection explicitly.
- [ ] FUNC-150: `workledger worklogs update <id>` shall treat tombstoned IDs as not found.
- [ ] FUNC-151: `workledger worklogs update <id>` shall return the updated canonical worklog on success.

## Batch Shift
- [ ] FUNC-152: `workledger worklogs shift` shall shift selected active local worklogs by one signed duration delta.
- [ ] FUNC-153: `workledger worklogs shift` shall reuse active-worklog selectors.
- [ ] FUNC-154: `workledger worklogs shift` shall require at least one explicit selector.
- [ ] FUNC-155: `workledger worklogs shift` shall require `--by <GoDuration>`.
- [ ] FUNC-156: `workledger worklogs shift --dry` shall preview validation and shifted timestamps without writing.
- [ ] FUNC-157: `workledger worklogs shift` shall change only the canonical `started_at` instant.
- [ ] FUNC-158: `workledger worklogs shift` shall preserve `id`, `issue_key`, `duration_seconds`, and `description`.
- [ ] FUNC-159: `workledger worklogs shift` shall return shifted canonical records on successful non-dry execution.

## Worklog Apply
- [ ] FUNC-160: `workledger worklogs apply` shall apply many local add operations from one payload.
- [ ] FUNC-161: `workledger worklogs apply` shall mutate canonical SQLite worklogs only.
- [ ] FUNC-162: `workledger worklogs apply --dry` shall validate and preview without writing.
- [ ] FUNC-163: `workledger worklogs apply --force` shall bypass duplicate or overlap rejection explicitly.
- [ ] FUNC-164: `workledger worklogs apply` shall support exactly one payload source per invocation: `--file <path>` or `--stdin`.
- [ ] FUNC-165: `workledger worklogs apply` shall reject an invocation when neither payload source is provided.
- [ ] FUNC-166: `workledger worklogs apply` shall reject an invocation when both payload sources are provided.
- [ ] FUNC-167: `workledger worklogs apply` shall treat payload construction as external to the CLI.
- [ ] FUNC-168: `workledger worklogs apply` shall return a deterministic would-apply summary plus per-operation results for successful dry-runs.
- [ ] FUNC-169: `workledger worklogs apply` shall return a deterministic applied summary plus per-operation results for successful non-dry runs.
- [ ] FUNC-170: `workledger worklogs apply` per-operation results shall identify the operation type and resulting local worklog `id`.
- [ ] FUNC-170a: `workledger` commands that persist local SQLite state shall preflight local storage writability before starting a write transaction.

## Worklog Delete
- [ ] FUNC-171: `workledger worklogs delete <id>` shall delete exactly one active local worklog.
- [ ] FUNC-172: `workledger worklogs delete <id>` shall remove the selected worklog from the active worklog set.
- [ ] FUNC-173: `workledger worklogs delete <id>` shall write one tombstone by default.
- [ ] FUNC-174: `workledger worklogs delete <id> --hard` shall permanently remove the selected active local worklog without creating a tombstone.
- [ ] FUNC-175: `workledger worklogs delete <id>` shall remain non-interactive once validation passes.
- [ ] FUNC-176: `workledger worklogs delete <id>` shall return deterministic success output with `id`, `issue_key`, `deleted_at`, and `hard_delete`.
- [ ] FUNC-177: `workledger worklogs delete <id>` shall be local-only.

## Batch Delete
- [ ] FUNC-178: Filtered batch delete shall be available through the `workledger worklogs delete` command family.
- [ ] FUNC-179: Filtered batch delete shall reuse active-worklog selectors.
- [ ] FUNC-180: Filtered batch delete shall accept any non-empty valid selector subset from the active-worklog selector set.
- [ ] FUNC-181: Filtered batch delete shall require `--yes` for execution.
- [ ] FUNC-182: Filtered batch delete shall support `--hard` to delete matched active worklogs without writing tombstones.
- [ ] FUNC-183: Single-delete by `<id>` and filtered batch-delete selectors shall be mutually exclusive modes.
- [ ] FUNC-184: Filtered batch delete shall be a valid no-op when zero active worklogs match the selector set.
- [ ] FUNC-185: Filtered batch delete shall apply to active worklogs only.
- [ ] FUNC-186: Filtered batch delete shall remain valid when exactly one active worklog matches.
- [ ] FUNC-187: Filtered batch delete shall support `--dry` to preview matched active worklogs without deleting them.
- [ ] FUNC-188: Batch-delete dry-run shall return the full matched active records together with the matched count.
- [ ] FUNC-189: Executed filtered batch delete shall return deleted IDs and deleted count rather than full deleted records.

## Worklog Restore
- [ ] FUNC-190: `workledger worklogs restore` shall operate in selector-based batch mode only.
- [ ] FUNC-191: `workledger worklogs restore` shall reuse the active-worklog selector family.
- [ ] FUNC-192: `workledger worklogs restore` shall require at least one explicit time selector.
- [ ] FUNC-193: `workledger worklogs restore` shall optionally accept `--issue <KEY>`.
- [ ] FUNC-194: `workledger worklogs restore` shall select tombstones by their original `started_at_utc`.
- [ ] FUNC-195: `workledger worklogs restore` shall require `--yes` for execution.
- [ ] FUNC-196: `workledger worklogs restore --dry` shall preview matched tombstones without restoring them.
- [ ] FUNC-197: `workledger worklogs restore --dry` shall be mutually exclusive with `--yes`.
- [ ] FUNC-198: `workledger worklogs restore --force` shall bypass duplicate or overlap rejection explicitly.
- [ ] FUNC-199: `workledger worklogs restore` shall insert active local worklogs using the original `id`, `issue_key`, `started_at_utc`, `duration_seconds`, and `description`.
- [ ] FUNC-200: `workledger worklogs restore` shall delete matching tombstones when execution succeeds.
- [ ] FUNC-201: `workledger worklogs restore` shall be a valid no-op when zero tombstones match.
- [ ] FUNC-202: `workledger worklogs restore` shall return executed success payloads with restored IDs and restored count rather than full active records.
- [ ] FUNC-203: `workledger worklogs restore` shall reject `--only-deleted`.

## Worklog Context
- [ ] FUNC-204: `workledger worklogs context` shall return read-only planning snapshots over canonical local worklogs.
- [ ] FUNC-205: `workledger worklogs context` shall reuse date-window selectors.
- [ ] FUNC-206: `workledger worklogs context` shall reuse repeated planning issue selectors.
- [ ] FUNC-207: `workledger worklogs context` shall support optional workday-analysis inputs `--day-start`, `--day-end`, `--lunch`, and `--no-lunch`.
- [ ] FUNC-207aa: `workledger worklogs context` shall resolve effective day-start precedence as `--day-start`, then config `worklogs.day_start`, then built-in fallback `08:00`, and effective day-end precedence as `--day-end`, then config `worklogs.day_end`, then built-in fallback `17:00`.
- [ ] FUNC-207a: `workledger worklogs context` shall resolve effective lunch in this order: `--no-lunch`, `--lunch`, config `worklogs.daily_lunch`, then built-in fallback `12:00-12:45`.
- [ ] FUNC-208: `workledger worklogs context` shall return one first-class planning snapshot for the selected scope.
- [ ] FUNC-209: `workledger worklogs context` shall structure the planning snapshot per selected local day.
- [ ] FUNC-210: `workledger worklogs context` shall include empty selected days when no worklogs exist for those days.
- [ ] FUNC-211: `workledger worklogs context` shall support `table` output.
- [ ] FUNC-212: `workledger worklogs context` shall support `json` output.
- [ ] FUNC-213: `workledger worklogs context` JSON output shall include `filters`, `summary`, `settings`, `planning`, and `days`.
- [ ] FUNC-214: Each `workledger worklogs context` day shall include active local worklogs already present for that day.
- [ ] FUNC-215: Each `workledger worklogs context` day shall include total booked duration for that day.
- [ ] FUNC-216: Each `workledger worklogs context` day shall include free slots inside the configured workday.
- [ ] FUNC-217: Each `workledger worklogs context` day shall include collisions already present in local state for that day.
- [ ] FUNC-218: `workledger worklogs context` table output shall render one row per selected day and include the per-day delta until the configured `worklogs.daily_minimum_quota_seconds`.

## Issue Metadata
- [ ] FUNC-219: `workledger issue-metadata list` shall read cached issue metadata from local SQLite only.
- [ ] FUNC-220: `workledger issue-metadata list` shall support `--issue` and the shared explicit time selectors.
- [ ] FUNC-221: `workledger issue-metadata list --issue <KEY>` without a time selector shall return the cached row for that issue only.
- [ ] FUNC-222: `workledger issue-metadata list` with time selectors shall derive distinct issue keys from matching active local worklogs and return cached metadata for those issues.
- [ ] FUNC-223: `workledger issue-metadata list` JSON output shall return `filters`, `items`, and `total`.
- [ ] FUNC-224: `workledger issue-metadata refresh --adapter=jira-cloud --field=max-estimate` shall refresh Jira Cloud original-estimate metadata into local SQLite.
- [ ] FUNC-225: `workledger issue-metadata refresh --adapter=jira-data-center --field=max-estimate` shall refresh Jira Data Center original-estimate metadata into local SQLite.
- [ ] FUNC-226: `workledger issue-metadata refresh` shall reuse active local-worklog selectors.
- [ ] FUNC-227: `workledger issue-metadata refresh` shall upsert local metadata rows for distinct selected issue keys.
- [ ] FUNC-228: `workledger issue-metadata refresh` shall report deterministic per-issue outcomes in `table` or `json` output.

## Reconcile Planning
- [ ] FUNC-229: `workledger plan reconcile` shall create remote-sync plans.
- [ ] FUNC-230: `workledger plan reconcile` shall require exactly one of `--pull` or `--push`.
- [ ] FUNC-231: `workledger plan reconcile` shall require at least one scope selector supplied by repeated `--adapter=<family>` and/or repeated `--instance=<name>`.
- [ ] FUNC-232: `workledger plan reconcile` shall require an explicit selected date window supplied by `--from` plus `--to` or by exactly one date-window shortcut selector.
- [ ] FUNC-233: `workledger plan reconcile` shall persist exactly one saved plan after command-level validation succeeds unless a reporting reconcile resolves only non-actionable scopes.
- [ ] FUNC-234: `workledger plan reconcile` shall return a no-plan result when a reporting reconcile finds only non-actionable scopes.
- [ ] FUNC-235: `workledger plan reconcile --pull --adapter=<family>` shall inspect remote worklogs from the selected adapter family's configured source scope.
- [ ] FUNC-235a: `workledger plan reconcile` shall accept repeated `--instance=<name>` as an adapter-instance allowlist, including the implicit Clockify instance `clockify` and configured Jira instances.
- [ ] FUNC-235b: omitted `--instance` shall include the implicit Clockify instance when `clockify` is selected and all configured instances for each explicitly selected Jira adapter family.
- [ ] FUNC-236: `workledger plan reconcile --pull --adapter=<family>` shall normalize remote observations into canonical candidate rows.
- [ ] FUNC-237: `workledger plan reconcile --pull --adapter=<family>` shall compare normalized observations with the current local canonical ledger.
- [ ] FUNC-238: `workledger plan reconcile --pull --adapter=<family>` shall produce a saved merge plan without mutating local canonical worklogs during reconcile.
- [ ] FUNC-238a: `workledger plan reconcile --pull` with multiple selected adapters or instances shall persist a saved `check_failed` plan instead of aborting when one selected adapter scope fails with a remote request error after command-level validation.
- [ ] FUNC-239: `workledger plan reconcile --push --adapter=<family>` shall load canonical local worklogs from SQLite for delivery planning.
- [ ] FUNC-240: `workledger plan reconcile --push --adapter=<family>` shall load deleted-worklog tombstones from SQLite for remote cleanup planning.
- [ ] FUNC-241: `workledger plan reconcile --push --adapter=<family> --only-deleted` shall limit push planning to tombstone-backed cleanup scopes.
- [ ] FUNC-242: `workledger plan reconcile --push --adapter=<family> --route-profile=<name>` shall select a named route profile for push planning.
- [ ] FUNC-243: `workledger plan reconcile --push --adapter=<family>` shall use the configured `default` route profile when `--route-profile` is omitted.
- [ ] FUNC-244: `workledger plan reconcile --push --adapter=<family>` shall compute local-versus-remote diffs for selected scopes against resolved target adapter instances.
- [ ] FUNC-244a: human-readable `workledger plan reconcile` output shall include next-step commands for `plan show <plan-id>` and, when the saved plan contains one or more `ready` items, `plan apply <plan-id>`.

## Plan Review
- [ ] FUNC-246: `workledger plan show` shall load a saved plan by requested plan ID when provided.
- [ ] FUNC-247: `workledger plan show` shall load the most recent saved plan when no plan ID is provided.
- [ ] FUNC-248: `workledger plan show` shall render the saved reconciliation report without new external requests.
- [ ] FUNC-249: `workledger plan show` shall render deterministic output suitable for operator review.
- [ ] FUNC-250: `workledger plan show` shall report `plan_status` per scope.
- [ ] FUNC-251: `workledger plan show` shall report `planned_action` per scope.
- [ ] FUNC-252: `workledger plan show` shall report comparison status per scope.
- [ ] FUNC-253: `workledger plan show` shall report target adapter family, target issue, saved reconcile time window, local row count, remote row count, and execution state per scope.
- [ ] FUNC-253a: `workledger plan show --only-ready` shall limit rendered scopes to saved plan items whose `plan_status` is `ready`.

## Plan Listing
- [ ] FUNC-254: `workledger plan list` shall load saved-plan metadata from SQLite only.
- [ ] FUNC-255: `workledger plan list` shall render saved plans ordered by `created_at desc`, then stable plan ID.
- [ ] FUNC-256: `workledger plan list` shall include deterministic summary counts for total items, ready items, and terminally succeeded items.
- [ ] FUNC-257: `workledger plan list` shall expose `plan_direction`, saved target adapter families, saved target instances, and saved reconcile time window.
- [ ] FUNC-258: `workledger plan list` shall support shared date-window selectors against saved plan `created_at` in the effective local timezone.

## Plan Apply
- [ ] FUNC-259: `workledger plan apply` shall load the requested plan ID when provided.
- [ ] FUNC-260: `workledger plan apply` shall load the most recent saved plan when no plan ID is provided.
- [ ] FUNC-261: `workledger plan apply` shall build tasks only from `ready` items whose execution state is `not_attempted`.
- [ ] FUNC-262: `workledger plan apply` shall succeed as a no-op when the saved plan contains zero executable `ready` items.
- [ ] FUNC-263: `workledger plan apply` shall use the saved scope definition and saved payload snapshot for execution.
- [ ] FUNC-264: `workledger plan apply` shall execute according to the saved `plan_direction`.
- [ ] FUNC-265: `workledger plan apply` shall execute only one saved plan at a time.
- [ ] FUNC-266: `workledger plan apply` shall record delete and create outcomes separately when one saved push item requires both steps.
- [ ] FUNC-267: `workledger plan apply` shall continue executing other eligible scopes when one scope fails.
- [ ] FUNC-268: `workledger plan apply` shall persist per-scope results independently.
- [ ] FUNC-269: `workledger plan apply` for `plan_direction=pull` shall merge the saved normalized remote payload into canonical local SQLite state.
- [ ] FUNC-270: `workledger plan apply` for `plan_direction=push` shall re-discover current remote worklogs inside the saved issue/window scope at execution time when cleanup is required.
- [ ] FUNC-271: `workledger plan apply` for `plan_direction=push` shall apply saved remote cleanup on the target adapter instance saved on that plan item before creating replacement worklogs.

## Plan Retry
- [ ] FUNC-272: `workledger plan retry` shall load the requested saved plan by ID.
- [ ] FUNC-273: `workledger plan retry` shall operate on saved plan items only.
- [ ] FUNC-274: `workledger plan retry` shall require an explicit retry scope such as `--only failed` or `--only uncertain`.
- [ ] FUNC-275: `workledger plan retry <id> --only failed` shall process `ready` items with `execution_state=failed`.
- [ ] FUNC-276: `workledger plan retry <id> --only uncertain` shall process `ready` items with `execution_state=uncertain`.
- [ ] FUNC-277: `workledger plan retry` shall reuse the same saved scope and the same saved payload.
- [ ] FUNC-278: `workledger plan retry` may re-list the current remote row set only when `plan_direction=push` and the planned action requires cleanup or safety checks.

## Adapter Pull
- [ ] FUNC-279: Clockify pull shall read Clockify worklogs for the configured operator and workspace only.
- [ ] FUNC-280: Clockify pull shall use only the configured Clockify workspace and user scope.
- [ ] FUNC-281: Clockify pull shall not infer source scope from issue-key routing.
- [ ] FUNC-282: Clockify pull shall treat entries without exactly one resolved exact issue-key tag such as `AAPP-123` as non-importable findings rather than actionable pull rows.
- [ ] FUNC-283: Jira Data Center pull shall import only worklogs authored by the authenticated Jira user.
- [ ] FUNC-284: Jira-family pull shall exclude configured reporting target issues from canonical local import on the owning adapter instance.
- [ ] FUNC-285: Pull plans shall merge into canonical local state without making remote systems authoritative.

## Adapter Push
- [ ] FUNC-286: Jira Cloud push shall participate in shared reconcile delivery to Jira Cloud targets.
- [ ] FUNC-287: Jira Data Center push shall participate in shared reconcile delivery to Jira Data Center targets.
- [ ] FUNC-288: Jira Data Center push shall compare and mutate only worklogs authored by the authenticated Jira user.
- [ ] FUNC-289: Clockify push shall target the single configured Clockify target.
- [ ] FUNC-290: Clockify push shall resolve a Clockify project from the local issue-key prefix using `clockify.project_mapping.issue_prefixes` when a prefix rule matches.
- [ ] FUNC-291: Clockify push shall use `clockify.project_mapping.default_project` when no prefix rule matches and a default project is configured.
- [ ] FUNC-292: Clockify push shall ensure an exact issue-key tag such as `AAPP-123` exists before creating a remote Clockify time entry.
- [ ] FUNC-293: Clockify push may create the missing exact issue-key tag when `clockify.project_mapping.create_issue_tag_if_missing=true`.
- [ ] FUNC-294: Reporting delivery shall deliver each canonical local worklog row as one remote reporting worklog row.
- [ ] FUNC-295: Reporting delivery shall support arbitrary operator-selected date windows.

## Progress Reporting
- [ ] FUNC-296: Remote batch commands shall share `--progress=auto|bar|plain|off` where progress reporting is supported.
- [ ] FUNC-297: `--progress=auto` shall be the default progress mode.
- [ ] FUNC-298: `--progress=auto` shall render an animated progress bar only when stderr is a TTY.
- [ ] FUNC-299: `--progress=bar` shall force interactive bar rendering when stderr is a TTY.
- [ ] FUNC-300: `--progress=bar` shall fall back to `plain` when stderr is not a TTY.
- [ ] FUNC-301: `--progress=plain` shall emit line-oriented progress summaries to stderr without cursor control.
- [ ] FUNC-302: `--progress=off` shall disable live progress output.

## Out of Scope
- [ ] FUNC-303: `workledger tui` shall be the only deferred command surface in this organized spec.
- [ ] FUNC-304: The TUI implementation shall remain outside the current implementation scope.
- [ ] FUNC-305: Tombstone-specific top-level commands shall not be part of the current command surface.

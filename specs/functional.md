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
- [ ] FUNC-002: Workledger shall provide local configuration bootstrap, configuration validation, reusable worklog preset management, and canonical local worklog management from the CLI.
- [ ] FUNC-003: Workledger shall provide adapter status, totals comparison, issues, reconcile planning, plan review, plan apply, and plan retry command surfaces from the CLI.

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
- [ ] FUNC-013a: Root help shall expose a Workledger-owned `completion` command.
- [ ] FUNC-013b: `workledger completion` shall generate scripts only for `bash`, `zsh`, and `fish`; PowerShell shall not be supported or advertised.
- [ ] FUNC-013c: Completion script generation shall write the generated script to stdout and shall not require config or initialized SQLite storage.
- [ ] FUNC-013e: Shell completion shall suggest fixed CLI enum values plus locally known worklog IDs, plan IDs, issue keys, local epic names, configured adapter instances, and configured Jira route profiles where relevant.
- [ ] FUNC-013f: Missing or invalid local config or storage shall suppress only affected dynamic completion candidates without emitting an operator-facing error.
- [ ] FUNC-013g: Locally known issue-key completion shall use the durable local issue catalog; rank used keys by latest active worklog `created_at`, unused issues by latest local update or cache refresh, and ties by issue key.

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
- [ ] FUNC-024: `workledger init` shall bootstrap `worklogs.minimum_duration_seconds: 900`, `worklogs.daily_minimum_quota_seconds: 28800`, `worklogs.day_start: 08:00`, `worklogs.day_end: 17:00`, and `worklogs.daily_lunch: 12:00-12:45` in a new starter config, with comments that `daily_minimum_quota_seconds` is for `workledger worklogs context` and that `day_start`, `day_end`, and `daily_lunch` are for context analysis and automatic worklog placement.
- [ ] FUNC-025: `workledger init` shall write commented adapter reference scaffolds for `clockify`, `jira_cloud`, and `jira_data_center`.
- [ ] FUNC-026: The commented Clockify reference scaffold shall include a `project_mapping` example.
- [ ] FUNC-027: The commented Jira Cloud reference scaffold shall include `pull.exclude_issues` and a non-default reporting route profile example.
- [ ] FUNC-028: The commented Jira Data Center reference scaffold shall include `pull.exclude_issues` and a non-default reporting route profile example.
- [ ] FUNC-029: `workledger init` shall create the SQLite parent directory for the validated configured `storage.sqlite_path` when it is missing.
- [ ] FUNC-030: `workledger init` shall create the SQLite file when the database does not exist.
- [ ] FUNC-031: `workledger init` shall initialize the full empty local schema when the database does not exist.
- [ ] FUNC-032: `workledger init` shall leave an existing compatible SQLite file unchanged.
- [ ] FUNC-033: `workledger init` shall repair an existing SQLite file when required local tables are missing, shall migrate legacy saved-plan item lifecycle results into delivery attempts before removing the legacy lifecycle columns, and shall preserve table rows when rebuilding explicit indexes after a single missing final database page is referenced exclusively by those indexes.
- [ ] FUNC-034: `workledger init` shall fail clearly when an existing SQLite file is incompatible, has table or auto-index corruption, or otherwise cannot be repaired without guessing at persisted data, identifying local storage corruption or incompatibility, naming the configured `storage.sqlite_path`, and telling the operator to inspect, replace, or restore the SQLite file before rerunning init.
- [ ] FUNC-035: `workledger init` shall attempt SQLite path provisioning and schema bootstrap from configured `storage.sqlite_path` even when a valid config file already exists.
- [ ] FUNC-036: `workledger init` shall support `table` output.
- [ ] FUNC-037: `workledger init` shall support `json` output.
- [ ] FUNC-037a: Ordinary commands shall never create or repair the SQLite schema during startup.
- [ ] FUNC-037b: Ordinary commands shall fail clearly when the configured SQLite file is missing, saying the SQLite store is not ready and telling the operator to run `workledger init`.
- [ ] FUNC-037c: Ordinary commands shall fail clearly before feature SQL runs when the configured SQLite schema is outdated or mismatched, telling the operator to run `workledger init` to repair it.
- [ ] FUNC-037d: Ordinary commands shall reject saved-plan schemas that still contain legacy item-level `applied_state`, `applied_at`, or `apply_message` columns until `workledger init` migrates them.

## Configuration Commands
- [ ] FUNC-038: `workledger config` shall validate the effective local config before rendering configuration details.
- [ ] FUNC-039: `workledger config` shall support `table` output.
- [ ] FUNC-040: `workledger config` shall support `json` output.
- [ ] FUNC-041: `workledger config` shall report config path, effective settings, configured-adapter counts, env-var counts, routing counts, reporting-target counts, and Clockify mapping counts.
- [ ] FUNC-042: `workledger config` shall expose the effective `day_start`, `day_end`, and `daily_lunch` settings in table and JSON output.
- [ ] FUNC-043: `workledger config` shall return a JSON error payload with all discovered validation errors on failure in JSON output.
- [ ] FUNC-044: `workledger setup jira-cloud` shall append one Jira Cloud instance block to an existing valid local config.
- [ ] FUNC-045: `workledger setup jira-cloud` shall accept `--instance`, `--base-url`, `--email`, `--token-env`, and repeated `--issue-prefix`.
- [ ] FUNC-045a: Jira Cloud and Jira Data Center setup shall require at least one `--issue-prefix`, where each value is the project-key portion before the dash in an issue key, such as `PROJ` in `PROJ-123`.
- [ ] FUNC-045b: Jira setup documentation and command help shall explain in plain language that `--issue-prefix` creates the default routing rule used to decide which configured Jira instance owns an issue; it is not a setup-time filter or display label.
- [ ] FUNC-045c: Jira Cloud setup shall write every `--issue-prefix` value to `jira_cloud.instances.<instance>.routing.profiles.default.issue_prefixes`.
- [ ] FUNC-046: `workledger setup jira-cloud` shall fail when the target instance name already exists.
- [ ] FUNC-047: `workledger setup jira-cloud` table output shall print an `export <token-env>=...` hint.
- [ ] FUNC-048: `workledger setup jira-cloud` table output shall print `workledger status` after the export hint.
- [ ] FUNC-049: `workledger setup jira-data-center` shall append one Jira Data Center instance block to an existing valid local config.
- [ ] FUNC-050: `workledger setup jira-data-center` shall accept `--instance`, `--base-url`, `--token-env`, and repeated `--issue-prefix`.
- [ ] FUNC-051: `workledger setup jira-data-center` shall write `jira_data_center.instances.<instance>.auth.bearer.token_env`.
- [ ] FUNC-052: `workledger setup jira-data-center` shall write `jira_data_center.instances.<instance>.routing.profiles.default.issue_prefixes`.
- [ ] FUNC-053: `workledger setup jira-data-center` table output shall print an `export <token-env>=...` hint.
- [ ] FUNC-054: `workledger setup jira-data-center` table output shall print `workledger status` after the export hint.
- [ ] FUNC-055: `workledger setup clockify` shall append one active Clockify block to an existing valid local config.
- [ ] FUNC-056: `workledger setup clockify` shall accept `--workspace-id`, `--user-id`, `--api-key-env`, and repeated `--project-map PREFIX=PROJECT`.
- [ ] FUNC-057: `workledger setup clockify` shall default `--api-key-env` to `CLOCKIFY_API_KEY`.
- [ ] FUNC-058: `workledger setup clockify` shall fail when an active `clockify` block already exists.
- [ ] FUNC-059: `workledger setup clockify` shall write `clockify.auth.api_key_env`.
- [ ] FUNC-060: `workledger setup clockify` may write `clockify.project_mapping.issue_prefixes`.
- [ ] FUNC-061: `workledger status` shall run local config validation, env-var checks, routing checks, and adapter connectivity checks by default.
- [ ] FUNC-070a: Local `status` checks shall validate the effective `storage.sqlite_path`, including DB-file writability when present, parent-directory writability, and whether SQLite sidecar files can be created.
- [ ] FUNC-070b: Bare `workledger status` shall include the local storage writability check.

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

## Status Connectivity
- [ ] FUNC-082: `workledger status` shall inspect every configured adapter family and configured adapter instance owned by each family.
- [ ] FUNC-083: `workledger status` connectivity checks shall cover `clockify`, `jira-cloud`, and `jira-data-center`.
- [ ] FUNC-084: Clockify connectivity checks shall expose one implicit configured adapter instance named `clockify` at runtime without changing the YAML `clockify:` block shape.
- [ ] FUNC-087: `workledger status` shall verify Clockify API reachability and credential acceptance when Clockify is configured.
- [ ] FUNC-088: `workledger status` shall confirm the authenticated Clockify user `id` exactly matches configured `clockify.user_id`.
- [ ] FUNC-089: `workledger status` shall confirm the configured `clockify.workspace_id` is visible through `activeWorkspace` or `defaultWorkspace`.
- [ ] FUNC-090: `workledger status` shall confirm configured Clockify workspaces and configured Jira targets are readable.
- [ ] FUNC-091: `workledger status` shall return no connectivity rows when no adapter families are configured.
- [ ] FUNC-092: `workledger status` shall skip connectivity rows for adapter families with zero configured targets.

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
- [ ] FUNC-102a: `workledger totals --route-profile <name>` shall be accepted with `--adapter=jira-cloud`, `--adapter=jira-data-center`, or `--instance <name>` when that instance resolves to exactly one configured Jira family.
- [ ] FUNC-102aa: `workledger totals --instance <name> --route-profile <name>` shall fail validation and require explicit `--adapter` when that instance name exists in both configured Jira families.
- [ ] FUNC-102b: `workledger totals --adapter=<jira-family> --route-profile <name>` shall limit totals scope to the selected route profile on the selected instance.
- [ ] FUNC-102c: `workledger totals --adapter=<jira-family> --route-profile <reporting-profile>` shall compare local source-prefix work matched by that profile against remote worklogs on that profile's configured `reporting_targets` issues.
- [ ] FUNC-103: `workledger totals --details` shall expand explicit single-result table output to include per-day rows.
- [ ] FUNC-104: `workledger totals --details` shall not change JSON output.

## Worklog Selectors
- [ ] FUNC-105: Date-window selectors shall support `--today`, `--yesterday`, `--tomorrow`, `--mon`, `--tue`, `--wed`, `--thu`, `--fri`, `--sat`, `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`.
- [ ] FUNC-105a: Shared date-window selectors shall support `--week-offset <int>` only as a modifier for exactly one selected weekday selector from `--mon` through `--sun`.
- [ ] FUNC-106: Active-worklog selectors shall support `--issue`, `--issue-prefix`, `--today`, `--yesterday`, `--tomorrow`, `--mon`, `--tue`, `--wed`, `--thu`, `--fri`, `--sat`, `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, and `--to`.
- [ ] FUNC-107: Active-worklog selectors shall support at most one `--issue` filter value per invocation.
- [ ] FUNC-107a: Active-worklog selectors shall support at most one `--issue-prefix` filter value per invocation.
- [ ] FUNC-109: `--fields` shall accept a comma-separated ordered subset of the selected record shape.
- [ ] FUNC-110: Planning issue selectors shall allow repeated `--issue <KEY>` values.
- [ ] FUNC-111: Planning issue selectors shall preserve operator-supplied issue order.

## Worklog Listing
- [ ] FUNC-112: `workledger worklogs list` shall require at least one explicit time selector from the shared date-window selector family.
- [ ] FUNC-113: `workledger worklogs list` shall render active local worklogs within the selected time scope.
- [ ] FUNC-115: `workledger worklogs list` shall support active-worklog selectors.
- [ ] FUNC-117: `workledger worklogs list` shall support the field selector.
- [ ] FUNC-118: `workledger worklogs list` shall return the full filtered result set without pagination.
- [ ] FUNC-119: `workledger worklogs list` shall expose active worklog JSON items using the default active-worklog record shape when `--fields` is not set.

## Worklog Search
- [ ] FUNC-121: `workledger worklogs search <query>` shall require one positional `<query>` argument.
- [ ] FUNC-122: `workledger worklogs search <query>` shall search canonical stored normalized `description` values by partial, case-insensitive substring match.
- [ ] FUNC-123: `workledger worklogs search <query>` shall treat `<query>` as a literal substring rather than wildcard syntax.
- [ ] FUNC-124: `workledger worklogs search <query>` shall search active local worklogs across all stored dates.
- [ ] FUNC-126: `workledger worklogs search <query>` shall reuse active-worklog selectors.
- [ ] FUNC-128: `workledger worklogs search <query>` shall reuse the field selector.
- [ ] FUNC-129: `workledger worklogs search <query>` shall return the full filtered result set without pagination.
- [ ] FUNC-130: `workledger worklogs search <query>` shall return exit code `0` when zero matches are found.

## Worklog Creation
- [ ] FUNC-136: `workledger worklogs add` shall require `--issue <KEY>`.
- [ ] FUNC-137: `workledger worklogs add` shall require exactly one of `--started <LocalTimestamp>`, `--started-utc <RFC3339UTC>`, `--fit`, or `--fill`.
- [ ] FUNC-138: `workledger worklogs add` shall require `--duration <GoDuration>`.
- [ ] FUNC-139: `workledger worklogs add` shall require `--description <text>`.
- [ ] FUNC-140: `workledger worklogs add` shall accept description input through a flag.
- [ ] FUNC-141: `workledger worklogs add --force` shall allow the operator to bypass duplicate or overlap rejection explicitly.
- [ ] FUNC-141a: `workledger worklogs add --dry` shall validate and preview one would-be local worklog without writing it.
- [ ] FUNC-141b: `workledger worklogs add --dry` shall use the same normalization, duplicate validation, overlap validation, and `--force` behavior as executed `worklogs add`.
- [ ] FUNC-141c: `workledger worklogs add --fit` and `workledger worklogs add --fill` shall reuse the `worklogs context` date-window selectors and workday-analysis inputs: `--today`, `--yesterday`, `--tomorrow`, `--mon`, `--tue`, `--wed`, `--thu`, `--fri`, `--sat`, `--sun`, `--current-week`, `--last-week`, `--current-month`, `--last-month`, `--from`, `--to`, `--day-start`, `--day-end`, `--lunch`, and `--no-lunch`.
- [ ] FUNC-141d: `workledger worklogs add --fit` and `workledger worklogs add --fill` shall apply `--week-offset` with the same weekday-only validation and week-shift semantics as `worklogs context`.
- [ ] FUNC-141e: `workledger worklogs add --fit` and `workledger worklogs add --fill` without an explicit date selector shall search the current local day.
- [ ] FUNC-141f: `workledger worklogs add --fit` shall search selected dates in ascending order, choose the earliest continuous free slot that contains the full requested duration, and create exactly one worklog.
- [ ] FUNC-141g: `workledger worklogs add --fit` shall not split across lunch, occupied gaps, dates, or local midnight.
- [ ] FUNC-141h: `workledger worklogs add --fill` shall search selected dates and free slots in ascending order and allocate the exact requested duration across one or more created worklogs.
- [ ] FUNC-141i: `workledger worklogs add --fill` may split across lunch, occupied gaps, and selected dates, but shall not cross local midnight within any fragment.
- [ ] FUNC-141j: `workledger worklogs add --fit` and `workledger worklogs add --fill` shall respect `day_start`, lunch exclusion unless `--no-lunch` is supplied, and occupied active local worklogs.
- [ ] FUNC-141k: Without `--overtime`, `workledger worklogs add --fit` and `workledger worklogs add --fill` shall consider only free slots whose actual local start is strictly earlier than the effective `day_end`; an eligible record may extend past `day_end` but shall not cross local midnight.
- [ ] FUNC-141ka: Without `--overtime`, an automatic placement slot beginning at or after `day_end` shall be skipped in favor of the next selected date, and automatic placement shall never extend beyond the selected date window.
- [ ] FUNC-141kb: `workledger worklogs add --fit --overtime` and `workledger worklogs add --fill --overtime` shall ignore `day_end` as a candidate-start limit while preserving `day_start`, lunch exclusion, occupied-worklog, minimum-duration, selected-date, and local-midnight constraints.
- [ ] FUNC-141kc: `workledger worklogs add --overtime` shall require `--fit` or `--fill` and shall not classify or persist created records as overtime.
- [ ] FUNC-141l: `workledger worklogs add --fit` and `workledger worklogs add --fill` shall fail validation with `no free slot available in the current time window` when placement is impossible even with overtime placement eligibility.
- [ ] FUNC-141la: When automatic placement without `--overtime` fails but the same request would succeed with overtime placement eligibility, validation shall append `use --overtime to allow placement starting at or after day_end` to the no-slot error.
- [ ] FUNC-142: `workledger worklogs add` shall auto-generate the created worklog `id`.
- [ ] FUNC-143: `workledger worklogs add` shall return the created canonical worklog on success.
- [ ] FUNC-143a: manual `workledger worklogs add` success output shall keep the single-record contract.
- [ ] FUNC-143b: automatic `workledger worklogs add --fit` and `workledger worklogs add --fill` success output shall return one `records` item per created worklog.

## Worklog Update
- [ ] FUNC-144: `workledger worklogs update <id>` shall use patch-style flags `--issue`, `--started`, `--started-utc`, `--duration`, and `--description`.
- [ ] FUNC-145: `workledger worklogs update <id>` shall require at least one patch flag.
- [ ] FUNC-146: `workledger worklogs update <id>` shall reject an invocation that supplies both `--started` and `--started-utc`.
- [ ] FUNC-147: `workledger worklogs update <id>` shall validate the full resulting record after patching.
- [ ] FUNC-148: `workledger worklogs update <id>` shall succeed and return the canonical record when normalization makes the patch a semantic no-op.
- [ ] FUNC-149: `workledger worklogs update <id> --force` shall allow the operator to bypass duplicate or overlap rejection explicitly.
- [ ] FUNC-150: `workledger worklogs update <id>` shall return not found when the requested active local worklog ID does not exist.
- [ ] FUNC-151: `workledger worklogs update <id>` shall return the updated canonical worklog on success.

## Worklog Presets
- [ ] FUNC-151a: `workledger presets` shall provide `add`, `list`, `show`, `update`, `delete`, and `apply` subcommands for operator-local reusable worklog presets.
- [ ] FUNC-151b: A preset shall contain a unique lowercase slug, issue key, local `HH:MM` start time, duration, and description.
- [ ] FUNC-151c: `workledger presets update <slug>` shall support patch-style `--name`, `--issue`, `--start`, `--duration`, and `--description` flags and require at least one field.
- [ ] FUNC-151d: `workledger presets apply <slug> --date <value>` shall create exactly one canonical local worklog by combining the selected local date with the preset start time.
- [ ] FUNC-151e: Preset application shall accept one-off `--issue`, `--start`, `--duration`, and `--description` overrides without mutating the preset.
- [ ] FUNC-151f: Preset application shall support `--dry` and `--force` with the same preview, duplicate, and overlap semantics as single-record `worklogs add`.
- [ ] FUNC-151g: Preset `--date` shall accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`, `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`, and signed day offsets in `+Nd` or `-Nd` form.
- [ ] FUNC-151h: Non-dry preset application shall atomically create the worklog and update preset recency without changing the preset content revision; a stale selected preset shall create neither change, and dry application shall remain read-only.
- [ ] FUNC-151i: Renaming or deleting a preset shall not mutate worklogs previously created from it.
- [ ] FUNC-151j: Shell completion shall suggest bounded, recency-ordered local preset slugs for preset show, update, delete, and apply commands.

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
- [ ] FUNC-173: `workledger worklogs delete <id>` shall atomically move the selected active local worklog into local trash.
- [ ] FUNC-174: `workledger worklogs delete` shall not expose delete-mode flags.
- [ ] FUNC-175: `workledger worklogs delete <id>` shall remain non-interactive once validation passes.
- [ ] FUNC-176: `workledger worklogs delete <id>` shall return deterministic success output with `id`, `trash_id`, `issue_key`, and `deleted_at`.
- [ ] FUNC-177: `workledger worklogs delete <id>` shall be local-only.

## Batch Delete
- [ ] FUNC-178: Filtered batch delete shall be available through the `workledger worklogs delete` command family.
- [ ] FUNC-179: Filtered batch delete shall reuse active-worklog selectors.
- [ ] FUNC-180: Filtered batch delete shall accept any non-empty valid selector subset from the active-worklog selector set.
- [ ] FUNC-181: Filtered batch delete shall require `--yes` for execution.
- [ ] FUNC-182: Filtered batch delete shall atomically move every matched active worklog into local trash when executed.
- [ ] FUNC-183: Single-delete by `<id>` and filtered batch-delete selectors shall be mutually exclusive modes.
- [ ] FUNC-184: Filtered batch delete shall be a valid no-op when zero active worklogs match the selector set.
- [ ] FUNC-185: Filtered batch delete shall apply to active worklogs only.
- [ ] FUNC-186: Filtered batch delete shall remain valid when exactly one active worklog matches.
- [ ] FUNC-187: Filtered batch delete shall support `--dry` to preview matched active worklogs without deleting them.
- [ ] FUNC-188: Batch-delete dry-run shall return the full matched active records together with the matched count.
- [ ] FUNC-189: Executed filtered batch delete shall return ordered `{id, trash_id}` mappings and deleted count rather than full deleted records.
- [ ] FUNC-189a: CLI filtered batch delete shall accept `--created-within <GoDuration>` as a delete-specific selector over local-ledger `created_at`.
- [ ] FUNC-189b: `--created-within` shall select active worklogs created during the inclusive interval from captured command time minus the duration through captured command time.
- [ ] FUNC-189c: `--created-within` shall be sufficient by itself for batch-delete mode and shall combine with issue and work-start selectors using AND semantics.
- [ ] FUNC-189d: `--created-within` shall remain CLI-only and shall not add a TUI creation-time delete control.
- [ ] FUNC-189e: Worklogs inserted by an applied reconciliation pull shall be eligible according to the time they entered the local ledger.

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

## Issues
- [ ] FUNC-219: `workledger issues list` shall read durable issues with effective optional metadata and lifetime active local budgets from SQLite only; without time selectors it shall include every local issue, including key-only and unused issues.
- [ ] FUNC-220: `workledger issues list` shall support `--issue`, exact local-group `--epic`, and the shared explicit time selectors.
- [ ] FUNC-221: `workledger issues list --issue <KEY>` without a time selector shall return that local issue or a not-found result.
- [ ] FUNC-222: `workledger issues list` with time selectors shall select distinct issues referenced by matching active local worklogs; each selected issue's budget shall still use its lifetime active local worklogs.
- [ ] FUNC-223: `workledger issues list` JSON output shall return `filters`, `items`, `total`, and a summary with issue counts, estimate coverage, logged duration, known estimated duration, known remaining duration, and overrun duration.
- [ ] FUNC-224: `workledger issues refresh --adapter=jira-cloud --field=max-estimate` shall refresh Jira Cloud original-estimate metadata into local SQLite.
- [ ] FUNC-225: `workledger issues refresh --adapter=jira-data-center --field=max-estimate` shall refresh Jira Data Center original-estimate metadata into local SQLite.
- [ ] FUNC-226: `workledger issues refresh` shall reuse active local-worklog selectors.
- [ ] FUNC-227: `workledger issues refresh` shall upsert local metadata rows for distinct selected issue keys.
- [ ] FUNC-228: `workledger issues refresh` shall report deterministic per-issue outcomes in `table` or `json` output.

- [ ] FUNC-228a: `workledger issues set --issue <KEY>` shall create a key-only issue or patch its optional local title, maximum estimate, and one epic group through `--title`, `--max-estimate`, `--epic`, `--clear-title`, `--clear-max-estimate`, and `--clear-epic`; omitted fields shall remain unchanged and setting and clearing the same field shall fail validation.
- [ ] FUNC-228b: `workledger issues apply --file <FILE>|--stdin` shall accept one non-empty JSON array of `{issue_key, title, max_estimate_seconds, epic}` patches; optional fields may be omitted to create a key-only issue, omitted fields shall remain unchanged, null shall clear a local field, unknown fields and duplicate keys shall fail validation, and the validated batch shall persist atomically. `--dry` shall validate without writing issues.
- [ ] FUNC-228c: Local maximum estimates shall override positive cached Jira estimates; clearing the override shall reveal the cache. Jira refresh shall update only cache fields and shall preserve local title, estimate, and epic fields. Clearing all optional local fields shall retain the issue identity without changing worklogs or cache.
- [ ] FUNC-228d: Issues list shall support `--issue-prefix` at the exact project boundary and `--search` for case-insensitive issue-key, title, or epic substrings, including in date-selected scopes; list ordering shall use project prefix followed by the issue number. Table output shall show readable estimate, logged, remaining, and overrun durations, epic, and estimate source; JSON shall preserve exact seconds and cache provenance.
- [ ] FUNC-228e: Session-worklog agents shall prefer explicit user issue selection, then an unambiguous current-session or branch key; after the work-evidence and explicit-duration gates, unresolved selection may inspect local issue metadata and choose only a clear single title match. Ambiguous or absent matches shall require an issue key; estimates shall never determine attribution or duration.

- [ ] FUNC-228f: Every successful active local worklog insertion or issue-key change shall create a missing local issue in the same transaction, including batch, preset, restore, and pulled worklogs; dry runs and failed writes shall create no issues, and this shall never create a remote issue.
- [ ] FUNC-228g: Epics shall be optional named local groups with at most one group per issue; they shall require no separate entity creation, remote lookup, or Jira hierarchy.
- [ ] FUNC-228h: Issue logged duration shall sum lifetime active local worklogs only, excluding trash and additional remote rows. A missing effective estimate shall produce unknown remaining duration; estimated issues shall expose `max(estimate - logged, 0)` remaining and `max(logged - estimate, 0)` overrun. Estimates shall remain informational and shall not restrict worklog creation.
- [ ] FUNC-228i: `workledger issues epics` shall group issues by local epic, include an ungrouped bucket when populated, support member `--issue-prefix` and case-insensitive group-name `--search`, and order named groups by name with ungrouped last. Group and list summaries shall sum per-issue known estimates, clamped remaining, and separate overruns, show estimated and unestimated issue counts, and identify unknown or partial remaining values without treating unestimated issues as zero estimates.
- [ ] FUNC-228j: Local issues shall record a creation timestamp when first introduced through explicit issue creation, worklog insertion or key changes, or metadata-cache insertion. Subsequent metadata edits, epic assignments, cache refreshes, worklog writes, and clearing optional fields shall preserve it. Explicit initialization shall migrate catalogs that lack creation timestamps without losing metadata or worklogs; historical timestamps shall be approximated from the earliest surviving local metadata, active-worklog, archived-worklog creation, or cache timestamp. TUI details shall show creation in the configured local timezone; missing creation values shall remain unknown and sort last, with natural issue-key ties.

## Reconcile Planning
- [ ] FUNC-229: `workledger plan reconcile` shall create remote-sync plans.
- [ ] FUNC-230: `workledger plan reconcile` shall default to push when neither `--pull` nor `--push` is supplied.
- [ ] FUNC-230a: `workledger plan reconcile` shall fail validation when both `--pull` and `--push` are supplied.
- [ ] FUNC-230b: Onboarding documentation and skills shall instruct operators to create, review, and apply a pull plan from every intended remote source for the complete first-push date window before creating a push plan, because push reconciliation may delete remote-only rows in that window.
- [ ] FUNC-231: `workledger plan reconcile` without `--adapter` or `--instance` shall target all configured reconcile-capable targets.
- [ ] FUNC-232: `workledger plan reconcile` shall require an explicit selected date window supplied by `--from` plus `--to` or by exactly one date-window shortcut selector.
- [ ] FUNC-233: `workledger plan reconcile` shall persist exactly one saved plan after command-level validation succeeds unless a reporting reconcile resolves only non-actionable scopes.
- [ ] FUNC-234: `workledger plan reconcile` shall return a no-plan result when a reporting reconcile finds only non-actionable scopes.
- [ ] FUNC-234a: A reporting no-plan result with at least one matched scope and zero actionable scopes shall report reason `exact_match`.
- [ ] FUNC-234b: Automatic multi-profile Jira push reconcile shall include exact-match reporting scopes in the saved merged plan when at least one selected adapter or profile creates a saved plan.
- [ ] FUNC-235: `workledger plan reconcile --pull --adapter=<family>` shall inspect remote worklogs only from the selected adapter family's configured routed `issue_prefixes` source scope.
- [ ] FUNC-235a: `workledger plan reconcile` shall accept repeated `--instance=<name>` as an adapter-instance allowlist, including the implicit Clockify instance `clockify` and configured Jira instances.
- [ ] FUNC-235b: explicit `--adapter` without `--instance` shall include the implicit Clockify instance when `clockify` is selected and all configured instances for each explicitly selected Jira adapter family.
- [ ] FUNC-235c: `workledger plan reconcile --adapter=<family> --instance=<name>` shall fail validation when an instance does not belong to one of the selected adapters.
- [ ] FUNC-235d: `workledger plan reconcile --instance=<name>` shall fail validation and require explicit `--adapter` when that instance name exists in both configured Jira families.
- [ ] FUNC-236: `workledger plan reconcile --pull --adapter=<family>` shall normalize remote observations into canonical candidate rows.
- [ ] FUNC-236a: Pull candidates shall round starts and durations to the nearest minute before saving and comparison; zero-duration results and rounded starts outside the selected window shall become invalid findings, and rounding-induced duplicates or overlaps shall block affected scopes. Remote observations, totals, and remote trash shall retain observed timing.
- [ ] FUNC-237: `workledger plan reconcile --pull --adapter=<family>` shall compare normalized observations with the current local canonical ledger.
- [ ] FUNC-238: `workledger plan reconcile --pull --adapter=<family>` shall produce a saved merge plan without mutating local canonical worklogs during reconcile.
- [ ] FUNC-238a: `workledger plan reconcile --pull` with multiple selected adapters or instances shall persist a saved `check_failed` plan instead of aborting when one selected adapter scope fails with a remote request error after command-level validation.
- [ ] FUNC-239: `workledger plan reconcile --push --adapter=<family>` shall load canonical local worklogs from SQLite for delivery planning.
- [ ] FUNC-240: `workledger plan reconcile --push --adapter=<family>` shall discover owned remote rows directly from the selected adapter scope and selected reconcile window, including issues or reporting targets that currently have no matching local rows.
- [ ] FUNC-241: Normal push planning shall plan remote cleanup or replacement when an owned remote scope has no matching local row-set in the selected window.
- [ ] FUNC-242: `workledger plan reconcile --route-profile=<name>` shall limit push planning to selected Jira targets that define the named route profile.
- [ ] FUNC-242a: `workledger plan reconcile --route-profile=<name>` shall fail validation unless every selected reconcile target is a Jira target.
- [ ] FUNC-243: `workledger plan reconcile` push planning shall include the configured `default` route profile for each selected Jira target when `--route-profile` is omitted.
- [ ] FUNC-243c: `workledger plan reconcile` push planning without `--route-profile` shall also include every non-default selected Jira route profile that uses `reporting_targets`.
- [ ] FUNC-243d: `workledger plan reconcile --route-profile=default` shall limit push planning to the `default` route profile only.
- [ ] FUNC-243e: `workledger plan reconcile` push planning without `--route-profile` shall fail clearly when the same source prefix is owned by more than one selected reporting-mode Jira route profile in the same adapter family.
- [ ] FUNC-243f: Automatic Jira push planning shall exclude exact configured reporting target issue keys from default-profile remote-owned discovery so reporting rows are not planned for cleanup as canonical local issue scopes.
- [ ] FUNC-243a: implicit all-target reconcile shall skip invalid configured targets when at least one valid target remains and shall surface those skipped targets in command output.
- [ ] FUNC-243b: implicit all-target reconcile shall fail validation when no valid reconcile target remains after filtering invalid configured targets, and the validation output shall include the per-target skip reasons.
- [ ] FUNC-244: `workledger plan reconcile --push --adapter=<family>` shall compute local-versus-remote diffs for selected scopes against resolved target adapter instances.
- [ ] FUNC-244a: human-readable `workledger plan reconcile` output shall include next-step commands for `plan show <plan-id>` and, when the saved plan contains one or more `ready` items, `plan apply <plan-id>`.
- [ ] FUNC-244b: human-readable `workledger plan reconcile` push output shall render a per-adapter summary breakdown for selected push targets, including adapter family, route profile when applicable, resolved target instances, scope count, actionable count, plan-created state, and reason when present; Jira targets shall use route-profile rows and Clockify shall use one non-profiled row.
- [ ] FUNC-244c: Saved-plan profile breakdown rows shall populate `reason`/`REASON` with a stable reason code when the profile has no actionable scopes or contains `check_failed` scopes; multiple reason codes shall render as `mixed`.

## Plan Review
- [ ] FUNC-246: `workledger plan show` shall load a saved plan by requested plan ID when provided.
- [ ] FUNC-247: `workledger plan show` shall load the most recent saved plan when no plan ID is provided.
- [ ] FUNC-248: `workledger plan show` shall render the saved reconciliation report without new external requests.
- [ ] FUNC-249: `workledger plan show` shall render deterministic output suitable for operator review.
- [ ] FUNC-250: `workledger plan show` shall report the plan's immutable `planning_status`, derived `execution_state`, and `plan_status` per scope.
- [ ] FUNC-251: `workledger plan show` shall report `planned_action` per scope.
- [ ] FUNC-252: `workledger plan show` shall report comparison status per scope.
- [ ] FUNC-253: `workledger plan show` shall report target adapter family, route profile when present, target issue, saved reconcile time window, local row count, remote row count, saved change counts, and execution state per scope.
- [ ] FUNC-253b: Human-readable `workledger plan show` output shall render one compact table row per scope with explicit `PROFILE`, `LOCAL`, `REMOTE`, `MATCH`, `CREATE`, and `DELETE` columns.
- [ ] FUNC-253c: When saved diff metrics are unavailable, human-readable `workledger plan show` output shall render `-` in `MATCH`, `CREATE`, and `DELETE` without any additional detail line.
- [ ] FUNC-253a: `workledger plan show` shall limit rendered scopes to saved plan items whose `plan_status` is `ready` by default; `workledger plan show --all` shall render all saved plan items regardless of status.

## Plan Listing
- [ ] FUNC-254: `workledger plan list` shall load saved-plan metadata from SQLite only.
- [ ] FUNC-255: `workledger plan list` shall render saved plans ordered by `created_at desc`, then stable plan ID.
- [ ] FUNC-256: `workledger plan list` shall include each plan's immutable `planning_status`, derived `execution_state`, and deterministic summary counts for total, actionable, open, and terminally succeeded items.
- [ ] FUNC-257: `workledger plan list` shall expose `plan_direction`, saved target adapter families, saved target instances, and saved reconcile time window.
- [ ] FUNC-258: `workledger plan list` shall support shared date-window selectors against saved plan `created_at` in the effective local timezone.

## Plan Apply
- [ ] FUNC-259: `workledger plan apply` shall load the requested plan ID when provided.
- [ ] FUNC-260: `workledger plan apply` shall load the most recent saved plan when no plan ID is provided.
- [ ] FUNC-261: `workledger plan apply` shall build tasks only from `ready` items whose execution state is `not_attempted`.
- [ ] FUNC-262: `workledger plan apply` shall fail validation when the saved plan contains zero unapplied `ready` items.
- [ ] FUNC-263: `workledger plan apply` shall use the saved scope definition and saved payload snapshot for execution.
- [ ] FUNC-264: `workledger plan apply` shall execute according to the saved `plan_direction`.
- [ ] FUNC-265: `workledger plan apply` shall execute only one saved plan at a time.
- [ ] FUNC-266: `workledger plan apply` shall record delete and create outcomes separately when one saved push item requires both steps.
- [ ] FUNC-267: `workledger plan apply` shall continue executing other eligible scopes when one scope fails.
- [ ] FUNC-268: `workledger plan apply` shall persist per-scope results independently.
- [ ] FUNC-268a: A saved plan shall set `applied_at` only after every actionable item has terminally succeeded.
- [ ] FUNC-269: `workledger plan apply` for `plan_direction=pull` shall merge the saved normalized remote payload into canonical local SQLite state.
- [ ] FUNC-270: `workledger plan apply` for `plan_direction=push` shall re-discover current remote worklogs inside the saved issue/window scope at execution time when cleanup is required.
- [ ] FUNC-271: `workledger plan apply` for `plan_direction=push` shall apply apply-time remote cleanup only for remote rows inside the saved target scope that are unmatched by the saved payload before creating missing replacement worklogs.
- [ ] FUNC-271a: `workledger plan apply` for `plan_direction=pull` shall archive local active rows removed by the saved merge into `workledger trash`.
- [ ] FUNC-271b: `workledger plan apply` for `plan_direction=push` shall archive successfully deleted remote cleanup rows into `workledger trash`.
- [ ] FUNC-271c: human-readable `workledger plan apply` success output shall include the aggregate trash archive count for the execution summary.
- [ ] FUNC-271d: Cancelling `workledger plan apply` before a remote mutation starts shall leave unscheduled items unattempted.
- [ ] FUNC-271e: Cancelling an in-flight remote mutation whose outcome cannot be confirmed shall leave its saved plan items uncertain for explicit reconciliation.

## Plan Retry
- [ ] FUNC-272: `workledger plan retry` shall load the requested saved plan by ID.
- [ ] FUNC-273: `workledger plan retry` shall operate on saved plan items only.
- [ ] FUNC-274: `workledger plan retry` shall require an explicit retry scope such as `--only failed` or `--only uncertain`.
- [ ] FUNC-275: `workledger plan retry <id> --only failed` shall process `ready` items with `execution_state=failed`.
- [ ] FUNC-276: `workledger plan retry <id> --only uncertain` shall process `ready` items with `execution_state=uncertain`.
- [ ] FUNC-276a: `workledger plan retry` shall fail validation when the selected retry scope contains no matching `ready` items.
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

## Tombstone Commands
- [ ] FUNC-318: `workledger worklogs update <id> --issue <new-key>` shall mutate the active local worklog in place without persisting delete intent for the previous issue allocation.

## Trash Commands
- [ ] FUNC-318a: `workledger trash list` shall require at least one explicit time selector from the shared date-window selector family.
- [ ] FUNC-318b: `workledger trash list` shall support `--issue` and `--issue-prefix` filters and date-window selectors.
- [ ] FUNC-318c: `workledger trash search <query>` shall require at least one explicit time selector from the shared date-window selector family.
- [ ] FUNC-318d: `workledger trash search <query>` shall search trashed `description` values by partial, case-insensitive literal substring match.
- [ ] FUNC-318e: `workledger trash show <id>` shall load one archived trash row by trash archive ID.
- [ ] FUNC-318f: `workledger trash` records shall expose whether the archived row origin is `local` or `remote`.
- [ ] FUNC-318g: `trash list` and `trash search` shall accept `--scope local|remote`; omission shall include both storage scopes.
- [ ] FUNC-318h: `trash restore <id>` shall restore one local trash row under its original worklog ID and consume the trash row atomically.
- [ ] FUNC-318i: Filtered `trash restore` shall reuse trash issue and original-start date selectors, require exactly one of `--dry` or `--yes`, restore only local rows, and reject ID mode combined with batch flags.
- [ ] FUNC-318j: Trash restoration shall have no force or partial mode; any active-ID, duplicate, overlap, internal-batch, or confirmed-membership conflict shall reject the complete operation without consuming trash.
- [ ] FUNC-318k: Remote trash and local trash without `source_worklog_id` shall remain audit-only and non-restorable.
- [ ] FUNC-318l: `workledger trash delete <id>` shall permanently remove one local or remote trash record after exactly one of `--dry` or `--yes` is supplied.
- [ ] FUNC-318m: Filtered `trash delete` shall reuse issue, issue-prefix, original-start date, and scope selectors and shall accept `--trashed-within <GoDuration>` over `trashed_at`.
- [ ] FUNC-318n: Filtered `trash delete` shall require at least one selector, treat `--scope` as sufficient, combine selectors with AND semantics, and require exactly one of `--dry` or `--yes`.
- [ ] FUNC-318o: `workledger trash clear` shall target all local and remote trash records, accept no selectors, and require exactly one of `--dry` or `--yes`.
- [ ] FUNC-318p: Permanent trash deletion shall remove only local SQLite trash records and shall not mutate active worklogs, saved plans, or remote services.
- [ ] FUNC-318q: Permanent trash deletion and clear shall remain CLI-only.

## Activity History
- [ ] FUNC-319: Workledger shall persist diagnostic CLI and TUI activity in the configured local SQLite store with source, canonical operation, safe summary and attributes, lifecycle state, UTC timestamps, duration, optional exit code, and sanitized failure details.
- [ ] FUNC-319a: Activity history shall retain the newest 500 entries in deterministic `started_at desc`, then `id desc` order and shall remain diagnostic rather than an immutable audit log.
- [ ] FUNC-319b: CLI activity shall be best-effort and silent, shall never change stdout, stderr, or exit status, and shall be omitted when valid configuration and compatible SQLite storage are unavailable.
- [ ] FUNC-319c: CLI activity shall record resolvable executable leaf commands, including validation failures, dry runs, completion generation, version, activity listing, and TUI, while excluding help, unknown commands, and hidden completion callbacks.
- [ ] FUNC-319d: CLI activity shall persist canonical command paths and allow-listed identifiers or selectors only and shall not persist raw argv, descriptions, search text, URLs, emails, paths, configuration values, input payloads, or credential-related values.
- [ ] FUNC-319e: CLI terminal activity states shall map exit `0` to `succeeded`, exit `6` to `partial`, exit `130` to `canceled`, and other non-zero exits to `failed`.
- [ ] FUNC-319f: `workledger activity list` shall support table and JSON output, default `--limit` to 50, accept limits from 1 through 500, and support optional `--source=cli|tui` and `--state=running|succeeded|failed|partial|canceled` filters.
- [ ] FUNC-319g: `workledger activity list` shall exclude its own running entry from the current result while retaining its completed entry for later reads.

## TUI
- [ ] FUNC-303: `workledger tui` shall open the alternate-screen interactive frontend only when stdin and stdout are terminals and shall reject positional arguments and explicit `--output` selection.
- [ ] FUNC-303a: The TUI shall expose six destinations labeled `1 Status`, `2 Worklogs`, `3 Plans`, `4 Presets`, `5 Trash`, and `6 Issues` in the destination rail; the action bar shall not repeat those destination shortcuts.
- [ ] FUNC-303b: Healthy startup shall open Worklogs for today while Status diagnostics load in the background; invalid configuration or unavailable or incompatible local storage shall open a diagnostics-only Status view.
- [ ] FUNC-303c: Status shall display structured config, storage, environment, routing, and adapter-connectivity results with category, target, state, message, failure kind, health counts, and last-check time.
- [ ] FUNC-303ca: Status shall expose `Status` and read-only `Config` subviews; Config shall show the same validated effective summary as `workledger config`, show validation issues when no valid summary is available, and refresh with the diagnostics snapshot.
- [ ] FUNC-303d: Worklogs shall expose Day, Week, and Month modes inside the existing Worklogs tab, default to Day, retain one selected local date across modes, and summarize its local Monday-through-Sunday week in Day and Week modes or its local calendar month in Month mode.
- [ ] FUNC-303da: The daily Worklogs list shall persistently show the selected day's booked duration, remaining daily quota, and a current-state timeline above the worklog rows, including on empty days.
- [ ] FUNC-303db: The current-state and placement timelines shall use a chronological ruler above a continuous segmented bar divided internally into 15-minute cells aligned to local `:00`, `:15`, `:30`, and `:45` boundaries in Day, Week, Month, and placement previews. Only the selected-day timeline within Week browsing shall use 1-hour cells aligned to local `:00` boundaries when that day exceeds its daily quota; without overtime it shall retain 15-minute cells. Day, Month, and all Add, Edit, and Move previews, including those opened from Week, shall retain 15-minute cells regardless of overtime. Overtime shall begin where cumulative logged duration in chronological order reaches that quota, and shall mark subsequent booked or proposed portions, including a boundary inside a worklog. Starting early, working through lunch, or working after `day_end` shall contribute regular time until the quota is reached; unbooked gaps and lunch shall not consume quota. Concurrent worklogs shall contribute their individual durations, consistently with the daily booked total. Proposed placements shall reclassify later persisted work as necessary, and cancellation shall restore the persisted classification. The bar shall use one proportional grid across booked, selected, free, lunch, and proposed segments, including when actual work extends its bounds. The bar shall fill the entire available width without trailing unused columns. Equal-duration complete cells shall share one proportional scale, with unavoidable terminal rounding distributed across the grid so their widths differ by at most one column. Compact timelines shall retain their view's active time grid; dividers that cannot preserve the underlying state pattern may yield at character resolution. Partial cells shall retain actual interval times rather than round bookings or proposals to the cell resolution. Internal rules shall retain the underlying occupied fill, leave the segment edges intact, and preserve visible state patterns when terminal resolution limits detail. The bar shall use solid regular booked time and warning-colored overtime with filled cell dividers, outlined free intervals, and full-height diagonal configured-lunch hatching enclosed in a thin border with its time range beneath the bar when space permits. Selection shall mark the complete worklog interval with a focus-colored range bracket in a dedicated annotation row between the ruler and the bar, with downward endcaps aligned to its first and last visible columns, while retaining its regular booked or overtime fill, dividers, and bottom edge. A selection occupying one column or too short to reach a sampled midpoint shall use a downward caret at its interval midpoint without widening or recoloring the occupied bar. With colors unavailable, the same bracket or caret shall identify selection while retaining the underlying booked and overtime patterns. The annotation row shall remain reserved when no worklog is selected. Selection shall never replace any bar cell, including its top and bottom edges, and its legend shall show the bracket and caret shapes. Timeline chrome shall yield before essential worklog details or placement controls when space is limited. Narrow lunch cells shall retain that hatch rather than substitute a boxed glyph; borders may share neighboring free edges, and internal dividers shall yield to the hatch when space is limited. Booked, selected, proposed, lunch, and free segments shall share the same visible bar height, with outlined sides aligned to their time-cell edges. Selection shall retain the same time scale and interval widths, update with the selected table row, and keep at least one visible cell for a non-zero worklog. The timeline shall extend through actual persisted or proposed time before `day_start` and after `day_end`, clip bookings to the selected local day, and distinguish quota-based overtime within a selected interval. The ruler shall show plain clock labels without added workday-boundary separators. Colors shall represent state rather than issue prefixes. Persisted bookings shall take precedence over overlapping lunch; the lunch legend and label shall disappear when no lunch cell remains visible.
- [ ] FUNC-303dbb: Day mode shall show the authoritative available windows and total free duration beneath the worklog list, indicate additional windows when space is limited, and explain excluded configured lunch time. Up/down row selection shall update the complete timeline selection and adjacent inspector together.
- [ ] FUNC-303dbc: `/` shall focus a case-insensitive local filter over the loaded day's issue keys, available titles, and descriptions; Enter shall browse matching rows, Esc shall clear the filter and restore the earlier selection when available, and refresh shall retain a matching selection by ID. Filtering shall not change booked totals, timeline occupancy, availability, or Week-mode day deletion; no match shall disable selected-row mutations.
- [ ] FUNC-303dbd: Enter on a selected Day worklog shall open scrollable details containing full description, identifiers, and local lifecycle timestamps; closing shall retain the selected date and row.
- [ ] FUNC-303dbe: Enter shall expand Status diagnostics, effective configuration fields, presets, Day trash records, and saved plan scopes into scrollable full details. Scope details shall include every saved desired entry with its timestamp, duration, issue key, and description; closing shall preserve the underlying destination and selection.
- [ ] FUNC-303dc: Week mode shall show one row for each Monday-through-Sunday date with booked duration, worklog count, weekday quota delta, and collision count, while Saturday and Sunday quota delta shall render as not applicable.
- [ ] FUNC-303dd: Week mode shall show the selected day's balance, current-state timeline, available windows, and worklog rows beside and beneath the weekly overview; `j`/`k` or up/down shall select a day without crossing the displayed week, `h`/`l` or left/right shall move by whole weeks, Enter shall open the selected date in Day mode, and `t` shall select today in its week.
- [ ] FUNC-303de: Week mode shall allow Add for the selected date, shall not expose Edit until the operator opens Day mode and selects a worklog, and shall use uppercase `D` to offer moving every active local worklog whose local start date is the selected date into local trash.
- [ ] FUNC-303dea: Week-mode selected-day deletion shall be a valid no-op on an empty day; otherwise it shall preview the exact worklog count and total duration, name the selected local date, explain conflict-free restoration, and require explicit confirmation.
- [ ] FUNC-303deb: Confirmed Week-mode selected-day deletion shall atomically delete exactly the previewed IDs and revisions and shall reject the operation without partial deletion when selected-day membership or any revision changed after confirmation opened.
- [ ] FUNC-303df: Worklogs modes shall render the same `Day`/`Week`/`Month` view selector beside or directly below their primary heading and use brackets as a non-color indication of the active mode; lowercase `d`, `w`, and `m` shall select Day, Week, and Month, while uppercase `D` shall initiate deletion only in Day or Week mode.
- [ ] FUNC-303dg: Month mode shall show a Monday-first, seven-column calendar with each in-month date's booked duration, explicit collision and temporal markers, and a non-color selected-date indication; adjacent-month padding cells shall be empty and unselectable. Its heading shall label the calendar-month total as local booked time without implying remote delivery or month completion.
- [ ] FUNC-303dh: Month mode shall separately count empty and non-empty below-quota past Monday-through-Friday dates against the configured daily minimum quota without netting extra hours on other dates; today shall be marked in progress, future dates pending, and weekends exempt from gap counts. The nominal weekday assumption shall be explained in the Actions menu without implying holiday or leave adjustments.
- [ ] FUNC-303di: Month-mode left/right and `h`/`l` shall select the previous or next date, up/down and `k`/`j` shall select the same weekday one week earlier or later, and navigation shall stop at the displayed month boundary. Page Up/Page Down shall change calendar months and clamp the selected day number to the destination month's last date; left click shall select an in-month calendar cell, Enter shall open Day mode, and `t` shall select today in its month.
- [ ] FUNC-303dj: Month mode shall show selected-day booked time, temporal state or quota shortfall, collision warnings, and a short worklog preview beneath the calendar; the current-state timeline and its legend may collapse as space decreases. Add and add from preset shall use the selected date and the ordinary guarded worklog form; Edit and Delete shall require opening Day mode, and no whole-month mutation shall be exposed.
- [ ] FUNC-303e: Worklogs shall support single local worklog add, edit, and explicitly confirmed delete while preserving existing validation, collision, and optimistic-revision rules; the selected-worklog detail shall display its created and last-updated timestamps in the effective local timezone.
- [ ] FUNC-303ea: Day-mode selected-worklog detail shall display `Local issue total` as the all-time sum of durations for active local worklogs whose issue key exactly equals the selected worklog's issue key; trash and remote worklogs shall be excluded.
- [ ] FUNC-303eaa: Day-mode selected-worklog detail shall show an available local issue title and, for a positive effective maximum estimate, `Estimate used` as the all-time active local issue total divided by that estimate with one decimal percentage and logged/estimate durations; values above 100% shall remain uncapped and show the excess duration. Missing or non-positive estimates shall omit usage, and loading or failed totals shall never appear as zero or a completed metric. Estimates shall be informational and shall not block worklog creation; timestamps, identifiers, and description detail may collapse as the workspace shrinks to preserve issue metrics.
- [ ] FUNC-303eb: Worklogs Day mode shall expose uppercase `M` Move only for a loaded selected worklog; Move shall retain that worklog's ID and preview its original and proposed local time windows and the proposed interval on the day timeline. Up/down arrows shall step chronologically through deduplicated valid destinations immediately before or after each other booking, at the configured day start, and ending at the configured day end. The source shall be excluded from occupancy; the complete moved interval shall fit inside the configured workday on the source's local date, avoid configured lunch and all other active bookings including intervals starting on another date, and preserve duration, issue, description, and creation time without splitting or shifting other rows.
- [ ] FUNC-303ec: Move shall initially retain the original start, require `Ctrl+S` to save a changed valid destination, use `Esc` to cancel without writing, and offer `r` to explicitly refresh the source and positions. Boundary navigation shall explain when no further valid position exists; unchanged or unavailable destinations and loading previews shall not submit. Saving shall prevent duplicate submissions and interaction changes until completion, preserve selection by ID after table reordering, and record the mutation as worklog-update activity. External changes shall invalidate the preview before saving; changed availability shall refresh positions without automatically saving, and stale or deleted sources shall block saving until explicitly refreshed or canceled. Move guidance shall appear only in the action bar.

- [ ] FUNC-303f: TUI Add shall expose `Fit`, `Fill`, and `Manual` placement modes, default to `Fill`, and apply automatic placement only to the currently selected local day.
- [ ] FUNC-303fa: TUI `Fit` shall preview and create one continuous worklog in the earliest valid free slot, while TUI `Fill` shall preview and atomically create one or more worklogs across the earliest valid free slots.
- [ ] FUNC-303fb: TUI automatic-placement previews shall require issue and duration, show the resulting local time windows before submission, ignore obsolete generations, and preserve the draft while recalculating after external changes.
- [ ] FUNC-303fba: TUI Add shall identify the selected day, show its booked duration and remaining daily quota, explain the selected placement mode, and visually separate direct-entry worklog details, user-controlled placement, and calculated output using persistent non-color structure, including one blank row between the final worklog field and Placement and one blank row between the calculated timeline and its legend when the legend is shown; its calculated timeline shall distinguish booked time, the proposed placement, lunch, and free time, and shall omit the `lunch` and `free` legend items when no matching cell is rendered. Save and cancel shortcuts shall appear only in the form action bar, while secondary placement explanation and calculated detail may collapse before the three group cues at the minimum supported viewport.
- [ ] FUNC-303fbaa: When a preview extends past the configured `day_end`, the TUI timeline shall extend through the latest preview end on the selected local day; its proposed regular and overtime portions shall follow the cumulative daily-quota rule, independently of the configured clock boundaries.
- [ ] FUNC-303fbb: A successful TUI automatic-placement preview shall show the number of worklogs to be created, the first resulting local time windows, and the projected daily total relative to the configured quota.
- [ ] FUNC-303fbc: TUI Add and Edit shall asynchronously show the entered issue's available local title and effective maximum estimate without contacting remote adapters, including before duration is entered; secondary issue information may collapse when space is exhausted.
- [ ] FUNC-303fbd: TUI Add shall asynchronously query the local issue catalog by case-insensitive prefix for bounded Issue-field completion, order used keys by latest local entry before unused catalog keys, and let Tab accept the current completion without leaving the field; completion failure shall remain non-fatal and shall not contact remote adapters.
- [ ] FUNC-303fbe: After the operator leaves a non-empty Issue field, TUI Add shall asynchronously load a bounded list of distinct descriptions most recently used by that exact active local issue, expose case-insensitive prefix completion in the Description field, and require explicit Tab acceptance; description completion failure shall remain non-fatal and shall not contact remote adapters.
- [ ] FUNC-303fbf: An empty TUI Add Issue field shall offer the first key in the ordered local issue catalog and shall require explicit Tab acceptance before using it.
- [ ] FUNC-303fbg: TUI Add and Edit shall normalize unambiguous duration forms including whitespace-separated Go-duration components and `H:MM` before preview or submission, while leaving ambiguous or invalid values for ordinary validation.
- [ ] FUNC-303fbh: TUI Add shall open a local issue picker from the focused Issue field with `Ctrl+K`; it shall open in browse mode, use `Ctrl+F` to open case-insensitive issue-key prefix search and cycle prefix, worklog-description substring, and title substring search, allow paging through all matching keys, and show available titles beside keys and local last-entry dates or `Unused`. Enter shall fill the draft without saving, and Esc shall close search before closing the picker without changing the draft.
- [ ] FUNC-303fbi: The TUI Add picker and Issue-field completion shall use the durable local issue catalog, including issues created through pulled worklogs, without deriving keys or descriptions from trash. Latest active-worklog `created_at` per key shall rank used keys, including when an older description matches; unused catalog keys shall follow by latest local update or cache refresh, with key-order ties. The separately scrollable side pane shall show available title and effective maximum estimate followed by all active worklog descriptions, each with local date and duration, preserving repeated descriptions and paging beyond the first batch.
- [ ] FUNC-303fc: TUI automatic-placement submission shall require the persisted placement to match the displayed preview; changed availability shall reject persistence and produce a refreshed preview.
- [ ] FUNC-303fd: TUI Add shall use `Ctrl+P` to cycle `Manual`, `Fit`, and `Fill`; automatic Add shall expose draft-local late-start eligibility and lunch toggles through `Ctrl+O` and `Ctrl+L` in the form action bar, label each shortcut with the action it would perform for the current state, recalculate the preview after each activation, initialize new drafts with starts at or after day end disabled and the effective lunch reserved, and describe recovery without referring to CLI flags.
- [ ] FUNC-303fe: TUI Manual Add shall expose issue, local start time in `HH:MM` form, duration, and single-line description fields without supplying a potentially conflicting start value; it shall compose the start timestamp from the selected local date and entered time before reusing existing timestamp, duplicate, and overlap validation. An empty Manual Start time shall offer the maximum local end time of active worklogs starting on the selected date, rounded up to the next representable minute when necessary, through explicit Tab acceptance without moving focus or overwriting typed input; it shall offer no value when no qualifying worklog exists, the resulting end crosses the selected local-day boundary, or its local minute is ambiguous. Once issue, start time, and duration are valid, Manual Add shall preview the proposed interval in the same calculated timeline, result, legend, and projected daily total used by automatic placement; an overlapping or duplicate proposal shall remain visible with an explicit non-color conflict distinction. TUI Edit shall retain its complete local date-time field. Detected duplicates or overlaps shall require a separate explicit force confirmation.
- [ ] FUNC-303ff: TUI Edit shall remain manual and shall not reposition a record through Fit or Fill.
- [ ] FUNC-303g: A stale edit shall preserve its draft, block submission, and offer confirmed reload; a stale delete shall reload the worklog view without forcing deletion.
- [ ] FUNC-303h: The TUI shall poll SQLite external-change state once per second, refresh idle worklog views while preserving selection by ID, and mark open editors stale without overwriting drafts.
- [ ] FUNC-303i: Status and worklog refreshes shall ignore late results from obsolete request generations.
- [ ] FUNC-303j: The TUI shall support numbered tab activation that immediately focuses the destination workspace, Tab/Shift+Tab panel focus cycling, Status `s`/`c` or left/right subview selection, `w` Week mode, Worklogs `m` Month mode, lowercase `d` Day mode, uppercase `D` mode-specific deletion, mode-aware arrow and Vim navigation, Month Page Up/Page Down navigation, `↑`/`↓` worklog-form field navigation alongside Tab/Shift+Tab, `t` today, Worklogs `a` blank add and uppercase `A` add from preset, Day-mode `e` edit and uppercase `M` move, `r` refresh, `?` searchable contextual Actions, `/` local Day filtering, Enter to open Day worklog details or the selected Week or Month date, `Esc` cancel, Add-form `Ctrl+K` issue history, `Ctrl+P` placement cycling, automatic-Add `Ctrl+O` late-start eligibility and `Ctrl+L` lunch toggles, `Ctrl+S` save, `q` quit, and `Ctrl+C` interrupt; displayed key-to-action guidance shall appear only in the action bar and the shared Actions menu except for the six numbered destination labels in the rail, Placement shall stay outside ordinary form traversal, Enter shall advance single-line fields, and Enter on Description shall save through the same guarded path as `Ctrl+S`.
- [ ] FUNC-303j0: `?` shall open a searchable menu of actions available for the current destination and selection. Typing shall filter actions, arrows shall select, Enter shall dispatch the same guarded action as its keyboard shortcut, and Esc shall close without changing the underlying selection or draft. Actions requiring confirmation shall retain that confirmation.
- [ ] FUNC-303ja: A left mouse click shall activate any available Status, Worklogs, Trash, Presets, Plans, or Issues rail row and immediately focus its workspace; keyboard rail browsing shall retain rail focus until Enter or Space transfers focus to the selected destination workspace, and navigation clicks shall be ignored while a form, operation, or confirmation is open and while resize guidance is displayed.
- [ ] FUNC-303jb: Trash shall share the selected local date with Worklogs, default to Day mode and All scope, preserve its Day/Week and All/Local/Remote selections during the session, and use `s` to cycle scope.
- [ ] FUNC-303jc: Trash Day mode shall list archived rows for the selected original-start date and use uppercase `R` to restore the selected local row; selecting a remote row shall explain that it is audit-only, and selected-item detail shall describe restore eligibility without repeating the restore shortcut shown in the action bar.
- [ ] FUNC-303jd: Trash Week mode shall summarize local and remote trash by Monday-through-Sunday day and use uppercase `R` to atomically restore the exact confirmed local subset for the selected day while leaving remote rows untouched; selected-day detail shall describe atomic restoration without repeating the restore shortcut shown in the action bar.
- [ ] FUNC-303je: Trash restore confirmation shall show date, count, duration, reason or origin, original ID for a single row, and a reconciliation-drift warning for pull-origin trash.
- [ ] FUNC-303k: Terminals smaller than 100 columns by 28 rows shall show only resize guidance while retaining loaded application state.
- [ ] FUNC-303ka: The bordered action bar shall be the TUI's only bottom element and shall display transient guidance and validation notices on the same row as essential action controls without adding a separate persistent status strip; outcomes persisted as activity shall not be repeated in the action bar.
- [ ] FUNC-303l: Normal TUI quit shall exit 0, non-terminal invocation or command validation shall exit 2, explicit interruption shall exit 130, and unexpected runtime failure shall exit 1.
- [ ] FUNC-303m: The TUI action bar shall use one content row beneath its separator, prioritize contextual actions, and leave destination navigation in the rail and Actions menu.
- [ ] FUNC-303n: The Presets tab shall list recency-ordered presets, show selected-preset detail, and support add, edit, confirmed delete, refresh, and keyboard row navigation.
- [ ] FUNC-303na: The Issues tab shall show lifetime active local issue budgets in a dominant list with a selected-issue inspector, optional title and epic, effective estimate source, readable lifetime logged and remaining durations in list rows, and separate overruns; missing estimates, partial totals, loading, empty, and failed states shall remain explicit without remote requests.
- [ ] FUNC-303nb: Issues `t` shall toggle Issues and Epics views; Epics shall show group budgets with estimate coverage and a distinct ungrouped bucket, and Enter shall drill into the selected group's members. `/` shall search issue key, title, and epic in Issues or group name in Epics; With no active range or marked batch, Esc shall clear search and then return a member list to its selected group, preserving view context and selection.
- [ ] FUNC-303nc: Issues `a` shall add a key with independently optional title, manual maximum estimate, and epic; `e` shall edit the selected issue's optional fields while keeping its key immutable. Forms for new issues shall omit blank optional fields; while editing, blank optional fields shall clear local values, clearing an estimate shall reveal a cached Jira value, `Ctrl+S` or Enter on the final Epic field shall save through shared validation, and Esc shall cancel without writing. New issues created within a group member view shall inherit that group. Add and edit shall wait for an available catalog.
- [ ] FUNC-303nd: Issues shall support keyboard row navigation, `r` refresh, and Enter to open wrapped scrolling issue details; relevant local changes shall refresh budgets while preserving selection, and unavailable queries or validation failures shall expose an actionable retry or correction without displaying unavailable budgets as zero.
- [ ] FUNC-303ne: Issues `s` shall open a sort menu with natural issue key ascending, remaining descending, overrun descending, title A–Z, logged time descending, and creation time descending; Epics shall offer name A–Z, remaining descending, overrun descending, and logged time descending. Enter shall apply and Esc shall cancel. Issues shall default to newest local creation first, including group member views; Epics shall default to name A–Z. Each view shall retain its own choice for the session, show the active sort near the list heading, preserve selection by issue key or epic name across sorting and refresh, and retain the issue sort in epic member lists. Missing values for the selected sort shall sort last; logged time shall remain known regardless of estimate availability and unused issues shall have zero logged time. Ties shall use natural issue keys or case-insensitive epic names with deterministic case ties and ungrouped last among ties. Sorting shall not change totals or hide exhausted estimates.
- [ ] FUNC-303nf: Issues, epic members, and Epics shall support Page Up/Page Down jumps across one visible page with one item of overlap and Home/End selection of the first/last matching item, clamped without wrapping. Paging shall use the current filtered order and available viewport after headers, wrapped summaries, search, resize, and Activity; epic pages shall account for whole group row heights and advance at least one item when a group fills the viewport. The inspector shall follow the selected item. While editing search text, Home/End shall retain cursor behavior; details shall retain their independent scrolling behavior. These commands shall be discoverable in Actions and compact page and sort hints shall appear in the action bar.
- [ ] FUNC-303ng: Issues and epic member lists shall separate row focus from a marked batch: Space shall toggle the focused issue and advance, `v` shall start or finish a contiguous selection range in the current filtered sort order, `V` shall do the same for unselection, and Ctrl+A shall mark all matching issues. Range navigation shall use existing arrows, paging, and Home/End; shrinking a range shall restore prior marks outside it. Esc shall finish a range, then clear marks, then clear search or leave the group. Search editing shall retain text-input behavior. Marks shall follow issue keys across sorting, filtering, view changes, refresh, and resize; changing the ordered view or refreshing shall finish any range. The list shall show color-independent marks, total selection count, and a count outside the current view; unavailable selected keys shall be removed with feedback.
- [ ] FUNC-303nh: Issues `b` shall open a bulk epic picker for marked issues when the catalog is available, showing the frozen batch count, issue-key preview, and hidden selection count. Users shall choose a matching existing local epic, enter a new local name, or explicitly choose no epic to remove assignment; blank input shall never silently clear assignments. Enter shall atomically apply epic-only patches to the entire selected batch, including hidden selections, without changing titles, estimates, or worklogs. Success shall clear the submitted marks and refresh issue and epic totals; cancel, validation, or write failure shall retain the batch and focus, and failed writes shall retain the destination for retry. External changes shall block stale application until the picker is reopened. Saving shall prevent duplicate submissions, and the picker shall fit the supported viewport with scrolling destinations and discoverable shortcuts.
- [ ] FUNC-303ni: Enter on an issue shall open adjacent metadata and worklog-history panes for that exact issue. History shall include every active local worklog across all dates, preserve repeated descriptions in latest-entry order, and show local start date/time, duration, and wrapped full description. The history pane shall initially own focus; Tab or left/right shall change pane focus, arrows or j/k shall scroll, Page Up/Down shall follow the visible pane height, and Home/End shall reach the first/last content, fetching remaining history pages when needed. `r` shall refresh metadata and history and retry failures; relevant external changes shall restart history. Empty, loading, partial-page, and failed states shall be explicit, with loaded history retained on a page failure. Esc or Enter shall return to the same issue list, filter, sort, and marked batch.
- [ ] FUNC-303o: TUI preset add and edit shall expose name, issue, local start time, duration, and description and shall reuse local issue and issue-scoped description completion.
- [ ] FUNC-303p: In every Worklogs mode, lowercase `a` shall open the blank Add form directly and uppercase `A` shall open a preset picker in browse mode; `Ctrl+F` shall reveal and focus its prefix-search field, and picker shortcuts shall appear only in the action bar. When no presets exist, uppercase `A` shall leave Worklogs unchanged and explain that a preset can be created in the Presets tab.
- [ ] FUNC-303q: Selecting a preset shall open the ordinary editable Add form in Manual placement on the selected day with preset values prefilled.
- [ ] FUNC-303r: A stale or deleted source preset shall preserve the populated worklog draft, detach it from the preset, and require the operator to review and save again.
- [ ] FUNC-303s: Preset editors and deletes shall use optimistic revisions, and external changes shall refresh idle preset views while preserving stale drafts.
- [ ] FUNC-303t: The TUI shall persist foreground Status, Worklogs, Trash, and Presets refreshes and mutations as activity while excluding startup loads, navigation, polling, completion, previews, external reloads, drawer reads, and canceled confirmations.
- [ ] FUNC-303u: Lowercase `g` shall toggle a global seven-row Activity drawer beneath the workspace in the right panel and shall be discoverable through the Actions menu; the drawer shall show persisted CLI and TUI worklog mutations, local issue metadata or epic assignment batches, trash restores, preset mutations or applications, and plan reconcile, apply, or retry attempts in every lifecycle state while excluding reads, refreshes, setup and remote metadata refresh, and the CLI TUI launcher; those visible activity outcomes shall not be repeated in the action bar; when open, Tab shall include the drawer in panel focus, arrow or `j`/`k` keys shall select entries, Page Up/Page Down shall page, Home/End shall jump to the oldest/newest entry, `r` shall refresh it, and left click shall focus or select it.
- [ ] FUNC-303v: The Activity drawer shall be hidden by default, preserve selection by activity ID, follow new entries only while the newest entry is selected, and be temporarily suppressed while a form, picker, Actions, details, or confirmation overlay is open.
- [ ] FUNC-303w: Plans shall share the selected local date with Worklogs, default to Week view, and show the 100 most recent saved plans created during the selected local Monday-through-Sunday week; allow `d`/`w` to switch between the selected day and week, `h`/`l` to move by one day or week, and `t` to return to today; preserve selection by plan ID across same-range refreshes; reset selection to the newest available plan when the range changes; summarize visible unapplied, failed, and uncertain ready scopes in the rail; and open a saved local-only scope review without contacting remote adapters.
- [ ] FUNC-303x: TUI reconcile shall require an explicit Push or Pull direction, a selected Day, Week, or custom inclusive local-date window, and either all valid configured targets or one explicitly selected target; a single selected Jira push target shall allow automatic or explicit configured route-profile selection, and the TUI shall display the effective direction, window, target, and profile before confirmation. Plan-creation shortcuts shall appear only in the action bar.
- [ ] FUNC-303y: Confirmed TUI reconcile shall inspect remote state and save a reviewable plan without applying it, shall expose live phase and scope progress, and shall distinguish exact-match no-plan, partial, canceled, validation, and failure outcomes.
- [ ] FUNC-303z: Plan review shall default to ready scopes, allow all scopes to be shown, and expose target, route profile, planned action, comparison state, execution state, reason, and matched/create/delete counts.
- [ ] FUNC-303z0: Plan scope review shall display saved local and remote row counts and durations at planning time, the projected destination after apply, and saved desired entries when present. Failed or missing remote inspection shall be shown as unavailable; review shall not infer per-row matches or deletions from aggregate counts or contact remote adapters.
- [ ] FUNC-303za: TUI plan apply shall be exposed only while an unapplied `ready` scope exists and shall execute the complete eligible saved plan through the shared reconcile service only after confirmation names its direction, eligible scope count, create count, delete count, and whether local or remote worklogs may change.
- [ ] FUNC-303zb: TUI plan retry shall expose failed and uncertain retries as separate confirmed actions only while matching `ready` scopes exist, and uncertain retry copy shall explain that an earlier remote mutation may already have succeeded.
- [ ] FUNC-303zc: A running TUI reconcile, apply, or retry operation shall reject duplicate actions, remain cancellable with Esc or Ctrl+C, and refresh Plans, Worklogs, and Trash after completion as applicable.
- [ ] FUNC-303zd: Foreground TUI reconcile, plan apply, and plan retry operations shall be recorded as activity without persisting payloads, descriptions, URLs, configuration values, or credentials.

## Out of Scope
- [ ] FUNC-304: TUI Totals, worklog search across dates or remote services, multi-day automatic placement, custom workday overrides, batch shift, arbitrary filtered batch delete or restore beyond Week-mode selected-day operations, payload apply, recurring preset schedules, and preset import or export shall remain deferred.

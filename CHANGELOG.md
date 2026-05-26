# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `workledger worklogs` issue-prefix filtering for narrower list and apply operations.

### Changed
- Shared weekday window resolution across worklog date-window selectors to keep shortcut behavior consistent.

## [0.1.3] - 2026-05-25

### Added
- Weekday shortcut flags for worklog date-window selection.
- Worklog snap support for date-window and workday inputs.
- Configurable workday start and end times for worklog flows.

### Changed
- Worklog query output now uses a `WINDOW` header and clearer time formatting.
- Worklog query results are sorted in ascending `started_at` order.
- Session worklog creation contract and skill guidance were tightened for clarity and precision.

### Fixed
- Default daily lunch time now uses `12:00-12:45`.

## [0.1.2] - 2026-05-22

### Fixed
- Jira issue-prefix matching now requires a trailing dash to avoid ambiguous routing.

## [0.1.1] - 2026-05-22

### Added
- Shared local SQLite storage preflight for write-capable CLI commands.
- `workledger doctor` local storage diagnostics with coverage for writable and unwritable storage cases.
- `workledger worklogs add --dry` preview mode for validating and rendering one would-be local worklog without writing it.
- Accepted date formats and examples in CLI help output.
- Onboarding skill and setup-command guidance for first-time configuration.
- Session worklog creator skill for generating local worklogs from session context.

### Changed
- Local write failures now return friendly `local_storage_not_writable` errors with `sqlite_path`, `parent_dir`, and `operation`.
- Read-only command paths continue to skip storage preflight, and `worklogs apply --dry` remains read-only.
- Specs and agent guidance now treat `workledger doctor` as the canonical local storage diagnostic.
- Clockify reconcile applies now pre-resolve projects and tags once before entry creation.

### Fixed
- Clockify entry creation now uses jittered goroutines to reduce burst contention during apply operations.

## [0.1.0] - 2026-05-15

### Added
- Initial MVP CLI implementation for local worklog capture, planning, status, and reconcile workflows.
- Homebrew-oriented release bootstrapping and packaging setup.

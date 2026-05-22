# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Shared local SQLite storage preflight for write-capable CLI commands.
- `workledger doctor` local storage diagnostics with coverage for writable and unwritable storage cases.

### Changed
- Local write failures now return friendly `local_storage_not_writable` errors with `sqlite_path`, `parent_dir`, and `operation`.
- Read-only command paths continue to skip storage preflight, and `worklogs apply --dry` remains read-only.
- Specs and agent guidance now treat `workledger doctor` as the canonical local storage diagnostic.

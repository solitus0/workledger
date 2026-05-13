#!/usr/bin/env python3
"""Shared helpers for grill-me Codex hooks."""

from __future__ import annotations

import os
import pathlib
import subprocess
from typing import Any, Mapping


def repo_root_from_payload(payload: Mapping[str, Any]) -> pathlib.Path:
    cwd = pathlib.Path(str(payload.get("cwd") or os.getcwd())).expanduser().resolve()
    try:
        root = subprocess.check_output(
            ["git", "rev-parse", "--show-toplevel"],
            cwd=str(cwd),
            text=True,
            stderr=subprocess.DEVNULL,
        ).strip()
        if root:
            return pathlib.Path(root).resolve()
    except Exception:
        pass
    return cwd


def grill_output_dir(payload: Mapping[str, Any]) -> pathlib.Path:
    root = repo_root_from_payload(payload)
    override = os.environ.get("GRILL_OUTPUT_DIR")
    if override:
        path = pathlib.Path(override).expanduser()
        if not path.is_absolute():
            path = root / path
    elif os.environ.get("GRILL_TEAM_VISIBLE") == "1":
        path = root / "docs" / "decisions" / "grill"
    else:
        path = root / ".grill"
    path.mkdir(parents=True, exist_ok=True)
    return path


def ensure_session_files(out_dir: pathlib.Path) -> None:
    files = {
        "current-session.md": "# Grill Session\n\n## Plan\n\n## Confirmed decisions\n\n## Rejected alternatives\n\n## Assumptions\n\n## Open questions\n\n## Codebase findings\n\n## Next branch to resolve\n\n",
        "open-questions.md": "# Open Questions\n\n",
        "codebase-findings.md": "# Codebase Findings\n\n",
    }
    for name, initial in files.items():
        target = out_dir / name
        if not target.exists():
            target.write_text(initial, encoding="utf-8")
    ledger = out_dir / "decisions.jsonl"
    if not ledger.exists():
        ledger.write_text("", encoding="utf-8")

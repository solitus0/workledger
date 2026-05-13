#!/usr/bin/env python3
from __future__ import annotations

import json
import sys

from grill_common import ensure_session_files, grill_output_dir


def main() -> int:
    payload = json.load(sys.stdin)
    out_dir = grill_output_dir(payload)
    ensure_session_files(out_dir)
    session_file = out_dir / "current-session.md"
    text = session_file.read_text(encoding="utf-8") if session_file.exists() else ""
    if text.strip():
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": (
                    "Continue the grill-me session using this saved project-root state. "
                    "Do not write session artifacts into the skill directory.\n\n" + text
                ),
            }
        }))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

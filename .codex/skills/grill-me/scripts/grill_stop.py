#!/usr/bin/env python3
from __future__ import annotations

import json
import sys
from datetime import datetime, timezone

from grill_common import ensure_session_files, grill_output_dir


def main() -> int:
    payload = json.load(sys.stdin)
    out_dir = grill_output_dir(payload)
    ensure_session_files(out_dir)
    with (out_dir / "session-events.jsonl").open("a", encoding="utf-8") as handle:
        handle.write(json.dumps({
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "event": "Stop",
            "session_id": payload.get("session_id"),
            "turn_id": payload.get("turn_id"),
            "output_dir": str(out_dir),
        }, ensure_ascii=False) + "\n")
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "Stop",
            "additionalContext": (
                f"Grill session artifacts are stored in {out_dir}. "
                "Keep current-session.md, decisions.jsonl, open-questions.md, "
                "and codebase-findings.md updated when decisions are made."
            ),
        }
    }))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

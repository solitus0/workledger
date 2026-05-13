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
    entry = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "event": "PostToolUse",
        "session_id": payload.get("session_id"),
        "turn_id": payload.get("turn_id"),
        "tool_name": payload.get("tool_name"),
        "tool_input": payload.get("tool_input"),
    }
    with (out_dir / "tool-events.jsonl").open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(entry, ensure_ascii=False, default=str) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

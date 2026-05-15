#!/usr/bin/env python3
"""Preflight a workledger worklogs apply payload.

This script catches obvious drafting mistakes before the authoritative CLI dry-run:

    workledger worklogs apply --stdin --dry --output json

It intentionally does not attempt duplicate, overlap, timezone, or SQLite validation.
"""

from __future__ import annotations

import argparse
from datetime import datetime
import json
import re
import sys
from typing import Any

ISSUE_KEY_RE = re.compile(r"^[A-Z][A-Z0-9]*-[1-9][0-9]*$")
LOCAL_STARTED_RE = re.compile(
    r"^(?:\d{4}-\d{2}-\d{2}|today|yesterday|tomorrow|[+-][0-9]+d)T[0-2][0-9]:[0-5][0-9]$"
)


def load_payload(path: str) -> Any:
    if path == "-":
        return json.load(sys.stdin)

    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def is_utc_rfc3339(value: str) -> bool:
    candidate = value.strip()
    if candidate.endswith("Z"):
        candidate = candidate[:-1] + "+00:00"

    try:
        parsed = datetime.fromisoformat(candidate)
    except ValueError:
        return False

    if parsed.tzinfo is None:
        return False

    offset = parsed.utcoffset()
    return offset is not None and offset.total_seconds() == 0


def validate_payload(payload: Any, minimum_duration_seconds: int | None = None) -> tuple[bool, list[str], dict[str, int]]:
    errors: list[str] = []

    if not isinstance(payload, dict):
        return False, ["payload must be one JSON object"], {"add_count": 0, "total_duration_seconds": 0}

    adds = payload.get("adds")
    if not isinstance(adds, list):
        return False, ["payload must contain top-level adds array"], {"add_count": 0, "total_duration_seconds": 0}

    if len(adds) == 0:
        errors.append("adds must contain at least one item")

    total_duration_seconds = 0
    for index, item in enumerate(adds):
        prefix = f"adds[{index}]"
        if not isinstance(item, dict):
            errors.append(f"{prefix} must be an object")
            continue

        issue_key = item.get("issue_key")
        if not isinstance(issue_key, str) or not issue_key.strip():
            errors.append(f"{prefix}.issue_key is required")
        elif not ISSUE_KEY_RE.match(issue_key):
            errors.append(f"{prefix}.issue_key must match PROJECT-123 grammar")

        has_started_at = "started_at" in item
        has_started_at_utc = "started_at_utc" in item
        if has_started_at == has_started_at_utc:
            errors.append(f"{prefix} must include exactly one of started_at or started_at_utc")
        elif has_started_at:
            started_at = item.get("started_at")
            if not isinstance(started_at, str) or not LOCAL_STARTED_RE.match(started_at):
                errors.append(f"{prefix}.started_at must use workledger local timestamp grammar")
        else:
            started_at_utc = item.get("started_at_utc")
            if not isinstance(started_at_utc, str) or not is_utc_rfc3339(started_at_utc):
                errors.append(f"{prefix}.started_at_utc must be explicit UTC RFC3339")

        duration = item.get("duration_seconds")
        if isinstance(duration, bool) or not isinstance(duration, int):
            errors.append(f"{prefix}.duration_seconds must be a whole number of seconds")
        elif duration <= 0:
            errors.append(f"{prefix}.duration_seconds must be positive")
        else:
            total_duration_seconds += duration
            if minimum_duration_seconds is not None and duration < minimum_duration_seconds:
                errors.append(
                    f"{prefix}.duration_seconds {duration} is below minimum {minimum_duration_seconds}"
                )

        description = item.get("description")
        if not isinstance(description, str) or not description.strip():
            errors.append(f"{prefix}.description is required")

    summary = {
        "add_count": len(adds),
        "total_duration_seconds": total_duration_seconds,
    }
    return len(errors) == 0, errors, summary


def main() -> int:
    parser = argparse.ArgumentParser(description="Preflight a workledger worklogs apply payload.")
    parser.add_argument("payload", help="Path to payload JSON, or - for stdin.")
    parser.add_argument(
        "--minimum-duration-seconds",
        type=int,
        default=None,
        help="Optional configured minimum local worklog duration.",
    )
    args = parser.parse_args()

    if args.minimum_duration_seconds is not None and args.minimum_duration_seconds <= 0:
        print("ERROR: --minimum-duration-seconds must be positive", file=sys.stderr)
        return 2

    try:
        payload = load_payload(args.payload)
    except (OSError, json.JSONDecodeError) as exc:
        print(json.dumps({"ok": False, "errors": [str(exc)], "summary": {"add_count": 0, "total_duration_seconds": 0}}, indent=2))
        return 1

    ok, errors, summary = validate_payload(payload, args.minimum_duration_seconds)
    print(json.dumps({"ok": ok, "errors": errors, "summary": summary}, indent=2))
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())

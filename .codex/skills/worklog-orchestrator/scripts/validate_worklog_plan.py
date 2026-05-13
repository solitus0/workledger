#!/usr/bin/env python3
"""Validate drafted local worklog candidates against retained heuristics."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Dict, List


@dataclass
class ValidationResult:
    ok: bool
    errors: List[str]


def validate_plan(payload: Dict) -> ValidationResult:
    errors: List[str] = []
    total_minutes = payload.get("total_minutes")
    entries = payload.get("entries", [])
    fixed = payload.get("fixed", {})
    minimum = payload.get("minimum", {})
    maximum = payload.get("maximum", {})

    summed = sum(int(entry.get("duration_minutes", 0)) for entry in entries)
    if total_minutes is not None and summed != int(total_minutes):
        errors.append(f"Entry total {summed} does not match requested total {total_minutes}")

    per_issue: Dict[str, int] = {}
    for entry in entries:
        issue = entry.get("issue") or entry.get("tag")
        duration = int(entry.get("duration_minutes", 0))
        if duration <= 0:
            errors.append(f"Entry has non-positive duration: {entry}")
        if issue:
            per_issue[issue] = per_issue.get(issue, 0) + duration

    for issue, duration in fixed.items():
        if per_issue.get(issue) != int(duration):
            errors.append(f"Issue {issue} total {per_issue.get(issue, 0)} != fixed {duration}")
    for issue, duration in minimum.items():
        if per_issue.get(issue, 0) < int(duration):
            errors.append(f"Issue {issue} total {per_issue.get(issue, 0)} < minimum {duration}")
    for issue, duration in maximum.items():
        if per_issue.get(issue, 0) > int(duration):
            errors.append(f"Issue {issue} total {per_issue.get(issue, 0)} > maximum {duration}")

    starts = [entry.get("start") for entry in entries if entry.get("start")]
    if starts and starts != sorted(starts):
        errors.append("Entries are not in chronological order")

    return ValidationResult(ok=not errors, errors=errors)


def main() -> None:
    import sys
    if len(sys.argv) != 2:
        raise SystemExit("Usage: validate_worklog_plan.py <plan.json>")
    with open(sys.argv[1], "r", encoding="utf-8") as handle:
        payload = json.load(handle)
    result = validate_plan(payload)
    print(json.dumps({"ok": result.ok, "errors": result.errors}, indent=2))
    raise SystemExit(0 if result.ok else 1)


if __name__ == "__main__":
    main()

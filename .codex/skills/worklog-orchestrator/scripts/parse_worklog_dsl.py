#!/usr/bin/env python3
"""Legacy helper: parse deprecated worklog orchestration DSL into normalized JSON."""

from __future__ import annotations

import argparse
import json
import re
from dataclasses import dataclass, asdict, field
from typing import Dict, List, Optional

MODE_FLAGS = {
    "--extract-session": "extract_session",
    "-x": "extract_session",
    "--inspect": "inspect",
    "-i": "inspect",
    "--prepare": "prepare",
    "--sync": "sync",
    "-s": "sync",
}

DURATION_RE = re.compile(r"^(?:(?P<hours>\d+)h)?(?:(?P<minutes>\d+)m)?$")
CONSTRAINT_RE = re.compile(r"^(?P<issue>[A-Z][A-Z0-9]+-\d+):(?P<value>.+)$")


@dataclass
class Parsed:
    mode: str = "prepare"
    from_value: Optional[str] = None
    to_value: Optional[str] = None
    project: Optional[str] = None
    workspace: Optional[str] = None
    jira: List[str] = field(default_factory=list)
    total_minutes: Optional[int] = None
    count: Optional[int] = None
    day_start: Optional[str] = None
    day_end: Optional[str] = None
    lunch: Optional[str] = None
    estimate_mode: str = "auto"
    budget: str = "auto"
    source: str = "auto"
    artifact_dir: Optional[str] = None
    fixed: Dict[str, int] = field(default_factory=dict)
    minimum: Dict[str, int] = field(default_factory=dict)
    maximum: Dict[str, int] = field(default_factory=dict)
    issue_count: Dict[str, int] = field(default_factory=dict)
    issue_max_count: Dict[str, int] = field(default_factory=dict)


def parse_duration(value: str) -> int:
    if value.startswith("PT"):
        hours = re.search(r"(\d+)H", value)
        minutes = re.search(r"(\d+)M", value)
        total = 0
        if hours:
            total += int(hours.group(1)) * 60
        if minutes:
            total += int(minutes.group(1))
        if total:
            return total
        raise ValueError(f"Invalid ISO-8601 duration: {value}")
    match = DURATION_RE.match(value)
    if not match:
        raise ValueError(f"Invalid duration: {value}")
    hours = int(match.group("hours") or 0)
    minutes = int(match.group("minutes") or 0)
    total = hours * 60 + minutes
    if total <= 0:
        raise ValueError(f"Duration must be positive: {value}")
    return total


def parse_constraint(values: List[str], integer: bool = False) -> Dict[str, int]:
    result: Dict[str, int] = {}
    for raw in values:
        match = CONSTRAINT_RE.match(raw)
        if not match:
            raise ValueError(f"Invalid constraint: {raw}")
        issue = match.group("issue")
        parsed = int(match.group("value")) if integer else parse_duration(match.group("value"))
        result[issue] = parsed
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("mode", nargs="?")
    parser.add_argument("--extract-session", "-x", action="store_true")
    parser.add_argument("--inspect", "-i", action="store_true")
    parser.add_argument("--prepare", action="store_true")
    parser.add_argument("--sync", "-s", action="store_true")
    parser.add_argument("--from", "-f", dest="from_value")
    parser.add_argument("--to", "-t", dest="to_value")
    parser.add_argument("--project", "-p")
    parser.add_argument("--workspace", "-w")
    parser.add_argument("--jira", "-j")
    parser.add_argument("--total")
    parser.add_argument("--count", "-c", type=int)
    parser.add_argument("--day-start")
    parser.add_argument("--day-end")
    parser.add_argument("--lunch")
    parser.add_argument("--estimate-mode", default="auto")
    parser.add_argument("--budget", default="auto")
    parser.add_argument("--source", default="auto")
    parser.add_argument("--artifact-dir")
    parser.add_argument("--fixed", action="append", default=[])
    parser.add_argument("--min", dest="minimum", action="append", default=[])
    parser.add_argument("--max", dest="maximum", action="append", default=[])
    parser.add_argument("--issue-count", action="append", default=[])
    parser.add_argument("--issue-max-count", action="append", default=[])
    return parser


def parse_invocation(invocation: str) -> Parsed:
    tokens = invocation.split()
    parser = build_parser()
    args = parser.parse_args(tokens)
    parsed = Parsed()
    if args.extract_session:
        parsed.mode = "extract_session"
    elif args.inspect:
        parsed.mode = "inspect"
    elif args.sync:
        parsed.mode = "sync"
    elif args.prepare:
        parsed.mode = "prepare"
    parsed.from_value = args.from_value
    parsed.to_value = args.to_value
    parsed.project = args.project
    parsed.workspace = args.workspace
    if args.jira:
        seen = set()
        jira_list: List[str] = []
        for key in args.jira.split(","):
            key = key.strip()
            if key and key not in seen:
                seen.add(key)
                jira_list.append(key)
        parsed.jira = jira_list
    if args.total:
        parsed.total_minutes = parse_duration(args.total)
    parsed.count = args.count
    parsed.day_start = args.day_start
    parsed.day_end = args.day_end
    parsed.lunch = args.lunch
    parsed.estimate_mode = args.estimate_mode
    parsed.budget = args.budget
    parsed.source = args.source
    parsed.artifact_dir = args.artifact_dir
    parsed.fixed = parse_constraint(args.fixed)
    parsed.minimum = parse_constraint(args.minimum)
    parsed.maximum = parse_constraint(args.maximum)
    parsed.issue_count = parse_constraint(args.issue_count, integer=True)
    parsed.issue_max_count = parse_constraint(args.issue_max_count, integer=True)
    for issue in list(parsed.fixed) + list(parsed.minimum) + list(parsed.maximum) + list(parsed.issue_count) + list(parsed.issue_max_count):
        if issue not in parsed.jira:
            parsed.jira.append(issue)
    return parsed


def main() -> None:
    import sys
    if len(sys.argv) < 2:
        raise SystemExit("Usage: parse_worklog_dsl.py '<invocation>'")
    parsed = parse_invocation(sys.argv[1])
    print(json.dumps(asdict(parsed), indent=2, sort_keys=True))


if __name__ == "__main__":
    main()

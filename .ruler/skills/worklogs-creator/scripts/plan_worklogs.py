#!/usr/bin/env python3
"""Plan exact Workledger additions inside context-reported quota-safe free slots."""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from dataclasses import dataclass
from datetime import date, datetime, timedelta, timezone
from pathlib import Path
from typing import Any, NoReturn


SECONDS_PER_MINUTE = 60


def fail(message: str) -> NoReturn:
    print(f"error: {message}", file=sys.stderr)
    raise SystemExit(2)


def load_json(path: Path, label: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        fail(f"cannot read {label}: {exc}")
    if not isinstance(value, dict):
        fail(f"{label} must contain a JSON object")
    return value


def parse_day(value: str, label: str) -> date:
    try:
        return date.fromisoformat(value)
    except ValueError:
        fail(f"{label} must be YYYY-MM-DD: {value}")


def parse_slot_time(value: str) -> datetime:
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00" if value.endswith("Z") else value)
    except ValueError:
        fail(f"invalid context timestamp: {value}")
    if parsed.utcoffset() is None:
        fail(f"context timestamp requires a UTC offset: {value}")
    return parsed


@dataclass
class Candidate:
    index: int
    candidate_id: str
    message: str
    issue_key: str
    remaining: int
    original: int


@dataclass
class Slot:
    day: date
    cursor: datetime
    remaining: int


def candidate_data(payload: dict[str, Any], minimum: int) -> list[Candidate]:
    raw_candidates = payload.get("candidates")
    if not isinstance(raw_candidates, list) or not raw_candidates:
        fail("candidates JSON must contain a non-empty candidates array")
    seen: set[str] = set()
    candidates: list[Candidate] = []
    for index, raw in enumerate(raw_candidates):
        if not isinstance(raw, dict):
            fail(f"candidate {index} must be an object")
        candidate_id = str(raw.get("id", "")).strip()
        message = str(raw.get("message", "")).strip()
        issue_key = raw.get("issue_key", "")
        duration = raw.get("duration_seconds")
        if not candidate_id or candidate_id in seen:
            fail(f"candidate {index} has a missing or duplicate id")
        if not message:
            fail(f"candidate {candidate_id} has no message")
        if not isinstance(issue_key, str):
            fail(f"candidate {candidate_id} issue_key must be a string")
        if not isinstance(duration, int) or duration < minimum or duration % SECONDS_PER_MINUTE:
            fail(f"candidate {candidate_id} duration must be whole minutes and at least {minimum} seconds")
        seen.add(candidate_id)
        candidates.append(Candidate(index, candidate_id, message, issue_key.strip(), duration, duration))
    return candidates


def ordered_days(
    from_day: date,
    to_day: date,
    priorities: list[date],
    excluded: set[date],
    include_weekends: bool,
) -> list[date]:
    if len(priorities) != len(set(priorities)):
        fail("priority dates must be unique")
    for day in priorities:
        if not (from_day <= day <= to_day):
            fail(f"priority date is outside the placement window: {day}")
        if day in excluded:
            fail(f"date cannot be both priority and excluded: {day}")
        if not include_weekends and day.weekday() >= 5:
            fail(f"weekend priority requires --include-weekends: {day}")
    days: list[date] = []
    cursor = from_day
    while cursor <= to_day:
        if cursor not in excluded and (include_weekends or cursor.weekday() < 5):
            days.append(cursor)
        cursor += timedelta(days=1)
    return priorities + [day for day in days if day not in priorities]


def context_slots(
    context: dict[str, Any], days: list[date], minimum: int,
) -> tuple[list[Slot], dict[date, int], int]:
    by_day: dict[date, dict[str, Any]] = {}
    raw_days = context.get("days")
    if not isinstance(raw_days, list):
        fail("context JSON has no days array")
    for raw_day in raw_days:
        if not isinstance(raw_day, dict) or "date" not in raw_day:
            continue
        by_day[parse_day(str(raw_day["date"]), "context day")] = raw_day

    slots: list[Slot] = []
    quotas: dict[date, int] = {}
    total_capacity = 0
    for day in days:
        raw_day = by_day.get(day)
        if raw_day is None:
            fail(f"context does not include eligible date: {day}")
        quota = raw_day.get("until_quota_seconds", 0)
        if not isinstance(quota, int):
            fail(f"invalid until_quota_seconds for {day}")
        quota = max(0, quota)
        quotas[day] = quota - quota % SECONDS_PER_MINUTE
        raw_slots = raw_day.get("free_slots", [])
        if not isinstance(raw_slots, list):
            fail(f"invalid free_slots for {day}")
        parsed_slots: list[tuple[datetime, datetime]] = []
        for raw_slot in raw_slots:
            if not isinstance(raw_slot, dict):
                continue
            start = parse_slot_time(str(raw_slot.get("start", "")))
            end = parse_slot_time(str(raw_slot.get("end", "")))
            if start.date() != day or end <= start:
                fail(f"invalid free slot on {day}")
            parsed_slots.append((start.astimezone(timezone.utc), end.astimezone(timezone.utc)))
        if quotas[day] < minimum:
            continue
        slot_capacity = 0
        for start, end in sorted(parsed_slots):
            duration = int((end - start).total_seconds())
            duration -= duration % SECONDS_PER_MINUTE
            if duration >= minimum:
                slots.append(Slot(day, start, duration))
                slot_capacity += duration
        total_capacity += min(slot_capacity, quotas[day])
    return slots, quotas, total_capacity


def choose_candidate(
    candidates: list[Candidate],
    capacity: int,
    minimum: int,
) -> tuple[Candidate, int] | None:
    unfinished = [candidate for candidate in candidates if candidate.remaining]
    fitting = [candidate for candidate in unfinished if candidate.remaining <= capacity]
    if fitting:
        chosen = max(fitting, key=lambda candidate: (candidate.remaining, -candidate.index))
        return chosen, chosen.remaining
    if capacity < minimum or not unfinished:
        return None
    chosen = max(unfinished, key=lambda candidate: (candidate.remaining, -candidate.index))
    chunk = min(capacity, chosen.remaining - minimum)
    if chunk < minimum:
        return None
    return chosen, chunk


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", required=True, type=Path)
    parser.add_argument("--candidates", required=True, type=Path)
    parser.add_argument("--from", dest="from_date", required=True)
    parser.add_argument("--to", dest="to_date", required=True)
    parser.add_argument("--priority", action="append", default=[])
    parser.add_argument("--exclude", action="append", default=[])
    parser.add_argument("--include-weekends", action="store_true")
    args = parser.parse_args()

    context = load_json(args.context, "context JSON")
    candidate_payload = load_json(args.candidates, "candidates JSON")
    planning = context.get("planning", {})
    if not isinstance(planning, dict):
        fail("context planning must be an object")
    minimum = planning.get("minimum_duration_seconds", 900)
    if not isinstance(minimum, int) or minimum <= 0 or minimum % SECONDS_PER_MINUTE:
        fail("context minimum_duration_seconds must be positive whole minutes")
    candidates = candidate_data(candidate_payload, minimum)
    from_day = parse_day(args.from_date, "--from")
    to_day = parse_day(args.to_date, "--to")
    if from_day > to_day:
        fail("--from must not be after --to")
    priorities = [parse_day(value, "--priority") for value in args.priority]
    excluded = {parse_day(value, "--exclude") for value in args.exclude}
    days = ordered_days(from_day, to_day, priorities, excluded, args.include_weekends)
    slots, quotas, capacity = context_slots(context, days, minimum)
    required = sum(candidate.original for candidate in candidates)
    if capacity < required:
        fail(
            f"insufficient eligible capacity: required={required} "
            f"available={capacity} deficit={required - capacity}"
        )

    records: list[dict[str, Any]] = []
    fragments: Counter[str] = Counter()
    daily: Counter[str] = Counter()
    for slot in slots:
        day = slot.day.isoformat()
        while (available := min(slot.remaining, quotas[slot.day] - daily[day])) >= minimum:
            selection = choose_candidate(candidates, available, minimum)
            if selection is None:
                break
            candidate, chunk = selection
            record = {
                "candidate_id": candidate.candidate_id,
                "message": candidate.message,
                "started_at_utc": slot.cursor.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "duration_seconds": chunk,
            }
            if candidate.issue_key:
                record["issue_key"] = candidate.issue_key
            records.append(record)
            fragments[candidate.candidate_id] += 1
            daily[day] += chunk
            candidate.remaining -= chunk
            slot.remaining -= chunk
            slot.cursor += timedelta(seconds=chunk)

    unfinished = {
        candidate.candidate_id: candidate.remaining
        for candidate in candidates
        if candidate.remaining
    }
    if unfinished:
        fail(f"planner could not place all candidate duration: {unfinished}")

    adds = []
    for record in records:
        add = {
            "started_at_utc": record["started_at_utc"],
            "duration_seconds": record["duration_seconds"],
            "description": record["message"],
        }
        if "issue_key" in record:
            add["issue_key"] = record["issue_key"]
        adds.append(add)
    payload = {
        "schema_version": 1,
        "summary": {
            "logical_candidates": len(candidates),
            "physical_records": len(records),
            "total_duration_seconds": required,
            "eligible_capacity_seconds": capacity,
            "unused_capacity_seconds": capacity - required,
            "minimum_duration_seconds": minimum,
        },
        "daily_totals": [
            {"date": day, "duration_seconds": duration}
            for day, duration in sorted(daily.items())
        ],
        "splits": [
            {
                "candidate_id": candidate.candidate_id,
                "fragments": fragments[candidate.candidate_id],
            }
            for candidate in candidates
            if fragments[candidate.candidate_id] > 1
        ],
        "records": records,
        "apply_payload": {"adds": adds},
    }
    json.dump(payload, sys.stdout, indent=2, sort_keys=True)
    print()


if __name__ == "__main__":
    main()

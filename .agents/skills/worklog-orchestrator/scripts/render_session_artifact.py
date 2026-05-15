#!/usr/bin/env python3
"""Create or update coding-session artifacts for later worklog drafting."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
from pathlib import Path
import re
import sys
import tempfile

BASE_DIR = Path(".local") / "codex-task-extractor"
MANAGED_FRONTMATTER_KEYS = {"session_id", "created_at", "updated_at"}
ISSUE_KEY_RE = re.compile(r"^[A-Z][A-Z0-9]*-[1-9][0-9]*$")


def sanitize_session_id(session_id: str) -> str:
    value = session_id.strip()
    if not value:
        raise ValueError("session_id must not be empty")

    safe = re.sub(r"[^A-Za-z0-9._-]+", "-", value).strip("-._")
    if not safe:
        safe = hashlib.sha1(value.encode("utf-8")).hexdigest()[:16]
    return safe[:120]


def date_dir_name(now: datetime | None = None) -> str:
    value = now or datetime.now(timezone.utc)
    return value.strftime("%m-%d")


def now_utc_iso() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def split_frontmatter_block(content: str) -> tuple[str | None, str]:
    if not content.startswith("---\n"):
        return None, content

    marker_index = content.find("\n---\n", 4)
    if marker_index == -1:
        return None, content

    return content[4:marker_index], content[marker_index + 5 :]


def parse_frontmatter_blocks(frontmatter_block: str | None) -> list[tuple[str, list[str]]]:
    if not frontmatter_block:
        return []

    blocks: list[tuple[str, list[str]]] = []
    current_key: str | None = None
    current_lines: list[str] = []

    for line in frontmatter_block.splitlines():
        if line and not line[0].isspace() and ":" in line:
            if current_key is not None:
                blocks.append((current_key, current_lines))
            current_key = line.partition(":")[0].strip()
            current_lines = [line]
            continue

        if current_key is not None:
            current_lines.append(line)

    if current_key is not None:
        blocks.append((current_key, current_lines))

    return blocks


def parse_frontmatter_map(frontmatter_block: str | None) -> dict[str, str]:
    values: dict[str, str] = {}
    if not frontmatter_block:
        return values

    for key, lines in parse_frontmatter_blocks(frontmatter_block):
        if len(lines) == 1:
            values[key] = lines[0].partition(":")[2].strip()
    return values


def extract_jira_keys(frontmatter_block: str | None) -> list[str]:
    keys: list[str] = []
    for key, lines in parse_frontmatter_blocks(frontmatter_block):
        if key != "jira_keys":
            continue
        for line in lines[1:]:
            stripped = line.strip()
            if not stripped.startswith("-"):
                continue
            candidate = stripped[1:].strip()
            if ISSUE_KEY_RE.match(candidate) and candidate not in keys:
                keys.append(candidate)
    return keys


def normalize_jira_keys(values: list[str]) -> list[str]:
    keys: list[str] = []
    for value in values:
        key = value.strip()
        if not ISSUE_KEY_RE.match(key):
            raise ValueError(f"invalid jira key: {value}")
        if key not in keys:
            keys.append(key)
    return keys


def merge_extra_frontmatter(
    existing_block: str | None,
    source_block: str | None,
    jira_keys: list[str],
) -> list[str]:
    existing_blocks = parse_frontmatter_blocks(existing_block)
    source_blocks = parse_frontmatter_blocks(source_block)

    skip_keys = MANAGED_FRONTMATTER_KEYS | {"jira_keys"}
    existing_map = {key: lines for key, lines in existing_blocks if key not in skip_keys}
    source_map = {key: lines for key, lines in source_blocks if key not in skip_keys}

    ordered_keys: list[str] = []
    for key, _lines in source_blocks + existing_blocks:
        if key not in skip_keys and key not in ordered_keys:
            ordered_keys.append(key)

    merged_lines: list[str] = []
    if jira_keys:
        merged_lines.append("jira_keys:")
        merged_lines.extend(f"  - {key}" for key in jira_keys)

    for key in ordered_keys:
        lines = source_map.get(key, existing_map.get(key))
        if lines:
            merged_lines.extend(lines)

    return merged_lines


def build_content(
    session_id: str,
    created_at: str,
    updated_at: str,
    body: str,
    extra_frontmatter_lines: list[str],
) -> str:
    header_lines = [
        "---",
        f"session_id: {session_id}",
        f"created_at: {created_at}",
        f"updated_at: {updated_at}",
    ]
    header_lines.extend(extra_frontmatter_lines)
    header_lines.extend(["---", ""])
    normalized_body = body.lstrip("\n")
    return "\n".join(header_lines) + "\n" + normalized_body


def target_paths(session_id: str, jira_keys: list[str]) -> list[Path]:
    safe_id = sanitize_session_id(session_id)
    day = date_dir_name()
    issue_dirs = jira_keys if jira_keys else ["_unlinked"]
    return [BASE_DIR / day / issue_key / f"{safe_id}.md" for issue_key in issue_dirs]


def existing_created_at(path: Path) -> tuple[str | None, str | None]:
    if not path.exists():
        return None, None
    content = path.read_text(encoding="utf-8")
    block, _body = split_frontmatter_block(content)
    values = parse_frontmatter_map(block)
    created_at = values.get("created_at", "").strip() or None
    return created_at, block


def write_atomic(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        dir=str(path.parent),
        prefix=f"{path.name}.",
        suffix=".tmp",
        delete=False,
    ) as tmp:
        tmp.write(content)
        tmp_path = Path(tmp.name)

    tmp_path.replace(path)


def main() -> int:
    parser = argparse.ArgumentParser(description="Create or update a coding-session worklog artifact.")
    parser.add_argument("--session-id", required=True, help="Stable session identifier.")
    parser.add_argument("--content-file", required=True, help="Markdown content to persist.")
    parser.add_argument("--jira-key", action="append", default=[], help="Linked issue key. May be repeated.")
    args = parser.parse_args()

    source = Path(args.content_file)
    if not source.exists():
        print(f"ERROR: content file not found: {source}", file=sys.stderr)
        return 1

    try:
        source_content = source.read_text(encoding="utf-8")
        source_block, body = split_frontmatter_block(source_content)
        supplied_keys = normalize_jira_keys(args.jira_key)
        artifact_keys = supplied_keys or extract_jira_keys(source_block)
        paths = target_paths(args.session_id, artifact_keys)
    except ValueError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2

    BASE_DIR.mkdir(parents=True, exist_ok=True)
    updated_at = now_utc_iso()
    statuses: list[str] = []

    for path in paths:
        created_at, existing_block = existing_created_at(path)
        existed = path.exists()
        extra_frontmatter_lines = merge_extra_frontmatter(existing_block, source_block, artifact_keys)
        content = build_content(
            session_id=args.session_id,
            created_at=created_at or updated_at,
            updated_at=updated_at,
            body=body,
            extra_frontmatter_lines=extra_frontmatter_lines,
        )
        write_atomic(path, content)
        statuses.append("updated" if existed else "created")

    if len(set(statuses)) == 1:
        print(f"status={statuses[0]}")
    else:
        print("status=mixed")
    for path in paths:
        print(f"path={path.as_posix()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

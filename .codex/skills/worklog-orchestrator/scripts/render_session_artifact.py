#!/usr/bin/env python3
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
from pathlib import Path
import re
import sys
import tempfile

BASE_DIR = Path('.local') / 'codex-task-extractor'
MANAGED_FRONTMATTER_KEYS = {'session_id', 'created_at', 'updated_at'}


def sanitize_session_id(session_id: str) -> str:
    session_id = session_id.strip()
    if not session_id:
        raise ValueError('session_id must not be empty')
    safe = re.sub(r'[^A-Za-z0-9._-]+', '-', session_id).strip('-._')
    if not safe:
        safe = hashlib.sha1(session_id.encode('utf-8')).hexdigest()[:16]
    return safe[:120]


def date_dir_name(now: datetime | None = None) -> str:
    value = now or datetime.now(timezone.utc)
    return value.strftime('%m-%d')


def resolve_path(session_id: str) -> Path:
    safe_id = sanitize_session_id(session_id)
    existing = find_existing_path(safe_id)
    if existing is not None:
        return existing

    return BASE_DIR / date_dir_name() / f'{safe_id}.md'


def find_existing_path(safe_id: str) -> Path | None:
    matches = sorted(BASE_DIR.glob(f'*/{safe_id}.md'))
    if matches:
        return matches[0]

    legacy_path = BASE_DIR / f'{safe_id}.md'
    if legacy_path.exists():
        return legacy_path

    return None


def now_utc_iso() -> str:
    return datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')


def split_frontmatter(content: str) -> tuple[dict[str, str], str]:
    if not content.startswith('---\n'):
        return {}, content

    marker_index = content.find('\n---\n', 4)
    if -1 == marker_index:
        return {}, content

    frontmatter_block = content[4:marker_index]
    body = content[marker_index + 5 :]

    metadata: dict[str, str] = {}
    for line in frontmatter_block.splitlines():
        key, separator, value = line.partition(':')
        if '' == separator:
            continue

        normalized_key = key.strip()
        if '' == normalized_key:
            continue

        metadata[normalized_key] = value.strip()

    return metadata, body


def split_frontmatter_block(content: str) -> tuple[str | None, str]:
    if not content.startswith('---\n'):
        return None, content

    marker_index = content.find('\n---\n', 4)
    if -1 == marker_index:
        return None, content

    return content[4:marker_index], content[marker_index + 5 :]


def parse_frontmatter_blocks(frontmatter_block: str | None) -> list[tuple[str, list[str]]]:
    if not frontmatter_block:
        return []

    blocks: list[tuple[str, list[str]]] = []
    current_key: str | None = None
    current_lines: list[str] = []

    for line in frontmatter_block.splitlines():
        if line and not line[0].isspace() and ':' in line:
            if current_key is not None:
                blocks.append((current_key, current_lines))

            current_key = line.partition(':')[0].strip()
            current_lines = [line]
            continue

        if current_key is None:
            continue

        current_lines.append(line)

    if current_key is not None:
        blocks.append((current_key, current_lines))

    return blocks


def merge_extra_frontmatter(
    existing_block: str | None,
    source_block: str | None,
) -> list[str]:
    existing_blocks = parse_frontmatter_blocks(existing_block)
    source_blocks = parse_frontmatter_blocks(source_block)

    existing_map = {
        key: lines for key, lines in existing_blocks if key not in MANAGED_FRONTMATTER_KEYS
    }
    source_map = {
        key: lines for key, lines in source_blocks if key not in MANAGED_FRONTMATTER_KEYS
    }

    ordered_keys: list[str] = []
    for key, _lines in source_blocks:
        if key not in MANAGED_FRONTMATTER_KEYS and key not in ordered_keys:
            ordered_keys.append(key)

    for key, _lines in existing_blocks:
        if key not in MANAGED_FRONTMATTER_KEYS and key not in ordered_keys:
            ordered_keys.append(key)

    merged_lines: list[str] = []
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
    extra_frontmatter_lines: list[str] | None = None,
) -> str:
    normalized_body = body.lstrip('\n')
    header_lines = [
        '---',
        f'session_id: {session_id}',
        f'created_at: {created_at}',
        f'updated_at: {updated_at}',
    ]
    if extra_frontmatter_lines:
        header_lines.extend(extra_frontmatter_lines)
    header_lines.extend(['---', ''])
    header = '\n'.join(header_lines)

    if '' == normalized_body:
        return f'{header}\n'

    return f'{header}\n{normalized_body}'


def write_atomic(path: Path, content: str) -> None:
    with tempfile.NamedTemporaryFile(
        mode='w',
        encoding='utf-8',
        dir=str(path.parent),
        prefix=f'{path.name}.',
        suffix='.tmp',
        delete=False,
    ) as tmp:
        tmp.write(content)
        tmp_path = Path(tmp.name)

    tmp_path.replace(path)


def main() -> int:
    parser = argparse.ArgumentParser(description='Create or update a per-session codex task log.')
    parser.add_argument('--session-id', required=True, help='Stable session identifier.')
    parser.add_argument('--content-file', required=True, help='Path to markdown content to persist.')
    args = parser.parse_args()

    target = resolve_path(args.session_id)
    source = Path(args.content_file)

    if not source.exists():
        print(f'ERROR: content file not found: {source}', file=sys.stderr)
        return 1

    BASE_DIR.mkdir(parents=True, exist_ok=True)
    target.parent.mkdir(parents=True, exist_ok=True)
    existed = target.exists()
    source_content = source.read_text(encoding='utf-8')
    source_frontmatter_block, body = split_frontmatter_block(source_content)

    updated_at = now_utc_iso()
    created_at = updated_at
    existing_frontmatter_block = None
    if existed:
        existing_content = target.read_text(encoding='utf-8')
        existing_frontmatter, _ = split_frontmatter(existing_content)
        existing_frontmatter_block, _ = split_frontmatter_block(existing_content)
        created_at = existing_frontmatter.get('created_at', '').strip() or updated_at

    extra_frontmatter_lines = merge_extra_frontmatter(existing_frontmatter_block, source_frontmatter_block)
    content_to_write = build_content(args.session_id, created_at, updated_at, body, extra_frontmatter_lines)
    write_atomic(target, content_to_write)

    print(f'status={"updated" if existed else "created"}')
    print(f'path={target.as_posix()}')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

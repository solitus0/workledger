#!/usr/bin/env python3
"""Present production ANSI frames in a terminal canvas for lossless PNG export."""
import argparse
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--directory", type=Path, default=ROOT / "docs/assets/screenshots",
                    help="directory containing production ANSI captures")
OUT = parser.parse_args().directory
SGR = re.compile(r"\x1b\[([0-9;]*)m")
PALETTES = {
    "dark": dict(fg="#CDD6F4", bg="#1E1E2E", border="#45475A", canvas="#11111B"),
    "light": dict(fg="#4C4F69", bg="#EFF1F5", border="#BCC0CC", canvas="#E6E9EF"),
}


def terminal_runs(frame, foreground, background):
    state = dict(fg=foreground, bg=background, bold=False, inverse=False)
    result, cursor = [], 0
    for match in SGR.finditer(frame):
        chunk = frame[cursor:match.start()]
        if chunk:
            fg, bg = (state["bg"], state["fg"]) if state["inverse"] else (state["fg"], state["bg"])
            result.append(dict(text=chunk, fg=fg, bg=bg, bold=state["bold"]))
        values = [int(x or 0) for x in match.group(1).split(";")]
        i = 0
        while i < len(values):
            code = values[i]
            if code == 0:
                state = dict(fg=foreground, bg=background, bold=False, inverse=False)
            elif code in (1, 22):
                state["bold"] = code == 1
            elif code in (7, 27):
                state["inverse"] = code == 7
            elif code in (39, 49):
                state["fg" if code == 39 else "bg"] = foreground if code == 39 else background
            elif code in (38, 48) and values[i + 1] == 2:
                state["fg" if code == 38 else "bg"] = "#" + "".join(f"{v:02x}" for v in values[i+2:i+5])
                i += 4
            else:
                raise ValueError(f"Unsupported SGR sequence: {match.group(0)!r}")
            i += 1
        cursor = match.end()
    if frame[cursor:]:
        fg, bg = (state["bg"], state["fg"]) if state["inverse"] else (state["fg"], state["bg"])
        result.append(dict(text=frame[cursor:], fg=fg, bg=bg, bold=state["bold"]))
    return result


SCRIPT = r'''
const {runs, colors, filename, columns, rows} = FRAME;
const fontSize = 16, lineHeight = 22, scale = 2;
const family = 'Menlo, Consolas, monospace';
await document.fonts.load(`${fontSize}px Menlo`);
await document.fonts.ready;
const canvas = document.getElementById('terminal');
const ctx = canvas.getContext('2d');
ctx.font = `${fontSize}px ${family}`;
const cell = ctx.measureText('M').width;
const metrics = ctx.measureText('Mg');
const ascent = metrics.fontBoundingBoxAscent || 13;
const descent = metrics.fontBoundingBoxDescent || 4;
const outer = 16, inset = 16, titleHeight = 34, top = 14, bottom = 16;
const width = Math.ceil(columns * cell + 2 * (outer + inset + 1));
const height = rows * lineHeight + 2 * outer + titleHeight + top + bottom + 2;
canvas.width = width * scale; canvas.height = height * scale;
canvas.style.width = width + 'px'; canvas.style.height = height + 'px';
ctx.scale(scale, scale);
ctx.fillStyle = colors.canvas; ctx.fillRect(0, 0, width, height);
ctx.beginPath(); ctx.roundRect(outer, outer, width - outer * 2, height - outer * 2, 12);
ctx.fillStyle = colors.bg; ctx.fill();
ctx.strokeStyle = colors.border; ctx.lineWidth = 1; ctx.stroke();
ctx.beginPath(); ctx.moveTo(outer, outer + titleHeight); ctx.lineTo(width - outer, outer + titleHeight); ctx.stroke();
for (let i = 0; i < 3; i++) {
  ctx.beginPath(); ctx.arc(outer + 19 + i * 14, outer + titleHeight / 2, 4, 0, Math.PI * 2);
  ctx.fillStyle = colors.border; ctx.fill();
}
ctx.font = '12px -apple-system, BlinkMacSystemFont, sans-serif';
ctx.textAlign = 'center'; ctx.textBaseline = 'middle'; ctx.fillStyle = colors.fg;
ctx.fillText('workledger tui', width / 2, outer + titleHeight / 2);
const cells = []; let column = 0, row = 0;
for (const run of runs) {
  for (const char of run.text) {
    if (char === '\n') { row++; column = 0; continue; }
    cells.push({...run, text: char, column, row}); column++;
  }
}
const x = outer + inset + 1, y = outer + titleHeight + top + 1;
for (const cellData of cells) {
  ctx.fillStyle = cellData.bg;
  ctx.fillRect(x + cellData.column * cell, y + cellData.row * lineHeight, Math.ceil(cell), lineHeight);
}
ctx.textAlign = 'left'; ctx.textBaseline = 'alphabetic';
for (const cellData of cells) {
  ctx.font = `${cellData.bold ? 600 : 400} ${fontSize}px ${family}`;
  ctx.fillStyle = cellData.fg;
  ctx.fillText(cellData.text, x + cellData.column * cell, y + cellData.row * lineHeight + (lineHeight - ascent - descent) / 2 + ascent);
}
const save = document.getElementById('save');
save.href = canvas.toDataURL('image/png');
save.download = filename;
document.getElementById('dimensions').textContent = `${columns} × ${rows} cells · ${canvas.width} × ${canvas.height} PNG`;
canvas.dataset.ready = 'true';
'''

for source in sorted(OUT.glob("*.ansi")):
    theme = source.stem.rsplit("-", 1)[-1]
    colors = PALETTES[theme]
    frame = source.read_text()
    plain = SGR.sub("", frame)
    lines = plain.splitlines()
    payload = dict(runs=terminal_runs(frame, colors["fg"], colors["bg"]), colors=colors,
                   filename=source.with_suffix(".png").name,
                   columns=max(map(len, lines)), rows=len(lines))
    data = json.dumps(payload, ensure_ascii=True).replace("<", "\\u003c")
    page = f'''<!doctype html>
<html lang="en"><meta charset="utf-8"><title>Workledger — {source.stem}</title>
<style>body{{margin:0;width:max-content;background:{colors['canvas']};color:{colors['fg']};font:13px system-ui}}canvas{{display:block}}.controls{{padding:0 16px 16px;display:flex;gap:12px;align-items:center}}a{{font:inherit;padding:6px 12px;color:inherit;border:1px solid currentColor;border-radius:4px;text-decoration:none}}</style>
<canvas id="terminal" role="img" aria-label="Workledger TUI with fictional Atlas data"></canvas>
<div class="controls"><a id="save">Save PNG</a><span id="dimensions">Rendering…</span></div>
<script type="module">const FRAME={data};\n{SCRIPT}</script></html>'''
    source.with_suffix(".html").write_text(page)
    print(source.with_suffix(".html"))

#!/usr/bin/env python3
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
"""Run the offline checker example and render an explicitly labelled replay."""
import argparse
import hashlib
from pathlib import Path
import subprocess
import textwrap

from PIL import Image, ImageDraw, ImageFont

ASSETS = Path(__file__).resolve().parent
ROOT = ASSETS.parent.parent
COMMAND = ['go', 'run', './examples/custom-checker']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--from-output', type=Path, help='Replay stdout captured from the same command, instead of running Go again')
    parser.add_argument('--font', type=Path, required=True, help='Path to the pinned IBM Plex Mono Regular font documented in the assets README')
    parser.add_argument('--frames-dir', type=Path, help='Write first, middle and final PNGs for review outside the repository')
    args = parser.parse_args()
    if args.from_output:
        output = args.from_output.read_text()
    else:
        output = subprocess.run(COMMAND, cwd=ROOT, capture_output=True, text=True, check=True, timeout=120).stdout
    readme = (ROOT / 'examples/custom-checker/README.md').read_text()
    expected = readme.split('```text\n', 1)[1].split('```', 1)[0]
    if output != expected:
        raise SystemExit('Actual output does not match the checked-in example README; inspect the difference first.')
    font_path = args.font
    expected_font_hash = '6a3412f058c7d8dfd9170c41e85ade48e5156ecb89356110ca57a0a27734af46'
    if hashlib.sha256(font_path.read_bytes()).hexdigest() != expected_font_hash:
        raise SystemExit('Font hash mismatch; use the pinned IBM Plex Mono Regular download from the assets README.')
    layout = ImageFont.Layout.BASIC
    font = ImageFont.truetype(str(font_path), 19, layout_engine=layout)
    small = ImageFont.truetype(str(font_path), 16, layout_engine=layout)
    title = ImageFont.truetype(str(font_path), 23, layout_engine=layout)
    rows = ['$ ' + ' '.join(COMMAND)]
    for line in output.rstrip('\n').splitlines():
        rows.extend(textwrap.wrap(line, width=91, subsequent_indent='  '))
    frames = []
    for count in (1, 2, len(rows)):
        img = Image.new('RGB', (1200, 380), '#0b1120')
        draw = ImageDraw.Draw(img)
        draw.text((32, 27), 'local-ci / custom checker', font=title, fill='#f1f5f9')
        draw.text((32, 70), 'REPLAY OF CAPTURED OUTPUT', font=small, fill='#b1bed0')
        draw.line((32, 105, 1168, 105), fill='#334155', width=1)
        for i, row in enumerate(rows[:count]):
            color = '#f1f5f9' if i == 0 else ('#86efac' if 'PASS' in row else '#fcd34d')
            draw.text((32, 132 + i * 35), row, font=font, fill=color)
        draw.text((32, 335), 'Synthetic diffs. No GitHub call. No stamp written.', font=small, fill='#b1bed0')
        frames.append(img)
    frames[0].save(ASSETS / 'demo.gif', save_all=True, append_images=frames[1:], duration=[1600, 1600, 4000], loop=0, optimize=False)
    frames[-1].save(ASSETS / 'demo-static.png')
    (ASSETS / 'demo-transcript.md').write_text('# Offline checker demo\n\nThis is a replay of captured stdout from the real offline example. It calls a custom\n`checker.Checker` on synthetic diffs, without GitHub access or stamp writes. The\nexample reports both outcomes and exits successfully after demonstrating them.\n\n```sh\n' + ' '.join(COMMAND) + '\n```\n\n```text\n' + output + '```\n')
    if args.frames_dir:
        args.frames_dir.mkdir(parents=True, exist_ok=True)
        for name, frame in zip(('first', 'middle', 'final'), frames):
            frame.save(args.frames_dir / f'demo-{name}.png')
    print('Generated labelled output replay, static PNG and transcript.')


if __name__ == '__main__':
    main()

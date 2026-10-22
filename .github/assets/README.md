# Presentation assets

The light and dark SVG banners are handwritten and use local system fonts. They show the local
check-to-stamp relationship. The demo shows the same custom checker as the root Quick start.

The GIF is a labelled replay of output captured from the real offline example, rather than a
screen recording or a live GitHub run. The example calls a custom `checker.Checker` with synthetic
diffs and shows both pass and fail reports. No GitHub request is made and no stamp is written.
[The static transcript](demo-transcript.md) and [final image](demo-static.png) show the whole result
without motion.

## Reproduce

You need Go 1.25 or newer, Python 3.11 or newer, Pillow 13.0.0, and the pinned IBM Plex Mono Regular font.
Pillow is an asset-generation dependency only. Its dependency license applies separately from
the first-party capture script.

From the repository root:

```sh
python3 -m venv /tmp/local-ci-demo-venv
/tmp/local-ci-demo-venv/bin/pip install Pillow==13.0.0
curl -fL https://raw.githubusercontent.com/google/fonts/47d54ca1ebb14fd864695f1d39dc734f39017c33/ofl/ibmplexmono/IBMPlexMono-Regular.ttf -o /tmp/local-ci-demo-font.ttf
/tmp/local-ci-demo-venv/bin/python .github/assets/capture-demo.py \
  --font /tmp/local-ci-demo-font.ttf --frames-dir /tmp/local-ci-demo-frames
```

The script runs `go run ./examples/custom-checker` with a 120-second timeout, checks its stdout
against the example README, then generates `demo.gif`, `demo-static.png` and `demo-transcript.md`.
Go may download dependencies on the first run. Example execution makes no off-machine calls.

The script requires this exact font and checks its SHA256 before rendering. The font is used
locally for rasterization and is not embedded in, or redistributed with, the GIF or PNG.
To render an already captured stdout file without running Go again, pass `--from-output FILE`;
that file must exactly match the documented example output.

Open the first, middle and final frames from `/tmp/local-ci-demo-frames` and check legibility, wrapping
and the visible result before accepting changed assets. The 7.2-second GIF uses three static
states without flashing. Preview the README at desktop and mobile widths in both themes.

## Asset and tool provenance

The raster demo uses IBM Plex Mono Regular, copyright © 2017 IBM Corp., with Reserved Font
Name "Plex". The inspected [SIL Open Font License 1.1](IBM-Plex-Mono-OFL.txt) permits use;
condition 5 says documents created using the font do not inherit the font's license. Only the
rendered image is shipped. The unchanged font file is downloaded separately for reproduction.

| Item | Pinned source or SHA256 |
|---|---|
| Font source | [google/fonts at 47d54ca1ebb14fd864695f1d39dc734f39017c33](https://github.com/google/fonts/blob/47d54ca1ebb14fd864695f1d39dc734f39017c33/ofl/ibmplexmono/IBMPlexMono-Regular.ttf) |
| Font SHA256 | `6a3412f058c7d8dfd9170c41e85ade48e5156ecb89356110ca57a0a27734af46` |
| License source | [OFL.txt at the same revision](https://github.com/google/fonts/blob/47d54ca1ebb14fd864695f1d39dc734f39017c33/ofl/ibmplexmono/OFL.txt) |
| License SHA256 | `7e6b2818edbd8f6a01ae80641cc8f16a51080d08fb4e532be3a0b6f74adb07da` |

Pillow 13.0.0 is a generation tool under [MIT-CMU](https://github.com/python-pillow/Pillow/blob/13.0.0/LICENSE),
not a runtime dependency or a shipped binary. Pillow's installed wheel also carries notices for
its bundled libraries, including FreeType. The renderer uses Pillow's basic FreeType text layout
and GIF/PNG encoders; it does not require advanced text layout or optional imaging plugins.
Pillow, FreeType and the font file are not included in the release archives.

Copyright 2026 Workloom Solutions Private Limited. First-party assets and the capture script
are covered by the root [MIT license](../../../LICENSE). The copied font license retains its own
copyright and terms.

#!/usr/bin/env python3
"""Makes gecko-symbols-*.woff2, the terminal's fallback for symbols that
JetBrains Mono and the phone's own fonts lack: the technical symbols,
among them Claude Code's mode marks (U+23F5, U+23F8) and ⏺ ⎿ ⏎.

Sources (SIL Open Font License 1.1, see OFL.txt): the "symbols" subsets of
@fontsource/noto-sans-symbols-2 and @fontsource/noto-sans-symbols 5.3.0.

    npm pack @fontsource/noto-sans-symbols-2@5.3.0 @fontsource/noto-sans-symbols@5.3.0
    python3 make-symbols.py \
      noto-sans-symbols-2-symbols-400-normal.woff2 \
      noto-sans-symbols-symbols-400-normal.woff2

Needs fontTools and brotli (pip install fonttools brotli).
"""
import sys
from fontTools import subset
from fontTools.ttLib import TTFont

TECH = (0x2300, 0x23FF)
OUT = {
    # Only the technical symbols (U+2300-23FF): the block phones lack (Android
    # has no U+23F5 and draws U+23F8 and U+23FA as colour emoji). Everything
    # else Claude Code draws, phones have, at a size that fits a cell.
    # Noto Sans Symbols 2: ⏵ ⏸ ⏺ ⏎ and most of the block.
    'gecko-symbols-a.woff2': (sys.argv[1], [TECH]),
    # Noto Sans Symbols: the rest of it (⎿ ⌕).
    'gecko-symbols-b.woff2': (sys.argv[2], [TECH]),
}
for out, (src, ranges) in OUT.items():
    font = TTFont(src)
    o = subset.Options()
    o.flavor = 'woff2'
    o.layout_features = []
    o.notdef_outline = True
    s = subset.Subsetter(o)
    s.populate(unicodes=[u for a, b in ranges for u in range(a, b + 1)])
    s.subset(font)
    font.flavor = 'woff2'
    font.save(out)
    cmap = font.getBestCmap()
    print(out, len(cmap), 'glyphs, ranges', ','.join(f'U+{min(u for u in cmap if a <= u <= b):04X}-{max(u for u in cmap if a <= u <= b):04X}' for a, b in ranges if any(a <= u <= b for u in cmap)))

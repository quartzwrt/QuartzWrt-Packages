#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Generate the operator icon atlas (16x16, RGB888 + 4bpp alpha) for quartz-screen.

DEV TOOL ONLY. Source SVGs are the Dashboard's official operator marks
(luci-app-dashboard/frontend/src/assets/operators, trademark notice there).
Chromium renders each SVG on a transparent 512x512 canvas; every 32x32 block is
box-downsampled to one 16x16 pixel (alpha-weighted RGB).

Binary format (QZI1, little endian):
  magic "QZI1" (4)
  u8 version (1)
  u8 count
  per icon:
    u8 name_len, name bytes
    u8 w, u8 h
    w*h*3 bytes RGB (row-major)
    ceil(w*h/2) bytes alpha 4bpp (high nibble first)
"""

import base64
import struct
from pathlib import Path

from playwright.sync_api import sync_playwright

HERE = Path(__file__).resolve()
PKG = HERE.parents[2]
SRC = PKG.parents[1] / "luci/applications/luci-app-dashboard/frontend/src/assets/operators"
OUT = PKG / "src/internal/render/icondata/operators.bin"
SIZE = 16
RENDER = 512

ICONS = ["china-mobile", "china-unicom", "china-telecom"]

JS = """async (p) => {
  const c = document.getElementById('c');
  c.width = p.size; c.height = p.size;
  const ctx = c.getContext('2d');
  ctx.clearRect(0, 0, c.width, c.height);
  const img = new Image();
  img.src = 'data:image/svg+xml;base64,' + p.b64;
  await img.decode();
  ctx.drawImage(img, 0, 0, c.width, c.height);
  const d = ctx.getImageData(0, 0, c.width, c.height).data;
  const n = 16, cell = c.width / n;
  const out = [];
  for (let y = 0; y < n; y++) {
    for (let x = 0; x < n; x++) {
      let sa = 0, sr = 0, sg = 0, sb = 0, cnt = 0;
      for (let yy = 0; yy < cell; yy++) {
        for (let xx = 0; xx < cell; xx++) {
          const o = (((y * cell + yy) * c.width) + x * cell + xx) * 4;
          const a = d[o + 3];
          sa += a; sr += d[o] * a; sg += d[o + 1] * a; sb += d[o + 2] * a; cnt++;
        }
      }
      if (sa === 0) out.push([0, 0, 0, 0]);
      else out.push([Math.round(sr / sa), Math.round(sg / sa), Math.round(sb / sa), Math.round(sa / cnt)]);
    }
  }
  return out;
}"""


def main():
    OUT.parent.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, args=["--no-sandbox"])
        page = browser.new_page()
        page.set_content("<canvas id=c></canvas>")
        icons = []
        for name in ICONS:
            svg = (SRC / f"{name}.svg").read_text()
            b64 = base64.b64encode(svg.encode()).decode()
            px = page.evaluate(JS, {"size": RENDER, "b64": b64})
            assert len(px) == SIZE * SIZE, (name, len(px))
            icons.append((name, px))
            print(f"{name}: {len(px)} px")
        browser.close()

    buf = bytearray(b"QZI1")
    buf += struct.pack("<BB", 1, len(icons))
    for name, px in icons:
        nb = name.encode()
        buf += struct.pack("<B", len(nb)) + nb
        buf += struct.pack("<BB", SIZE, SIZE)
        for r, g, b, a in px:
            buf += bytes([r, g, b])
        for i in range(0, len(px), 2):
            hi = round(px[i][3] / 17)
            lo = round(px[i + 1][3] / 17) if i + 1 < len(px) else 0
            buf += bytes([(hi << 4) | lo])
    OUT.write_bytes(bytes(buf))
    print(f"{OUT}: {len(buf)} bytes")


if __name__ == "__main__":
    main()

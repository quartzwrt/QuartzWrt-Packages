#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Generate bitmap font atlases for quartz-screen.

DEV TOOL ONLY — not installed on the router and not needed at build time.
The generated *.bin files are committed under src/internal/render/fontdata/.

Requirements on the development host:
  - Python 3 + Playwright (Chromium)
  - Noto Sans CJK SC / Noto Sans Mono CJK SC (SIL OFL 1.1)

Glyphs are rendered at 4x with Chromium and box-downsampled in the browser to
4bpp coverage (anti-aliased) or 1bpp (GB2312 fallback).

Binary format (little endian):
  magic "QZF1" (4 bytes)
  u8 version (1)
  u8 bpp (4 or 1)
  u8 ascent (px)
  u8 line_height (px)
  u16 glyph_count
  per glyph:
    u32 codepoint
    i16 bearing_x (px, relative to pen)
    i16 bearing_y (px, relative to baseline; negative = above)
    u16 advance_16 (1/16 px)
    u8 w, u8 h
    bitmap: ceil(w*h*bpp/8) bytes
      4bpp: two pixels per byte, high nibble first
      1bpp: rows MSB first
"""

import math
import struct
from pathlib import Path

from playwright.sync_api import sync_playwright

SS = 4  # supersampling factor
MAX_CANVAS_W = 12000

UI_CJK = (
    "概览网络无线蜂窝系统时钟便签"
    "已运行天小时分钟秒"
    "已连接未连接在线离线客户端设备"
    "温度内存处理器磁盘固件内核型号负载"
    "信号质量运营商制式频段带宽速率"
    "下行上行累计流量总量今日本月"
    "无数据源未知可用不可用"
    "启用停用开启关闭夜间亮度调光息屏启动进程就绪隐藏"
    "年月日星期上午下午"
    "摄氏度华氏度"
    "当前页面正在载入等待"
    "路由器地址用户名密码"
    "连接状态注册拨号小区"
    "SIM卡槽强中弱"
    "首页详情设置"
    "共个条第项"
    "扫描中请稍候"
    "刷新成功失败错误"
    "分钟前刚刚"
    "固态存储根分区"
    "下载上传连接数"
    "双工"
    "关机重启"
    "正常异常警告"
    "周一周二周三周四周五周六周日"
    "一二三四五六七八九十"
    "峰值广播中国移动电信联通广电铁机房联维护联系"
    "发射接收连接断开"
)

ASCII_EXTRA = "°·…—–↑↓←→▲▼●○▪▫✓✗×"

FACES = [
    # file, css family, weight, px, glyph kind
    ("ui9", "Noto Sans CJK SC", 400, 9, "ui"),
    ("ui11", "Noto Sans CJK SC", 400, 11, "ui"),
    ("ui13", "Noto Sans CJK SC", 400, 13, "ui"),
    ("uib13", "Noto Sans CJK SC", 700, 13, "ui"),
    ("uib16", "Noto Sans CJK SC", 700, 16, "ui"),
    ("uib22", "Noto Sans CJK SC", 700, 22, "ui"),
    ("uib28", "Noto Sans CJK SC", 700, 28, "ui"),
    ("mono11", "Noto Sans Mono CJK SC", 500, 11, "ascii"),
    ("mono13", "Noto Sans Mono CJK SC", 400, 13, "ascii"),
    ("mono16", "Noto Sans Mono CJK SC", 500, 16, "ascii"),
    ("mono22", "Noto Sans Mono CJK SC", 500, 22, "ascii"),
    ("clock40", "Noto Sans Mono CJK SC", 700, 40, "clock"),
    ("clock64", "Noto Sans Mono CJK SC", 700, 64, "clock"),
]

OUT_DIR = Path(__file__).resolve().parents[2] / "src" / "internal" / "render" / "fontdata"


def ui_glyphs():
    cps = set(range(0x20, 0x7F))
    cps.update(ord(c) for c in ASCII_EXTRA)
    cps.update(ord(c) for c in UI_CJK)
    return sorted(cps)


def ascii_glyphs():
    cps = set(range(0x20, 0x7F))
    cps.update(ord(c) for c in ASCII_EXTRA)
    return sorted(cps)


def clock_glyphs():
    return [ord(":"), ord(" "), ord("A"), ord("P"), ord("M")] + list(range(ord("0"), ord("9") + 1))


JS_RENDER = """(p) => {
  const c = document.getElementById('c');
  c.width = p.width; c.height = p.height;
  const ctx = c.getContext('2d');
  ctx.fillStyle = '#000'; ctx.fillRect(0, 0, c.width, c.height);
  ctx.font = `${p.weight} ${p.size}px "${p.family}"`;
  ctx.textBaseline = 'alphabetic';
  ctx.fillStyle = '#fff';
  const advances = [];
  for (let i = 0; i < p.cps.length; i++) {
    const ch = String.fromCodePoint(p.cps[i]);
    advances.push(ctx.measureText(ch).width / p.ss);
    ctx.fillText(ch, i * p.cellW + p.margin, p.baseline);
  }
  const img = ctx.getImageData(0, 0, c.width, c.height).data;
  const tw = Math.floor(p.cellW / p.ss), th = Math.floor(p.cellH / p.ss);
  const out = [];
  for (let i = 0; i < p.cps.length; i++) {
    const cx = i * p.cellW;
    // box-downsample one cell
    const small = new Uint8Array(tw * th);
    for (let y = 0; y < th; y++) {
      for (let x = 0; x < tw; x++) {
        let acc = 0;
        for (let yy = 0; yy < p.ss; yy++) {
          for (let xx = 0; xx < p.ss; xx++) {
            acc += img[(((y * p.ss + yy) * p.width) + cx + x * p.ss + xx) * 4];
          }
        }
        small[y * tw + x] = p.bpp === 4
          ? Math.round(acc / (p.ss * p.ss) * 15 / 255)
          : (acc >= (p.ss * p.ss * 255 / 2) ? 15 : 0);
      }
    }
    // bbox of non-zero coverage
    let x0 = tw, x1 = -1, y0 = th, y1 = -1;
    for (let y = 0; y < th; y++) {
      for (let x = 0; x < tw; x++) {
        if (small[y * tw + x] > 0) {
          if (x < x0) x0 = x;
          if (x > x1) x1 = x;
          if (y < y0) y0 = y;
          if (y > y1) y1 = y;
        }
      }
    }
    if (x1 < 0) {
      out.push({cp: p.cps[i], bx: 0, by: 0,
                adv: Math.max(1, Math.round(advances[i] * 16)),
                w: 0, h: 0, bm: []});
      continue;
    }
    const w = x1 - x0 + 1, h = y1 - y0 + 1;
    const bm = [];
    for (let y = y0; y <= y1; y++)
      for (let x = x0; x <= x1; x++)
        bm.push(small[y * tw + x]);
    out.push({
      cp: p.cps[i],
      bx: x0 - Math.floor(p.margin / p.ss),
      by: y0 - Math.floor(p.baseline / p.ss),
      adv: Math.max(1, Math.round(advances[i] * 16)),
      w, h, bm
    });
  }
  return out;
}"""


def render_face(page, family, weight, size, cps):
    cell_w = int(math.ceil(size * 1.45)) * SS
    cell_h = int(math.ceil(size * 1.65)) * SS
    baseline = int(math.ceil(size * 1.10)) * SS
    margin = int(math.ceil(size * 0.18)) * SS
    per = max(1, MAX_CANVAS_W // cell_w)
    glyphs = []
    for start in range(0, len(cps), per):
        chunk = cps[start : start + per]
        payload = {
            "width": cell_w * len(chunk),
            "height": cell_h,
            "cellW": cell_w,
            "cellH": cell_h,
            "baseline": baseline,
            "margin": margin,
            "family": family,
            "weight": weight,
            "size": size * SS,
            "ss": SS,
            "bpp": 4,
            "cps": chunk,
        }
        page.set_viewport_size({"width": 800, "height": 600})
        page.set_content("<canvas id=c></canvas>")
        res = page.evaluate(JS_RENDER, payload)
        glyphs.extend(res)
        print(f"  batch {start//per+1}: {len(chunk)} glyphs")
    ascent = max(1, round(size * 0.98))
    return ascent, max(1, round(size * 1.35)), glyphs


def render_gb2312(page):
    cps = gb2312_chars()
    size = 14
    cell = 16
    cols = 64
    per = cols * 64
    glyphs = []
    for start in range(0, len(cps), per):
        chunk = cps[start : start + per]
        rows = (len(chunk) + cols - 1) // cols
        payload = {
            "width": cols * cell * SS,
            "height": rows * cell * SS,
            "cellW": cell * SS,
            "cellH": cell * SS,
            "baseline": (cell // 2 + 7) * SS,
            "margin": 0,
            "family": "Noto Sans CJK SC",
            "weight": 400,
            "size": size * SS,
            "ss": SS,
            "bpp": 1,
            "cps": chunk,
            "center": True,
        }
        page.set_viewport_size({"width": 800, "height": 600})
        page.set_content("<canvas id=c></canvas>")
        res = page.evaluate(
            """(p) => {
            const c = document.getElementById('c');
            c.width = p.width; c.height = p.height;
            const ctx = c.getContext('2d');
            ctx.fillStyle = '#000'; ctx.fillRect(0, 0, c.width, c.height);
            ctx.font = `400 ${p.size}px "${p.family}"`;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillStyle = '#fff';
            for (let i = 0; i < p.cps.length; i++) {
              const col = i % 64, row = Math.floor(i / 64);
              ctx.fillText(String.fromCodePoint(p.cps[i]),
                (col + 0.5) * p.cellW, (row + 0.5) * p.cellH);
            }
            const img = ctx.getImageData(0, 0, c.width, c.height).data;
            const tw = Math.floor(p.cellW / p.ss), th = Math.floor(p.cellH / p.ss);
            const out = [];
            for (let i = 0; i < p.cps.length; i++) {
              const cx = (i % 64) * p.cellW;
              const cy = Math.floor(i / 64) * p.cellH;
              const bm = [];
              for (let y = 0; y < th; y++) {
                for (let x = 0; x < tw; x++) {
                  let acc = 0;
                  for (let yy = 0; yy < p.ss; yy++)
                    for (let xx = 0; xx < p.ss; xx++)
                      acc += img[(((cy + y * p.ss + yy) * p.width) + cx + x * p.ss + xx) * 4];
                  bm.push(acc >= (p.ss * p.ss * 255 / 2) ? 15 : 0);
                }
              }
              out.push({cp: p.cps[i], bx: 0, by: -13, adv: 16 * 16,
                        w: tw, h: th, bm});
            }
            return out;
        }""",
            payload,
        )
        glyphs.extend(res)
        print(f"  gb2312 batch {start//per+1}: {len(chunk)} glyphs")
    return glyphs


def gb2312_chars():
    out = set()
    for hi in range(0xB0, 0xF8):
        for lo in range(0xA1, 0xFF):
            try:
                ch = bytes([hi, lo]).decode("gb2312")
            except UnicodeDecodeError:
                continue
            out.add(ord(ch))
    return sorted(out)


def write_face(path, bpp, ascent, line_height, glyphs):
    buf = bytearray()
    buf += b"QZF1"
    buf += struct.pack("<BBBBH", 1, bpp, ascent, line_height, len(glyphs))
    for g in glyphs:
        buf += struct.pack(
            "<IhhHBB", g["cp"], g["bx"], g["by"], g["adv"], g["w"], g["h"]
        )
        data = g["bm"]
        if bpp == 4:
            for i in range(0, len(data), 2):
                hi = data[i] & 0xF
                lo = data[i + 1] & 0xF if i + 1 < len(data) else 0
                buf.append((hi << 4) | lo)
        else:
            for y in range(g["h"]):
                row = data[y * g["w"] : (y + 1) * g["w"]]
                for x0 in range(0, g["w"], 8):
                    byte = 0
                    for bit in range(8):
                        if x0 + bit < g["w"] and row[x0 + bit]:
                            byte |= 1 << (7 - bit)
                    buf.append(byte)
    path.write_bytes(bytes(buf))
    print(f"  {path.name}: {len(glyphs)} glyphs, {len(buf)/1024:.1f} KiB")


def main():
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(
            headless=True, args=["--no-sandbox", "--font-render-hinting=none"]
        )
        page = browser.new_page()
        for name, family, weight, size, kind in FACES:
            if kind == "ui":
                cps = ui_glyphs()
            elif kind == "ascii":
                cps = ascii_glyphs()
            else:
                cps = clock_glyphs()
            print(f"{name}:")
            ascent, line_height, glyphs = render_face(page, family, weight, size, cps)
            write_face(OUT_DIR / f"{name}.bin", 4, ascent, line_height, glyphs)
        print("fallback14:")
        glyphs = render_gb2312(page)
        write_face(OUT_DIR / "fallback14.bin", 1, 14, 18, glyphs)
        browser.close()
    print("done")


if __name__ == "__main__":
    main()

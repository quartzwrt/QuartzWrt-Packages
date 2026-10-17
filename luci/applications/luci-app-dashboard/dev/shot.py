#!/usr/bin/env python3
"""luci-app-dashboard 静态截图基线（dev 工具，不进安装包）

生成 dev/shots/ 下的 dashboard-*（预览页 + mock）与 app-*（真实构建产物 + mock 宿主
适配器）基线截图。与 dev/vm-shot.py（固件内真实数据）分工：本脚本不需要 VM。

前置:
  - python3 -m pip install --user --break-system-packages playwright
  - python3 -m playwright install chromium
  - 起预览服务：dev/serve.sh（默认 8898）

用法:
  dev/shot.py                  # 全部基线（双模 × 四态 + app 三态）
  dev/shot.py --only dark      # 只出 dark 系列
  BASE=http://127.0.0.1:8898 dev/shot.py
"""
import argparse, os, sys
from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "shots")
BASE = os.environ.get("BASE", "http://127.0.0.1:8898")
PREVIEW = BASE + "/applications/luci-app-dashboard/dev/preview.html"

# (文件名, 查询串, 视口)
SHOTS = [
    ("dashboard-dark.png",      "?theme=dark",               (1280, 900)),
    ("dashboard-light.png",     "?theme=light",              (1280, 900)),
    ("dashboard-wallpaper.png", "?theme=dark&wallpaper=1&alpha=80", (1280, 900)),
    ("dashboard-partial.png",   "?theme=dark&state=partial", (1280, 900)),
    ("dashboard-down.png",      "?theme=dark&state=down",    (1280, 900)),
    ("dashboard-mobile.png",    "?theme=dark",               (477, 900)),
    ("app-normal.png",          "?app=1&theme=dark",         (1280, 900)),
    ("app-partial.png",         "?app=1&theme=dark&state=partial", (1280, 900)),
    ("app-down.png",            "?app=1&theme=dark&state=down", (1280, 900)),
    ("app-mobile.png",          "?app=1&theme=dark",         (477, 900)),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", default="", help="只跑名字含该子串的项（如 dark / app）")
    args = ap.parse_args()

    os.makedirs(OUT, exist_ok=True)
    made = []

    with sync_playwright() as p:
        browser = p.chromium.launch()
        for name, query, (w, h) in SHOTS:
            if args.only and args.only not in name:
                continue
            ctx = browser.new_context(viewport={"width": w, "height": h},
                                      device_scale_factor=1)
            pg = ctx.new_page()
            pg.goto(PREVIEW + query, wait_until="load", timeout=30000)
            pg.wait_for_timeout(900)          # mock fetch + 渲染
            path = os.path.join(OUT, name)
            pg.screenshot(path=path, full_page=True)
            made.append(name)
            ctx.close()
        browser.close()

    print("生成 %d 张：" % len(made))
    for n in made:
        print("  ", n)
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""W2.9 壁纸管理面 —— VM 端到端验收（Playwright + VM 预览台）

前置:
  - VM 已启动（QuartzWrt-Builder/scripts/test-in-vm.sh --detach；宿主 8080 → VM:80）
  - 被测包已装入 VM（apk add --allow-untrusted luci-app-quartz-config-*.apk）
  - python3 -m pip install --user --break-system-packages playwright
    python3 -m playwright install chromium

用法:
  python3 wallpaper-e2e.py [--base http://127.0.0.1:8080] [--out dev/shots/vm-wallpaper]

覆盖链路（每个断言 = 一条 check）:
  初始态（无壁纸：图片隐藏/无移除按钮/无 data-qz-wallpaper/--qz-wallpaper-on=none）
  → 上传 2400x1350 PNG：客户端收敛断言（页面回显 1920x1080）→ 落盘断言（HTTP 200、
    JPEG 魔数、尺寸/权限由调用方 ssh 复核）→ 本页即时生效（属性 + 变量 + body::before）
  → 跨页探测（概览页 theme.js 命中 data-qz-wallpaper）
  → 替换 3000x1000 PNG（收敛到 1920x640，md5 变化）
  → 移除（URL 404、开关回落、按钮回隐藏）

证据输出：<out>/*.png + <out>/run-log.jsonl
"""
import argparse
import hashlib
import json
import os
import struct
import sys
import time
import urllib.error
import urllib.request
import zlib
from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
CONFIG_PATH = "/cgi-bin/luci/admin/system/quartz-config"
OVERVIEW_PATH = "/cgi-bin/luci/admin/status/overview"
SERVED_PATH = "/quartz-wallpaper/wallpaper.jpg"

RESULTS = []

# 全页截图是"整份文档"的拼接图，而贴底操作条（.cbi-page-actions）是 position: sticky 的
# 视口相对 chrome——直接 full_page 截会把它定格在"视口底部"位置（长图里看起来悬在页面中部，
# 盖住中段内容）。截图前解除 sticky，操作条回到文档流内的真实位置（= 用户滚到页底时看到的
# 位置）；视口截图不受影响、仍呈现真实贴底行为。
FLAT_ACTIONS_CSS = ".cbi-page-actions{position:static !important}"


def shot(page, path, full_page=True):
    if full_page:
        page.add_style_tag(content=FLAT_ACTIONS_CSS)
    page.screenshot(path=path, full_page=full_page, animations="disabled")


def check(name, ok, detail=""):
    RESULTS.append({"check": name, "ok": bool(ok), "detail": str(detail)[:400]})
    print("%s %s%s" % ("PASS" if ok else "FAIL", name, (" — " + str(detail)[:200]) if detail else ""))
    return bool(ok)


def write_png(path, w, h, rgb):
    """纯标准库生成测试 PNG（免 Pillow 依赖）"""
    raw = b"".join(b"\x00" + bytes(rgb) * w for _ in range(h))

    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 6))
           + chunk(b"IEND", b""))
    open(path, "wb").write(png)
    return path


def http_get(url):
    try:
        with urllib.request.urlopen(urllib.request.Request(url), timeout=15) as res:
            return res.status, res.read()
    except urllib.error.HTTPError as e:
        return e.code, b""


def probe(page):
    """当前页面上的壁纸状态（主题层 + 控件层）"""
    return page.evaluate("""() => {
        const root = document.documentElement;
        const img = document.querySelector('#qz-wallpaper-image');
        const rm = document.querySelector('#qz-wallpaper-remove');
        const st = document.querySelector('#qz-wallpaper-status');
        const before = getComputedStyle(document.body, '::before');
        return {
            attr: root.hasAttribute('data-qz-wallpaper'),
            var_on: getComputedStyle(root).getPropertyValue('--qz-wallpaper-on').trim(),
            before_img: (before.backgroundImage || '').slice(0, 120),
            img_visible: !!img && img.style.display !== 'none',
            img_src: img ? (img.getAttribute('src') || '') : null,
            img_natural: img ? [img.naturalWidth, img.naturalHeight] : null,
            remove_visible: !!rm && rm.style.display !== 'none',
            status: st ? st.textContent.trim() : null,
            upload_visible: !!document.querySelector('#qz-wallpaper-upload')
        };
    }""")


def login(ctx, base, password=""):
    pg = ctx.new_page()
    pg.goto(base + "/cgi-bin/luci/", wait_until="domcontentloaded", timeout=20000)
    pg.wait_for_selector("#luci_password", timeout=15000)
    pg.fill("#luci_username", "root")
    pg.fill("#luci_password", password)
    pg.click("button.cbi-button-positive.important")
    pg.wait_for_selector("#qz-sidebar", timeout=15000)
    return pg


def upload(pg, path, expect_text, timeout=60000):
    with pg.expect_file_chooser() as fc:
        pg.click("#qz-wallpaper-upload")
    fc.value.set_files(path)
    pg.wait_for_function(
        "() => (document.querySelector('#qz-wallpaper-status') || {}).textContent?.includes(%s)"
        % json.dumps(expect_text), timeout=timeout)
    pg.wait_for_timeout(800)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:8080")
    ap.add_argument("--out", default=os.path.join(HERE, "shots", "vm-wallpaper"))
    ap.add_argument("--vm-password", default="")
    args = ap.parse_args()

    outdir = os.path.abspath(args.out)
    os.makedirs(outdir, exist_ok=True)

    img1 = write_png(os.path.join(outdir, "src-orange-2400x1350.png"), 2400, 1350, (222, 130, 60))
    img2 = write_png(os.path.join(outdir, "src-blue-3000x1000.png"), 3000, 1000, (40, 90, 200))

    log = []
    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        ctx = browser.new_context(viewport={"width": 1600, "height": 1000},
                                  color_scheme="dark", locale="zh-CN")
        errors = []
        page = login(ctx, args.base, args.vm_password)
        page.on("pageerror", lambda e: errors.append(str(e)[:200]))
        page.goto(args.base + CONFIG_PATH, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_selector("#qz-wallpaper-upload", timeout=30000)
        page.wait_for_timeout(1500)

        st = probe(page)
        log.append({"step": "initial", **st})
        check("初始态：控件就位（上传按钮可见）", st["upload_visible"])
        check("初始态：无壁纸（图片/移除按钮隐藏）",
              not st["img_visible"] and not st["remove_visible"], json.dumps(st, ensure_ascii=False)[:200])
        check("初始态：data-qz-wallpaper 未置位", not st["attr"])
        shot(page, os.path.join(outdir, "01-initial-dark.png"))

        # ---- 上传 ----
        status, body = http_get(args.base + SERVED_PATH)
        check("上传前：/quartz-wallpaper/wallpaper.jpg 非 200（404）", status == 404, "HTTP %s" % status)

        upload(page, img1, "1920x1080")
        st = probe(page)
        log.append({"step": "uploaded", **st})
        shot(page, os.path.join(outdir, "02-uploaded-dark.png"))
        check("上传：客户端收敛为 1920x1080（页面回显）", "1920x1080" in (st["status"] or ""), st["status"])
        check("上传：预览图可见且非空 src", st["img_visible"] and (st["img_src"] or "").startswith(SERVED_PATH),
              st["img_src"])
        check("上传：本页即时生效（data-qz-wallpaper + body::before 指向带版本号的 URL）",
              st["attr"] and "wallpaper.jpg?v=" in st["before_img"], st["before_img"])

        status, body = http_get(args.base + SERVED_PATH)
        md5_1 = hashlib.md5(body).hexdigest()
        check("上传：服务端 200 且为 JPEG（魔数 ffd8ff）", status == 200 and body[:3] == b"\xff\xd8\xff",
              "HTTP %s, %d bytes, md5=%s" % (status, len(body), md5_1))
        check("上传：落盘体积 ≤2MB", 0 < len(body) <= 2 * 1024 * 1024, "%d bytes" % len(body))

        # ---- 跨页探测（theme.js 的 Image() 探测路径）----
        other = ctx.new_page()
        other.on("pageerror", lambda e: errors.append("overview: " + str(e)[:200]))
        other.goto(args.base + OVERVIEW_PATH, wait_until="domcontentloaded", timeout=30000)
        other.wait_for_selector("#qz-sidebar", timeout=15000)
        other.wait_for_timeout(2500)
        attr_other = other.evaluate("() => document.documentElement.hasAttribute('data-qz-wallpaper')")
        shot(other, os.path.join(outdir, "03-overview-probe-dark.png"), full_page=False)
        log.append({"step": "overview-probe", "attr": attr_other})
        check("跨页：概览页 theme.js 探测命中 data-qz-wallpaper", attr_other)

        # ---- 替换 ----
        upload(page, img2, "1920x640")
        st = probe(page)
        log.append({"step": "replaced", **st})
        shot(page, os.path.join(outdir, "04-replaced-dark.png"))
        status, body2 = http_get(args.base + SERVED_PATH)
        md5_2 = hashlib.md5(body2).hexdigest()
        check("替换：收敛为 1920x640（页面回显）", "1920x640" in (st["status"] or ""), st["status"])
        check("替换：服务端文件确已更新（md5 变化）", status == 200 and md5_2 != md5_1,
              "md5 %s → %s" % (md5_1, md5_2))

        # ---- 移除 ----
        page.click("#qz-wallpaper-remove")
        page.wait_for_selector(".modal .cbi-button-remove", timeout=10000)
        shot(page, os.path.join(outdir, "05-confirm-remove-dark.png"), full_page=False)
        page.click(".modal .cbi-button-remove")
        page.wait_for_selector("#qz-wallpaper-remove", state="hidden", timeout=20000)
        page.wait_for_timeout(1000)
        st = probe(page)
        log.append({"step": "removed", **st})
        status, body3 = http_get(args.base + SERVED_PATH)
        check("移除：服务端文件消失（404）", status == 404, "HTTP %s" % status)
        check("移除：本页回落（无 data-qz-wallpaper、图片隐藏、移除按钮回隐藏）",
              (not st["attr"]) and (not st["img_visible"]) and (not st["remove_visible"]),
              json.dumps(st, ensure_ascii=False)[:200])
        shot(page, os.path.join(outdir, "06-removed-dark.png"))

        check("全程无页面 JS 异常", not errors, "; ".join(errors[:3]))

        browser.close()

    with open(os.path.join(outdir, "run-log.jsonl"), "w") as f:
        for row in log:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")

    failed = [r for r in RESULTS if not r["ok"]]
    print("\n==> %d/%d checks passed%s" % (len(RESULTS) - len(failed), len(RESULTS),
                                           "" if not failed else "，失败：" + ", ".join(r["check"] for r in failed)))
    print("证据：%s" % outdir)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

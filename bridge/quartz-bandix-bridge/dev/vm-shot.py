#!/usr/bin/env python3
"""quartz-bandix-bridge VM 视觉证据（WP-8 / W8.6，dev 工具不进安装包）

真浏览器登录 LuCI → 截 bandix 四页（上游前端，在 quartz 主题下）→ 落地 dev/shots/vm/。
与 dev/vm-accept.sh（协议层 41 项）分工：本脚本证明"网页端看到的东西与 bridge 契约同源"，
并留档 bandix 页在 QuartzWrt 主题下的渲染状态。

前置：
  QuartzWrt-Builder/scripts/test-in-vm.sh --detach      # 宿主 8080 → VM:80
用法（在包目录下）：
  python3 dev/vm-shot.py --tag wp8 --locale zh-CN
输出：
  dev/shots/vm/bandix-<页>-<tag>.png + run-<tag>.json（DOM 摘要/控制台/坏响应）
"""
import argparse, json, os, sys
from playwright.sync_api import sync_playwright

BASE = "http://127.0.0.1:8080"
PAGES = [
    ("status", "admin/network/bandix"),
    ("connection", "admin/network/bandix/connection"),
    ("dns", "admin/network/bandix/dns"),
    ("settings", "admin/network/bandix/settings"),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--password", default="quartz")
    ap.add_argument("--out", default="dev/shots/vm")
    ap.add_argument("--tag", default="wp8")
    ap.add_argument("--locale", default="zh-CN")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    console, bad = [], []
    evidence = {"tag": args.tag, "base": BASE, "pages": {}}

    with sync_playwright() as p:
        b = p.chromium.launch()
        ctx = b.new_context(viewport={"width": 1500, "height": 1000}, locale=args.locale)
        pg = ctx.new_page()
        pg.on("console", lambda m: console.append(f"{m.type}: {m.text}") if m.type == "error" else None)
        pg.on("response", lambda r: bad.append(f"{r.status} {r.url}") if r.status >= 400 else None)

        # 登录（登录页是 JS 视图；字段 luci_username/luci_password）
        pg.goto(f"{BASE}/cgi-bin/luci/", wait_until="load")
        pg.wait_for_timeout(1500)
        if pg.locator('input[name="luci_username"]').count():
            pg.fill('input[name="luci_username"]', "root")
        pg.fill('input[name="luci_password"]', args.password)
        pg.keyboard.press("Enter")
        pg.wait_for_load_state("load")
        pg.wait_for_timeout(2500)

        for name, path in PAGES:
            pg.goto(f"{BASE}/cgi-bin/luci/{path}", wait_until="load")
            pg.wait_for_timeout(3500)
            d = pg.evaluate(
                """() => ({
                    url: location.href,
                    title: document.title,
                    text: (document.body.innerText || '').replace(/\\s+/g, ' ').slice(0, 600),
                    tables: document.querySelectorAll('table').length,
                    rows: document.querySelectorAll('table tr').length,
                })"""
            )
            evidence["pages"][name] = d
            pg.screenshot(path=f"{args.out}/bandix-{name}-{args.tag}.png", full_page=True)

        ctx.close()
        b.close()

    evidence["console_errors"] = console
    evidence["bad_responses"] = bad
    with open(f"{args.out}/run-{args.tag}.json", "w", encoding="utf-8") as f:
        json.dump(evidence, f, ensure_ascii=False, indent=2)

    for name, _ in PAGES:
        print(f"  bandix-{name}-{args.tag}.png")
    print(f"evidence: {args.out}/run-{args.tag}.json")
    return 0


if __name__ == "__main__":
    sys.exit(main())

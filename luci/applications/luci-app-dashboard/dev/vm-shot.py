#!/usr/bin/env python3
"""
WP-3 固件内验收（W3.6）：真浏览器登录 LuCI → 截图落地页 / Dashboard，采集证据。

前置：
  QuartzWrt-Builder/scripts/test-in-vm.sh            # 起 VM（宿主 8080 → VM:80）
用法：
  python3 dev/vm-shot.py --tag nodata            # 无采集器数据（降级态）
  ssh -p 2222 root@127.0.0.1 "mkdir -p /var/run/quartz && cat > /var/run/quartz/status.json" < dev/mock/status.json
  python3 dev/vm-shot.py --tag data              # 有快照（正常态）
输出：
  dev/shots/vm/landing-<tag>.png / dashboard-<tag>.png
  dev/shots/vm/run-<tag>.json                    # DOM/网络/控制台证据
"""
import argparse, json, os, re, sys
from playwright.sync_api import sync_playwright

BASE = "http://127.0.0.1:8080"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--password", default="quartz")
    ap.add_argument("--out", default="shots/vm")
    ap.add_argument("--tag", default="")
    ap.add_argument("--locale", default=None, help="浏览器语言（如 zh-CN），用于验证 i18n")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    console, bad = [], []
    evidence = {"tag": args.tag, "base": BASE}

    with sync_playwright() as p:
        b = p.chromium.launch()
        ctx = b.new_context(viewport={"width": 1500, "height": 1000}, locale=(args.locale or "en-US"))
        pg = ctx.new_page()
        pg.on("console", lambda m: console.append(f"{m.type}: {m.text}") if m.type in ("error", "warning") else None)
        pg.on("response", lambda r: bad.append(f"{r.status} {r.url}") if r.status >= 400 else None)

        # ---- 登录（登录页是 JS 视图；表单字段 luci_username/luci_password）----
        pg.goto(f"{BASE}/cgi-bin/luci/", wait_until="load")
        pg.wait_for_timeout(1500)
        try:
            if pg.locator('input[name="luci_username"]').count():
                pg.fill('input[name="luci_username"]', "root")
            pg.fill('input[name="luci_password"]', args.password)
            pg.keyboard.press("Enter")
        except Exception as e:
            evidence["login_error"] = str(e)
        pg.wait_for_load_state("load")
        pg.wait_for_timeout(2500)

        landing_url = pg.url
        evidence["landing_url"] = landing_url
        pg.screenshot(path=f"{args.out}/landing-{args.tag}.png", full_page=True)

        def snapshot_dom(path_tag, name):
            d = pg.evaluate(
                """() => ({
                    url: location.href,
                    title: document.title,
                    cards: document.querySelectorAll('.qzd-card').length,
                    hero: !!document.querySelector('.qzd-hero'),
                    banner: (document.querySelector('.alert-message.error h4')||{}).textContent || null,
                    emptyStates: document.querySelectorAll('.qzd-empty').length,
                    tableRows: document.querySelectorAll('.qzd-table tbody tr').length,
                    deepLinks: Array.from(document.querySelectorAll('.qzd-more')).map(a => a.getAttribute('href') + ' | ' + a.textContent.trim()),
                    hostname: (document.querySelector('.qzd-hostname')||{}).textContent || null,
                    rates: Array.from(document.querySelectorAll('.qzd-rate .v')).map(e => e.textContent.trim()),
                    cardsText: Array.from(document.querySelectorAll('.qzd-card h4')).map(e => e.textContent.trim()),
                    menuActive: (document.querySelector('#qz-nav .qz-nav-item.active a')||{}).textContent || null,
                    sidebarFirst: (document.querySelector('#qz-nav .qz-nav-item a')||{}).textContent || null,
                })"""
            )
            evidence[name] = d
            pg.screenshot(path=f"{args.out}/{name}-{args.tag}.png", full_page=True)

        # ---- 落地页（登录后 redirect 目标）----
        snapshot_dom(args.tag, "landing")

        # ---- 显式访问 dashboard ----
        pg.goto(f"{BASE}/cgi-bin/luci/admin/dashboard", wait_until="load")
        pg.wait_for_timeout(4000)  # 等产物加载 + 首次取数 + 首屏渲染
        snapshot_dom(args.tag, "dashboard")

        # ---- 明暗双模各截一张（主题的三态切换钮）----
        for mode in ("dark", "light"):
            try:
                pg.evaluate(
                    """(m) => { const r = document.documentElement;
                        r.setAttribute('data-darkmode', m === 'dark' ? 'true' : 'false');
                        r.setAttribute('data-qz-mode', m); }""",
                    mode,
                )
                pg.wait_for_timeout(600)
                pg.screenshot(path=f"{args.out}/dashboard-{mode}-{args.tag}.png", full_page=True)
            except Exception as e:
                evidence[f"mode_{mode}_error"] = str(e)

        evidence["console"] = console[:40]
        evidence["http_4xx_5xx"] = bad[:40]
        ctx.close()
        b.close()

    with open(f"{args.out}/run-{args.tag}.json", "w", encoding="utf-8") as f:
        json.dump(evidence, f, ensure_ascii=False, indent=2)
    print(json.dumps({k: v for k, v in evidence.items() if k in ("landing_url", "landing", "dashboard")},
                     ensure_ascii=False, indent=2)[:2500])
    print("evidence:", f"{args.out}/run-{args.tag}.json")


if __name__ == "__main__":
    main()

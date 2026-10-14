#!/usr/bin/env python3
"""
R1 用户实测反馈复验（2026-10-14）：三条现象的固件内取证与回归。

① 落地页（Dashboard）不应出现 LuCI 的「保存并应用 / 保存 / 复位」页面操作条
   （只读页；luci.js addFooter 由 handleSave/handleSaveApply/handleReset 是否存在决定）
② 侧边栏命名：Dashboard 中文名不得与基座 状态→概览 撞名
③ 主题 chrome（侧边栏/顶栏/操作条）材质应跟随 /etc/config/quartz 的 panel_opacity
   （--qz-chrome 透明度 = 设计基准 × --qz-panel-alpha）

前置：VM 在跑（QuartzWrt-Builder/scripts/test-in-vm.sh --detach，宿主 8080 / ssh 2222）
用法：python3 dev/vm-probe.py --tag before|after [--opacity 50]
输出：dev/shots/vm-r1/<tag>-{dark,light}.png + run-<tag>.json
"""
import argparse, json, os, subprocess, sys
from playwright.sync_api import sync_playwright

BASE = "http://127.0.0.1:8080"

PROBE = """() => {
    const cs = (sel) => {
        const el = document.querySelector(sel);
        return el ? getComputedStyle(el).backgroundColor : null;
    };
    const rootStyle = getComputedStyle(document.documentElement);
    const tok = (n) => rootStyle.getPropertyValue(n).trim();
    const navItems = Array.from(document.querySelectorAll('#qz-nav .qz-nav-item'))
        .map(li => (li.querySelector('a') || {}).textContent.trim());
    const navAll = Array.from(document.querySelectorAll('#qz-nav a'))
        .map(a => a.textContent.trim()).filter(Boolean);
    const actions = document.querySelector('.cbi-page-actions');
    return {
        url: location.href,
        title: document.title,
        pageActionBtns: actions
            ? Array.from(actions.querySelectorAll('button')).map(b => b.textContent.trim())
            : null,
        indicators: (document.getElementById('indicators') || {}).innerHTML || '',
        navTop: navItems,
        navAll: navAll,
        overviewEntries: navAll.filter(t => t === '概览').length,
        hero: !!document.querySelector('.qzd-hero'),
        tokens: {
            panel_alpha: tok('--qz-panel-alpha'),
            blur: tok('--qz-blur'),
            chrome: tok('--qz-chrome'),
        },
        bg: {
            sidebar: cs('#qz-sidebar'),
            header: cs('header'),
            panel: cs('.cbi-map > .cbi-section') || cs('.cbi-map'),
            body: cs('body'),
        },
        translucentAttr: document.documentElement.hasAttribute('data-qz-translucent'),
        darkmode: document.documentElement.getAttribute('data-darkmode'),
    };
}"""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--password", default="quartz")
    ap.add_argument("--out", default="shots/vm-r1")
    ap.add_argument("--tag", required=True)
    ap.add_argument("--opacity", default=None, help="设置 /etc/config/quartz panel_opacity 后复测")
    ap.add_argument("--locale", default="zh-CN")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    if args.opacity is not None:
        subprocess.run(
            ["ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no",
             "-p", "2222", "root@127.0.0.1",
             f"uci set quartz.theme.panel_opacity='{args.opacity}' && uci commit quartz && echo set-ok"],
            check=True)

    ev = {"tag": args.tag, "base": BASE, "panel_opacity": args.opacity}
    console, bad = [], []

    with sync_playwright() as p:
        b = p.chromium.launch()
        ctx = b.new_context(viewport={"width": 1500, "height": 1000}, locale=args.locale)
        pg = ctx.new_page()
        pg.on("console", lambda m: console.append(f"{m.type}: {m.text}") if m.type in ("error", "warning") else None)
        pg.on("response", lambda r: bad.append(f"{r.status} {r.url}") if r.status >= 400 else None)

        pg.goto(f"{BASE}/cgi-bin/luci/", wait_until="load")
        pg.wait_for_timeout(1500)
        try:
            if pg.locator('input[name="luci_username"]').count():
                pg.fill('input[name="luci_username"]', "root")
            pg.fill('input[name="luci_password"]', args.password)
            pg.keyboard.press("Enter")
        except Exception as e:
            ev["login_error"] = str(e)
        pg.wait_for_load_state("load")
        pg.wait_for_timeout(3000)

        ev["landing"] = pg.evaluate(PROBE)
        for mode in ("dark", "light"):
            pg.evaluate("""(m) => { const r = document.documentElement;
                r.setAttribute('data-darkmode', m === 'dark' ? 'true' : 'false');
                r.setAttribute('data-qz-mode', m); }""", mode)
            pg.wait_for_timeout(500)
            pg.screenshot(path=f"{args.out}/{args.tag}-{mode}.png", full_page=True)

        # 对照页：基座 状态→概览（仍有表单页脚，且其命名是"概览"的合法持有者）
        pg.goto(f"{BASE}/cgi-bin/luci/admin/status/overview", wait_until="load")
        pg.wait_for_timeout(2500)
        ev["overview_page"] = pg.evaluate(PROBE)

        ev["console"] = console[:40]
        ev["http_4xx_5xx"] = bad[:40]
        ctx.close()
        b.close()

    with open(f"{args.out}/run-{args.tag}.json", "w", encoding="utf-8") as f:
        json.dump(ev, f, ensure_ascii=False, indent=2)

    L = ev["landing"]
    print(json.dumps({
        "title": L["title"], "pageActionBtns": L["pageActionBtns"],
        "navTop": L["navTop"], "overviewEntries": L["overviewEntries"],
        "tokens": L["tokens"], "bg": L["bg"], "translucent": L["translucentAttr"],
        "indicators": L["indicators"][:200],
        "overview_page": {"title": ev["overview_page"]["title"],
                          "btns": ev["overview_page"]["pageActionBtns"],
                          "bg": ev["overview_page"]["bg"]},
    }, ensure_ascii=False, indent=2))
    print("evidence:", f"{args.out}/run-{args.tag}.json")


if __name__ == "__main__":
    main()

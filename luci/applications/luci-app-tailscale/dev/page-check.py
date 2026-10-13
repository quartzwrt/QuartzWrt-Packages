#!/usr/bin/env python3
"""luci-app-tailscale（WP-10）VM 页面检查（Playwright + VM 预览台）。

前置：VM 已启动（QuartzWrt-Builder/scripts/test-in-vm.sh --detach；宿主 8080→VM:80）；
      包已安装（运行时 apk 安装或随镜像）；root 密码默认 quartz。
用法：python3 dev/page-check.py [--password quartz] [--mode dark|light]
输出：dev/shots/vm/<id>.png + run-log.jsonl（标题/视图文本长度/控制台错误/4xx）
"""
import argparse, json, os, sys, time
from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "shots", "vm")
BASE = "http://127.0.0.1:8080"

PAGES = [
    ("t01_tailscale-setting", "/cgi-bin/luci/admin/vpn/tailscale/setting", "Tailscale · 全局设置", True),
    ("t02_tailscale-interface", "/cgi-bin/luci/admin/vpn/tailscale/interface", "Tailscale · 接口信息", True),
    ("t03_tailscale-log", "/cgi-bin/luci/admin/vpn/tailscale/log", "Tailscale · 日志", True),
    ("t04_menu-json", "/cgi-bin/luci/admin/menu", "菜单树（VPN 组存在性，断言）", False),
]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--password", default="quartz")
    ap.add_argument("--mode", default="dark", choices=["dark", "light"])
    args = ap.parse_args()
    os.makedirs(OUT, exist_ok=True)
    log = open(os.path.join(OUT, "run-log.jsonl"), "a", encoding="utf-8")

    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx = browser.new_context(
            viewport={"width": 1500, "height": 1000},
            locale="zh-CN",
            extra_http_headers={"Accept-Language": "zh-CN,zh;q=0.9"},
        )
        pg = ctx.new_page()
        errors = []
        pg.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        bad = []
        pg.on("response", lambda r: bad.append(f"{r.status} {r.url}") if r.status >= 400 else None)

        # 登录（表单路径，同真实用户）
        pg.goto(BASE + "/cgi-bin/luci/", wait_until="domcontentloaded", timeout=20000)
        pg.wait_for_selector("#luci_password", timeout=15000)
        pg.fill("#luci_username", "root")
        pg.fill("#luci_password", args.password)
        pg.click("button.cbi-button-positive.important")
        pg.wait_for_selector("#qz-sidebar", timeout=15000)
        # 暗色固定（主题持久化在 localStorage）
        pg.evaluate("(m) => document.documentElement.setAttribute('data-darkmode', m === 'dark' ? 'true' : 'false')", args.mode)

        rc = 0
        for pid, path, title, need_view in PAGES:
            errors.clear(); bad.clear()
            t0 = time.time()
            pg.goto(BASE + path, wait_until="domcontentloaded", timeout=30000)
            if pid == "t04_menu-json":
                menu = pg.evaluate("async () => { const r = await fetch('/cgi-bin/luci/admin/menu', {headers:{'Accept':'application/json'}}); return await r.json(); }")

                def children(node):
                    return (node or {}).get('children', {}) or {}

                # 结构：root -> children -> admin -> children -> vpn -> children -> tailscale
                admin = children(menu).get('admin')
                ts = children(children(admin).get('vpn')).get('tailscale')
                subs = children(ts)
                assert ts is not None, 'menu: admin/vpn/tailscale missing'
                for k in ('setting', 'interface', 'log'):
                    assert k in subs, f'menu child missing: {k}'
                print(f'    menu ok: vpn/tailscale -> {sorted(subs)}')
            if need_view:
                try:
                    pg.wait_for_function(
                        """() => { const v = document.querySelector('#view');
                           if (!v) return false;
                           const t = v.textContent.replace(/\\s+/g,' ').trim();
                           return t.length > 40 && !/正在载入视图|Loading view/.test(t); }""",
                        timeout=20000)
                except Exception as e:
                    print(f"FAIL {pid}: 视图未就绪 ({e})", file=sys.stderr); rc = 1
            pg.wait_for_timeout(1200)
            # LuCI 内部滚动容器：截图前回顶，避免截到滚动中段
            pg.evaluate("() => { window.scrollTo(0,0); document.querySelector('#maincontent')?.scrollTo(0,0); document.querySelector('#view')?.scrollTo(0,0); }")
            pg.wait_for_timeout(300)
            shot = os.path.join(OUT, pid + ".png")
            pg.screenshot(path=shot, full_page=True)
            vinfo = pg.evaluate("""() => { const v = document.querySelector('#view');
                if (!v) return {len: -1, snippet: ''};
                const t = v.textContent.replace(/\\s+/g,' ').trim();
                return {len: t.length, snippet: t.slice(0, 120)}; }""")
            rec = {"id": pid, "title": title, "path": path, "elapsed": round(time.time() - t0, 1),
                   "view_len": vinfo["len"], "snippet": vinfo["snippet"],
                   "console_errors": errors[:5], "http_errors": bad[:5], "mode": args.mode}
            log.write(json.dumps(rec, ensure_ascii=False) + "\n"); log.flush()
            print(f"{pid}: view_len={vinfo['len']} errors={len(errors)} http>=400={len(bad)} -> {shot}")
            if errors or bad:
                print(f"    console: {errors[:3]}\n    http: {bad[:3]}")
        browser.close()
    return rc


if __name__ == "__main__":
    sys.exit(main())

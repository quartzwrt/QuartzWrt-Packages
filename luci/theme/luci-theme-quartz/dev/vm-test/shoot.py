#!/usr/bin/env python3
"""WP-2 逐页主题截图（Playwright + VM 预览台）

前置:
  - VM 已启动（scripts/test-in-vm.sh --detach，宿主 8080 → VM:80）
  - python3 -m pip install --user --break-system-packages playwright
  - python3 -m playwright install chromium

用法:
  python3 shoot.py --mode dark            # 全量暗色（默认输出 ../shots/vm-test-dark）
  python3 shoot.py --mode light
  python3 shoot.py --mode dark --only v01_login
  python3 shoot.py --mode dark --from v30_network-interfaces
  python3 shoot.py --mode dark --flat-actions     # 全页截图前解除贴底操作条 sticky（长图不再中段悬停）
"""
import argparse, json, os, re, sys, time
from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = "http://127.0.0.1:8080"
READY_JS = """() => {
  const v = document.querySelector('#view');
  if (!v) return true;
  const t = v.textContent.replace(/\\s+/g, ' ').trim();
  return !/正在载入视图|Loading view/i.test(t);
}"""
VIEW_JS = """() => {
  const v = document.querySelector('#view');
  if (!v) return {len: -1, kids: -1};
  const t = v.textContent.replace(/\\s+/g, ' ').trim();
  return {len: t.length, kids: v.children.length, snippet: t.slice(0, 80)};
}"""


def login(ctx, password=""):
    """表单登录（真实用户路径）；失败回退 fetch 方式。返回 (page, note)"""
    pg = ctx.new_page()
    try:
        pg.goto(BASE + "/cgi-bin/luci/", wait_until="domcontentloaded", timeout=20000)
        pg.wait_for_selector("#luci_password", timeout=15000)
        pg.fill("#luci_username", "root")
        pg.fill("#luci_password", password)
        pg.click("button.cbi-button-positive.important")
        pg.wait_for_selector("#qz-sidebar", timeout=10000)
        return pg, "form"
    except Exception as e:
        try:
            pg.goto(BASE + "/cgi-bin/luci/", wait_until="domcontentloaded", timeout=20000)
            pg.evaluate("""(pw) => fetch('/cgi-bin/luci/', {method:'POST',
                headers:{'Content-Type':'application/x-www-form-urlencoded'},
                body:'luci_username=root&luci_password=' + encodeURIComponent(pw), credentials:'same-origin'})""", password)
            pg.wait_for_timeout(400)
            pg.goto(BASE + "/cgi-bin/luci/admin/status/overview", wait_until="domcontentloaded", timeout=20000)
            pg.wait_for_selector("#qz-sidebar", timeout=10000)
            return pg, "fetch-fallback: " + str(e)[:80]
        except Exception as e2:
            return pg, "LOGIN_FAILED: " + str(e2)[:120]


def shoot(ctx, page, item, mode, outdir, settle_ms, ready_timeout, flat_actions=False):
    name = item["id"]
    cons, fails, bad = [], [], []
    page.on("console", lambda m: cons.append(f"{m.type}: {m.text[:180]}") if m.type == "error" else None)
    page.on("pageerror", lambda e: cons.append("pageerror: " + str(e)[:180]))
    page.on("requestfailed", lambda r: fails.append(f"{r.url.split('//')[-1][:90]} {str(r.failure)[:60]}"))
    page.on("response", lambda r: bad.append(f"{r.status} {r.url.split('//')[-1][:100]}") if r.status >= 400 else None)

    rec = {"id": name, "title": item.get("title"), "path": item["path"], "mode": mode}
    t0 = time.time()
    try:
        page.goto(BASE + item["path"], wait_until="domcontentloaded", timeout=30000)
    except Exception as e:
        rec["error"] = "goto: " + str(e)[:100]
    try:
        page.wait_for_function(READY_JS, timeout=ready_timeout, polling=250)
    except Exception:
        rec["stuck"] = True
    page.wait_for_timeout(settle_ms)
    try:
        rec["h"] = page.evaluate("Math.max(document.documentElement.scrollHeight, document.body.scrollHeight)")
        rec["title_page"] = page.title()
        rec["view"] = page.evaluate(VIEW_JS)
    except Exception as e:
        rec["probe_err"] = str(e)[:80]
    png = os.path.join(outdir, name + ".png")
    try:
        # --flat-actions：全页截图是"整份文档"的拼接图，而贴底操作条（position: sticky）
        # 是视口相对的 chrome——直接截会把它定格在"视口底部"位置（长图里看起来悬在页面
        # 中部）。截图前解除 sticky，操作条回到文档流内的真实位置（= 用户滚到页底时看到
        # 的位置），长图因此不再有该假象（视口截图不受影响）。
        if flat_actions:
            page.add_style_tag(content=".cbi-page-actions{position:static !important}")
        page.screenshot(path=png, full_page=True, animations="disabled")
    except Exception as e:
        rec["error"] = "shot: " + str(e)[:100]
    rec["ms"] = int((time.time() - t0) * 1000)
    if cons:
        rec["console"] = cons[:4]
    if fails:
        rec["reqfail"] = fails[:4]
    if bad:
        # 预期的噪音：/quartz-wallpaper/wallpaper.jpg 探测 404（无壁纸时），登录页 403
        rec["http4xx"] = [x for x in bad if "quartz-wallpaper" not in x][:6]
    return rec


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mode", choices=["dark", "light"], required=True)
    ap.add_argument("--pages", default=os.path.join(HERE, "pages.json"))
    ap.add_argument("--outdir", default=None)
    ap.add_argument("--only", default=None)
    ap.add_argument("--from", dest="from_id", default=None)
    ap.add_argument("--settle-ms", type=int, default=800)
    ap.add_argument("--ready-timeout", type=int, default=25000)
    ap.add_argument("--password", default="")
    ap.add_argument("--width", type=int, default=1500)
    ap.add_argument("--height", type=int, default=1000)
    ap.add_argument("--flat-actions", action="store_true",
                    help="全页截图前把贴底操作条置为 position: static（避免长图中段悬停假象）")
    args = ap.parse_args()

    pages = json.load(open(args.pages))
    if args.only:
        pages = [p for p in pages if p["id"] == args.only]
    if args.from_id:
        ids = [p["id"] for p in pages]
        pages = pages[ids.index(args.from_id):]
    if not pages:
        print("无匹配页面"); sys.exit(1)

    theme_dir = os.path.abspath(os.path.join(HERE, ".."))
    outdir = args.outdir or os.path.join(theme_dir, "shots", f"vm-test-{args.mode}")
    os.makedirs(outdir, exist_ok=True)
    logpath = os.path.join(outdir, "run-log.jsonl")
    done = set()
    if os.path.exists(logpath):
        for line in open(logpath):
            try:
                r = json.loads(line)
                if r.get("id") and not r.get("error"):
                    done.add(r["id"])
            except Exception:
                pass
    fh = open(logpath, "a")

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, args=["--hide-scrollbars", "--disable-gpu"])
        # 登录页专用上下文（无会话）；先落 localStorage 再截图，保证双模正确
        login_item = [x for x in pages if x.get("login") is False]
        for item in login_item:
            ctx0 = browser.new_context(locale="zh-CN", viewport={"width": args.width, "height": args.height})
            pg0 = ctx0.new_page()
            pg0.goto(BASE + "/cgi-bin/luci/", wait_until="domcontentloaded", timeout=20000)
            pg0.evaluate(f"() => localStorage.setItem('qz-theme', '{args.mode}')")
            rec = shoot(ctx0, pg0, item, args.mode, outdir, args.settle_ms, args.ready_timeout, args.flat_actions)
            fh.write(json.dumps(rec, ensure_ascii=False) + "\n"); fh.flush()
            print(f"{'STUCK ' if rec.get('stuck') else 'OK '}{item['id']} h={rec.get('h')} {rec.get('ms')}ms")
            ctx0.close()

        ctx = browser.new_context(locale="zh-CN", viewport={"width": args.width, "height": args.height})
        pg, lnote = login(ctx, args.password)
        print("login:", lnote)
        pg.evaluate(f"() => localStorage.setItem('qz-theme', '{args.mode}')")
        pg.close()

        rest = [x for x in pages if x.get("login") is not False]
        for i, item in enumerate(rest, 1):
            if item["id"] in done and not args.only:
                print(f"[{i}/{len(rest)}] {item['id']} 已存在，跳过")
                continue
            page = ctx.new_page()
            rec = shoot(ctx, page, item, args.mode, outdir, args.settle_ms, args.ready_timeout, args.flat_actions)
            page.close()
            fh.write(json.dumps(rec, ensure_ascii=False) + "\n"); fh.flush()
            flag = "STUCK " if rec.get("stuck") else ("ERR " if rec.get("error") else "OK ")
            print(f"[{i}/{len(rest)}] {flag}{item['id']} h={rec.get('h')} view_len={(rec.get('view') or {}).get('len')} {rec.get('ms')}ms"
                  + (f" console={len(rec.get('console', []))}" if rec.get("console") else ""))
        browser.close()
    fh.close()


if __name__ == "__main__":
    main()

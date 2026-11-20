#!/usr/bin/env python3
"""luci-app-usb-tether（WP-13）「保存并应用」UI 链路复现/验证（Playwright）。

背景：CLI e2e 全绿但真机「保存并应用」按钮无限转圈——本脚本走真实 UI 路径
（勾选启用 → 保存并应用），记录全部 ubus/CGI 请求时序、控制台错误、按钮状态
变化与 VM 侧最终落盘状态，用于定位 LuCI uci apply/confirm 链的卡点。

前置：VM 已启动（test-in-vm.sh --detach）；dev/vm-e2e.sh 已装包；root 密码 quartz；
      出发状态 enabled=0（脚本自行设定）。
用法：python3 dev/ui-saveapply-test.py [--password quartz]
"""
import argparse, json, os, sys, time
from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = "http://127.0.0.1:8080"
SETTING = "/cgi-bin/luci/admin/network/usb-tether/setting"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--password", default="quartz")
    args = ap.parse_args()

    reqs, errs = [], []

    def on_request(r):
        reqs.append({"t": round(time.time() - T0, 1), "req": r.method + " " + r.url[:120]})

    def on_response(r):
        for e in reqs:
            if e["req"].endswith(r.url[:110]) and "resp" not in e:
                e["resp"] = r.status
                break

    T0 = time.time()
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx = browser.new_context(viewport={"width": 1500, "height": 1000}, locale="zh-CN",
                                  extra_http_headers={"Accept-Language": "zh-CN,zh;q=0.9"})
        pg = ctx.new_page()
        pg.on("request", on_request)
        pg.on("response", on_response)
        pg.on("console", lambda m: errs.append(m.text) if m.type == "error" else None)

        pg.goto(BASE + "/cgi-bin/luci/", wait_until="domcontentloaded", timeout=20000)
        pg.wait_for_selector("#luci_password", timeout=15000)
        pg.fill("#luci_username", "root")
        pg.fill("#luci_password", args.password)
        pg.click("button.cbi-button-positive.important")
        pg.wait_for_selector("#qz-sidebar", timeout=15000)

        pg.goto(BASE + SETTING, wait_until="domcontentloaded", timeout=30000)
        pg.wait_for_selector("#view input[type=checkbox]", timeout=15000)
        flag = pg.locator("#view input[type=checkbox]").first
        print("enabled 初始勾选态:", flag.is_checked())
        if not flag.is_checked():
            flag.check()
            print("已勾选启用")

        # 保存并应用 = 页脚 cbi-dropdown（「保存并应用」/「强制应用」二选）——真实用户路径
        pg.wait_for_selector("div.cbi-page-actions div.cbi-button-apply, .cbi-page-actions div.cbi-button-apply", timeout=10000)
        dd = pg.locator("div.cbi-button-apply")
        print("apply 下拉数:", dd.count())

        t_click = time.time()
        dd.first.click()
        pg.wait_for_timeout(600)
        opts = pg.locator("div.cbi-button-apply li")
        print("选项:", [opts.nth(i).get_attribute("data-value") for i in range(opts.count())])
        pg.locator("div.cbi-button-apply li[data-value='0']").click()
        print("已选择「保存并应用」，观察 35s ...")

        # 轮询 40s：观察按钮 spinner/横幅/通知/模态
        seen = set()
        for i in range(40):
            pg.wait_for_timeout(1000)
            state = pg.evaluate("""() => {
                const b = document.querySelector('.cbi-page-actions .cbi-button-apply, .cbi-page-actions .cbi-button-save.important');
                const spin = document.querySelector('.cbi-page-actions .cbi-button-apply.loading, .cbi-page-actions .spinning, [data-loading=true]');
                const modal = document.querySelector('.modal, #modal_overlay');
                const banner = document.querySelector('.alert-message, .cbi-alert-message');
                return {
                    btnText: b ? b.textContent.trim() : null,
                    btnLoading: b ? (b.disabled || b.getAttribute('data-loading') !== null || b.classList.contains('loading')) : null,
                    spin: !!spin,
                    modal: modal ? modal.textContent.replace(/\\s+/g,' ').slice(0,80) : null,
                    banner: banner ? banner.textContent.replace(/\\s+/g,' ').slice(0,80) : null
                };
            }""")
            key = json.dumps(state, ensure_ascii=False)
            if key not in seen:
                seen.add(key)
                print(f"[{i}s]", state["btnText"], "| loading:", state["btnLoading"], "| modal:", state["modal"], "| banner:", state["banner"])

        # 最终视图文本（含确认横幅与否）
        view = pg.evaluate("() => document.querySelector('#view') ? document.querySelector('#view').textContent.replace(/\\s+/g,' ').slice(0,300) : ''")
        print("视图文本:", view)

        browser.close()

    print("\n=== 请求时序（点击后） ===")
    for e in reqs:
        if e["t"] >= round(t_click - T0, 1) - 1:
            print(f"  +{e['t']:>5}s {e['req']} -> {e.get('resp')}")
    print("=== 控制台错误 ===")
    for e in errs[:10]:
        print(" ", e)
    return 0


if __name__ == "__main__":
    sys.exit(main())

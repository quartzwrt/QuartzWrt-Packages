#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""luci-app-quartz-screen 状态页冒烟测试（开发机 Playwright，非构建依赖）。

用最小 LuCI 运行时桩（E/fs/uci/poll/ui/_）加载 status.js，渲染 DOM 并检查：
  - 状态字段存在（Service/Framebuffer/Current page…）
  - 预览 <img> 得到 data:image/png;base64
  - 按钮 click 处理器可调用（fs.exec test/apply）

不覆盖 form.Map 设置页（需真实 LuCI）。
"""

import json
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

HERE = Path(__file__).resolve()
APP = HERE.parents[1]
VIEW = APP / "htdocs/luci-static/resources/view/quartz-screen/status.js"

STATUS = {
    "running": True,
    "pid": 1234,
    "fb": "/dev/fb0",
    "fb_name": "fb_gc9307",
    "width": 320,
    "height": 172,
    "bpp": 16,
    "page": "overview",
    "pages": ["overview", "network", "cellular", "system"],
    "brightness": 80,
    "night": False,
    "last_render_at": 1760000000,
    "snapshot_ok": True,
    "stale": False,
    "errors": [],
    "config_enabled": True,
    "interval": 8,
}

# 1x1 transparent PNG
PNG_B64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk"
    "YAAAAAYAAjCB0C8AAAAASUVORK5CYII="
)

STUBS = """
<script>
function E(tag, attrs, children) {
  const el = document.createElement(tag);
  attrs = attrs || {};
  for (const k in attrs) {
    const v = attrs[k];
    if (v == null) continue;
    if (k === 'click') el.addEventListener('click', v);
    else if (k === 'class') el.className = v;
    else el.setAttribute(k, v);
  }
  function add(c) {
    if (c == null || c === false) return;
    if (Array.isArray(c)) { c.forEach(add); return; }
    el.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  add(children);
  return el;
}
window.E = E;
window._ = function(s) { return s; };
window.view = { extend: function(o) { return o; } };
window.poll = { add: function() {} };
window.ui = {
  addNotification: function() {},
  createHandlerFn: function(ctx, fn) { return fn; }
};
window.uci = { load: function() { return Promise.resolve(); }, get: function() { return null; } };
window.fs = {
  exec: function(path, args) {
    if (args && args[0] === 'status')
      return Promise.resolve({ code: 0, stdout: JSON.stringify(STATUS), stderr: '' });
    if (args && args[0] === 'preview')
      return Promise.resolve({ code: 0, stdout: PNG_B64 + '\\n', stderr: '' });
    if (args && (args[0] === 'test' || args[0] === 'apply'))
      return Promise.resolve({ code: 0, stdout: '', stderr: '' });
    return Promise.resolve({ code: 1, stdout: '', stderr: 'unexpected ' + args });
  }
};
</script>
"""


def main():
    js = VIEW.read_text()
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, args=["--no-sandbox"])
        page = browser.new_page(viewport={"width": 1100, "height": 900})
        stubs = STUBS.replace(
            "window.E = E;", "window.STATUS = " + json.dumps(STATUS) + "; window.PNG_B64 = " + json.dumps(PNG_B64) + "; window.E = E;"
        )
        page.set_content(
            "<!doctype html><meta charset=utf-8><body><div id=maincontent></div></body>"
            + stubs
        )
        errors = []
        page.on("pageerror", lambda e: errors.append(str(e)))
        page.evaluate(
            """(src) => {
              const mod = new Function('view','fs','uci','poll','ui','E','_', src);
              const v = mod(view, fs, uci, poll, ui, E, _);
              return v.load().then(function(data) {
                const root = v.render(data);
                document.getElementById('maincontent').appendChild(root);
                return new Promise(function(r) { setTimeout(r, 300); });
              });
            }""",
            js,
        )
        page.wait_for_timeout(400)
        text = page.inner_text("#maincontent")
        imgs = page.eval_on_selector_all(
            "#maincontent img", "els => els.map(e => e.src)"
        )
        buttons = page.eval_on_selector_all(
            "#maincontent button", "els => els.map(e => e.textContent)"
        )
        page.screenshot(path=str(APP / "dev" / "status-smoke.png"), full_page=True)
        browser.close()

    print("text:", text.replace("\n", " | ")[:400])
    print("images:", len(imgs), "buttons:", buttons)
    if errors:
        print("PAGE ERRORS:", errors)
        sys.exit(1)
    for want in ["Status", "Running", "Framebuffer", "Current page", "Live preview"]:
        if want not in text:
            print("MISSING:", want)
            sys.exit(1)
    if len(imgs) != 4 or not all(s.startswith("data:image/png;base64,") for s in imgs):
        print("preview images bad")
        sys.exit(1)
    print("STATUS SMOKE PASS")


if __name__ == "__main__":
    main()

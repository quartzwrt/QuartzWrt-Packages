#!/usr/bin/env python3
"""quartz-app-api 验收用「可切换测试软件源」HTTP 服务。

模拟真实升级路径：同一仓库 URL 下由上游发布新版本——
- GET /x86_64/packages.adb 等静态文件：当前 stage 目录内容；
- GET /swap/stage1|stage2：切换当前 stage（返回 200 + 当前 stage 名），
  供验收脚本在「安装旧版 → 上游出新版 → 升级」链路上驱动状态。

用法:
    python3 dev/testrepo/serve.py [--port 9990] [--bind 0.0.0.0]

目录约定（与仓库内 dev/testrepo/ 一致）：
    testrepo/qz-test.pem          测试签名公钥（VM 侧装到 /etc/apk/keys/）
    testrepo/stage1/…             quartz-pkgtest 1.0.0-r1（旧）
    testrepo/stage2/…             quartz-pkgtest 1.0.0-r1 + 1.0.1-r1（新）

VM 侧仓库地址: http://<宿主IP>:9990/x86_64/packages.adb（QEMU 用户网络里宿主 = 192.168.1.2 / 10.0.2.2）。
"""
import argparse
import http.server
import os
import sys

BASE = os.path.dirname(os.path.abspath(__file__))
STAGES = ("stage1", "stage2")
state = {"stage": "stage1"}


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        # 根目录下的共享文件（如 qz-test.pem）优先按 BASE 提供；
        # 其余（/x86_64/…）按当前 stage 解析（/swap 切换后立即生效，连接复用不受影响）。
        rel = path.split("?", 1)[0].split("#", 1)[0].lstrip("/")
        if rel and os.path.isfile(os.path.join(BASE, rel)):
            self.directory = BASE
        else:
            self.directory = os.path.join(BASE, state["stage"])
        return super().translate_path(path)

    def do_GET(self):  # noqa: N802
        if self.path.startswith("/swap/"):
            want = self.path[len("/swap/"):].split("?")[0]
            if want not in STAGES:
                self.send_error(400, "unknown stage")
                return
            state["stage"] = want
            body = want.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        super().do_GET()

    def log_message(self, fmt, *args):
        sys.stderr.write("testrepo: " + fmt % args + "\n")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9990)
    ap.add_argument("--bind", default="0.0.0.0")
    args = ap.parse_args()
    srv = http.server.ThreadingHTTPServer((args.bind, args.port), Handler)
    sys.stderr.write(f"testrepo serving {BASE} on {args.bind}:{args.port} (stage={state['stage']})\n")
    srv.serve_forever()


if __name__ == "__main__":
    main()

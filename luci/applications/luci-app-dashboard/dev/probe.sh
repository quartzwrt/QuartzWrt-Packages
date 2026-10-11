#!/bin/sh
# luci-app-dashboard 预览截图（视觉回归基线，WP-3）
# 前置：dev/serve.sh 已在本机 8898 端口服务；Windows Edge 经 WSL 互操作（同 WP-2 spike 做法）
set -e
cd "$(dirname "$0")"

EDGE="/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe"
BASE="http://localhost:8898/applications/luci-app-dashboard/dev/preview.html"
CB="cb=$(date +%s)"

# 每次全新浏览器 profile：python http.server 无 Cache-Control，避免启发式缓存喂旧文件
rm -rf /mnt/c/temp/qz-edge-dash-* 2>/dev/null || true
mkdir -p shots

shot() {
	name="$1"; size="$2"; qs="$3"
	"$EDGE" --headless=new --disable-gpu --hide-scrollbars --no-first-run --no-default-browser-check \
		--user-data-dir="C:\\temp\\qz-edge-dash-$name" --window-size="$size" \
		--virtual-time-budget=5000 --screenshot="C:\\temp\\qz-dash-$name.png" \
		"$BASE?$qs&$CB" >/dev/null 2>&1 || true
	cp "/mnt/c/temp/qz-dash-$name.png" "shots/$name.png"
	echo "  shots/$name.png"
}

echo "截图输出："
shot dashboard-dark      1440,1180 "theme=dark&state=normal&live=0"
shot dashboard-light     1440,1180 "theme=light&state=normal&live=0"
shot dashboard-partial   1440,1180 "theme=dark&state=partial&live=0"
shot dashboard-down      1440,1180 "theme=dark&state=down&live=0"
shot dashboard-wallpaper 1440,1180 "theme=dark&state=normal&live=0&wallpaper=1&alpha=85"
# 窄屏：Windows Edge 无头窗口有最小宽度（实测 CSS 视口 ≈477px 起），
# 520 宽可完整捕获 ≤699px 移动端规则（单列、隐藏 MAC 列、Hero 摘要换行）；真机手机宽度留待固件内验收
shot dashboard-mobile    520,1600  "theme=dark&state=normal&live=0"

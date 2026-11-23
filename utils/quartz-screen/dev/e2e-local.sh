#!/bin/sh
# quartz-screen 本地自检（无需真机屏）：字体/页面/预览/假 framebuffer 守护。
# 用法：dev/e2e-local.sh [build-binary]
#   默认用 `go build` 生成临时二进制；也可传入已构建的二进制路径。
set -eu

PKG_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
SRC="$PKG_DIR/src"
GO=${GO:-go}
BIN=${1:-}
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

if [ -z "$BIN" ]; then
	BIN="$WORK/quartz-screen"
	(cd "$SRC" && GOFLAGS=-mod=mod "$GO" build -o "$BIN" ./cmd/quartz-screen)
fi

echo "== 1. Go 测试"
(cd "$SRC" && GOFLAGS=-mod=mod "$GO" test ./...)

echo "== 2. 预览（暗/浅 + base64）"
"$BIN" preview --demo --page overview --theme dark --scale 2 --out "$WORK/ov-dark.png"
"$BIN" preview --demo --page overview --theme light --format base64 > "$WORK/ov-light.b64"
head -c 8 "$WORK/ov-dark.png" | od -An -tx1 | grep -q '89 50 4e 47' || {
	echo "PNG signature missing"; exit 1;
}
[ -s "$WORK/ov-light.b64" ] || { echo "base64 preview empty"; exit 1; }

echo "== 3. 全部页面设计稿"
"$BIN" render --all --out "$WORK/mockups" --scale 2 --demo >/dev/null
count=$(find "$WORK/mockups" -name '*.png' | wc -l)
[ "$count" -eq 14 ] || { echo "expected 14 mockups, got $count"; exit 1; }

echo "== 4. 假 framebuffer 守护（4s）"
mkdir -p "$WORK/run"
QUARTZ_SCREEN_RUN_DIR="$WORK/run" timeout -s TERM 4 "$BIN" run --demo \
	--fb-file "$WORK/run/fb.raw" --status-file /nonexistent >"$WORK/run/log" 2>&1 || true
[ -f "$WORK/run/fb.raw" ] || { echo "fake fb missing"; exit 1; }
size=$(wc -c < "$WORK/run/fb.raw")
[ "$size" -eq $((320 * 172 * 2)) ] || { echo "fb size $size"; exit 1; }
grep -q '"running": true' "$WORK/run/quartz-screen.json" || {
	echo "status file not running"; cat "$WORK/run/quartz-screen.json"; exit 1;
}

echo "== 5. 状态命令"
QUARTZ_SCREEN_RUN_DIR="$WORK/run" "$BIN" status | grep -q '"pages"' || {
	echo "status missing pages"; exit 1;
}

echo "ALL PASS"

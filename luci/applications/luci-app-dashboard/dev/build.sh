#!/bin/sh
# luci-app-dashboard 构建（WP-3 / W3.2）
#   src/  → htdocs/luci-static/resources/quartz-dashboard/{app.js,app.css}（产物提交入库）
#   并回写壳层的 BUILD 常量（缓存破坏；配合 L.env.resource_version）
# 前置：node ≥ 20 + corepack（pnpm 由 corepack 按需缓存，无需全局安装）
set -e
cd "$(dirname "$0")"

SRC=../src
OUT=../htdocs/luci-static/resources/quartz-dashboard
SHELL_JS=../htdocs/luci-static/resources/view/quartz/dashboard.js

JS_BUDGET=51200     # 50KB gzip
CSS_BUDGET=12288    # 12KB gzip

cd "$SRC"
if [ ! -d node_modules ]; then
	echo "== 安装依赖（corepack pnpm）=="
	corepack pnpm install --frozen-lockfile 2>/dev/null || corepack pnpm install
fi

echo "== 构建 =="
corepack pnpm run build
cd - >/dev/null

# 产物名归一（不同 Vite 版本 CSS 输出名有差异）
for f in "$OUT"/*.css; do
	[ -e "$f" ] || continue
	[ "$(basename "$f")" = "app.css" ] || mv "$f" "$OUT/app.css"
done

# 体积断言（gzip -9，硬预算见 docs/DESIGN.md §7）
js_gz=$(gzip -9c "$OUT/app.js" | wc -c)
css_gz=$(gzip -9c "$OUT/app.css" | wc -c)
printf 'app.js  %sB gzip（预算 %sB）\n' "$js_gz" "$JS_BUDGET"
printf 'app.css %sB gzip（预算 %sB）\n' "$css_gz" "$CSS_BUDGET"
[ "$js_gz" -le "$JS_BUDGET" ] || { echo "✗ app.js 超出体积预算" >&2; exit 1; }
[ "$css_gz" -le "$CSS_BUDGET" ] || { echo "✗ app.css 超出体积预算" >&2; exit 1; }

# 壳层 BUILD 常量回写（内容哈希前 8 位）
HASH=$(sha256sum "$OUT/app.js" | cut -c1-8)
sed -i "s/^var BUILD = '.*';/var BUILD = '$HASH';/" "$SHELL_JS"
echo "== 完成：壳层 BUILD → $HASH =="

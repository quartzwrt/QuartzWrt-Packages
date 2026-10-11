#!/bin/sh
# Tailwind 隔离 spike 构建脚本：
#   out/chosen.gen.css        待测方案 A（qz: 前缀 + 完整导入 + source(none) 精确扫描）
#   out/control.gen.css       对照组（无前缀 + 完整导入），验证测试台可检出污染
#   out/cascade.unlayered.css cascade.css 的“无层对照版”（剥掉 @layer 包裹），
#                             用于矩阵中演示：主题不入层时 Tailwind utilities 会被压制
#                             （真实 cascade.css 自本次起已是入层版本，层序 theme<base<qz-theme<components<utilities）
#
# 依赖：curl（首次运行会下载 Tailwind standalone CLI 到 .bin/，约 110MB）
set -e
cd "$(dirname "$0")"

BIN=.bin/tailwindcss
if [ ! -x "$BIN" ]; then
	mkdir -p .bin
	echo "下载 Tailwind v4.3.3 standalone CLI ..."
	curl -fL --retry 3 -o "$BIN" \
		"https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-x64"
	chmod +x "$BIN"
fi

mkdir -p out
"$BIN" build -i src/chosen.css -o out/chosen.gen.css
"$BIN" build -i src/control.css -o out/control.gen.css

# 生成 cascade.css 的无层对照版
python3 - <<'PY'
import pathlib

src = pathlib.Path('../../../htdocs/luci-static/quartz/cascade.css')
out = pathlib.Path('out/cascade.unlayered.css')

text = src.read_text(encoding='utf-8')
decl = '@layer theme, base, qz-theme, components, utilities;\n'
opener = '@layer qz-theme {\n'
closer = '} /* @layer qz-theme */'

for needle in (decl, opener, closer):
    assert needle in text, f'cascade.css 缺少层标记: {needle!r}（结构可能已变化）'

text = (text.replace(decl, '', 1)
            .replace(opener, '', 1)
            .replace(closer, '', 1))
out.write_text(text, encoding='utf-8')
print(f'cascade.unlayered.css: {out.stat().st_size} bytes')
PY

echo ""
echo "--- 产物断言 ---"
printf 'chosen  体积: %s bytes\n' "$(wc -c < out/chosen.gen.css)"
printf 'control 体积: %s bytes\n' "$(wc -c < out/control.gen.css)"
printf 'chosen  含 qz: 前缀选择器: %s 处（期望 >0）\n' "$(grep -o '\.qz\\:' out/chosen.gen.css | wc -l)"
printf 'chosen  含无前缀 .container 选择器: %s 处（期望 0）\n' "$(grep -c '\.container' out/chosen.gen.css || true)"
printf 'chosen  含 preflight(base 层): %s 处（期望 >0，靠层序与中和压制）\n' "$(grep -o 'box-sizing' out/chosen.gen.css | wc -l || true)"
printf 'control 含无前缀 .container 选择器: %s 处（期望 >0）\n' "$(grep -o '\.container' out/control.gen.css | wc -l || true)"

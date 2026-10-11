#!/bin/sh
# Tailwind 隔离 spike 测试矩阵（需 8899 端口已服务包根目录；Windows Edge 经 WSL 互操作）
set -e
cd "$(dirname "$0")"
EDGE="/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe"
BASE="http://localhost:8899/dev/spike/tailwind/test.html"

# 每次运行使用全新浏览器 profile：避免启发式缓存喂旧 CSS（本测试页另有 URL 缓存破坏参数兜底）
rm -rf /mnt/c/temp/qz-edge-* 2>/dev/null || true

run() {
	name="$1"; url="$2"
	"$EDGE" --headless=new --disable-gpu --hide-scrollbars --no-first-run --no-default-browser-check \
		--user-data-dir="C:\\temp\\qz-edge-$name" --virtual-time-budget=9000 --dump-dom "$url" 2>/dev/null \
		| grep -o 'SPIKE_RESULT [^<]*' | head -1 | sed 's/^SPIKE_RESULT //' > "out/result-$name.json"
	python3 - "$name" <<'PY'
import json, sys
name = sys.argv[1]
raw = open(f'out/result-{name}.json').read().strip()
d = json.loads(raw)
print(f"== {name} ==")
print(f"  inject={d['inject']} cascade={d['cascade']}")
print(f"  verdict={d['verdict']} diffCount={d['diffCount']} utilityWins={d['utilityWins']}")
print(f"  probe: {d['probeBefore']} -> {d['probeAfter']}")
for x in d['diffs'][:8]:
	print(f"    - {x['sel']} {x['prop']}: {x['before']} -> {x['after']}")
PY
}

run sanity "$BASE"
run control-unlayered "$BASE?inject=out/control.gen.css"
run chosen-unlayered "$BASE?cascade=out/cascade.unlayered.css&inject=out/chosen.gen.css"
run chosen-layered "$BASE?inject=out/chosen.gen.css"

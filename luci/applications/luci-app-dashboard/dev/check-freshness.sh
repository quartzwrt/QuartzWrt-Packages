#!/bin/sh
# 产物新鲜度门禁（DESIGN.md §4「提交产物 + 重建校验」的本地版）
#   CI 目前为手动触发（用户拍板），故把重建 diff 门禁做成本地脚本：
#   重建产物后若与已提交版本不一致 → 说明源码改了而产物没提交（或反之），报错退出。
# 用法：提交前跑一次；将来 CI 恢复自动构建时，可直接作为 job 复用（npm ci && 本脚本）
set -e
cd "$(dirname "$0")"

echo "== 重建以校验产物新鲜度 =="
./build.sh

if [ -n "$(git status --porcelain -- ../root/www/luci-static/resources ../frontend/pnpm-lock.yaml)" ]; then
	echo "✗ 产物/锁文件与已提交版本不一致——请把重建结果一并提交（源码与产物必须同源）" >&2
	git status --short -- ../root/www/luci-static/resources ../frontend/pnpm-lock.yaml >&2
	exit 1
fi

echo "✓ 产物新鲜：源码与提交的产物一致"

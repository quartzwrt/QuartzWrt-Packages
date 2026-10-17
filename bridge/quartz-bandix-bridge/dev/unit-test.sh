#!/usr/bin/env bash
# quartz-bandix-bridge 宿主单测（WP-8 · dev 工具，不进安装包）
#
# 目的：在没有路由器的前提下验证契约逻辑——方法表形状、动作面收敛（不暴露
#       installUpdate 等）、入参白名单校验、上游两形态归一、错误映射、参数转发。
#
# 原理：用锁定基座**目标架构的 ucode**（musl 二进制，可在宿主用 musl loader 直跑）
#       执行"bridge 源码 + 测试体"的拼接产物；`ubus` 与 `log` 两个 import 在拼接时
#       替换为桩（dev/stub.uc + fixture.json 提供上游应答，dev/tests.uc 为断言体）。
#       真机侧（rpcd 装载、真 ubus、真 luci.bandix）由 VM 验收覆盖，本测不含传输层。
#
# 用法：
#   dev/unit-test.sh                     # 自动探测构建树
#   BUILD_TREE=/path/to/immortalwrt dev/unit-test.sh
#   UCODE=/path/ucode UCODE_LD=/path/ld-musl-x86_64.so.1 UCODE_LIBPATH=dir:dir dev/unit-test.sh
#
# 退出码：0 = 全绿；非 0 = 有用例失败或环境不可用。

set -euo pipefail

PKG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRIDGE_UC="$PKG_DIR/files/usr/share/rpcd/ucode/quartz-bandix-bridge.uc"
BUILD_TREE="${BUILD_TREE:-/home/nrxboat/quartzwrt/build/immortalwrt}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [ -z "${UCODE:-}" ] || [ -z "${UCODE_LD:-}" ]; then
  [ -d "$BUILD_TREE" ] || { echo "FAIL: 找不到构建树（BUILD_TREE=$BUILD_TREE）" >&2; exit 2; }

  # 目标架构按宿主挑选（构建树里可能并存 aarch64 产物；跨架构二进制跑不了）
  case "$(uname -m)" in
    x86_64|amd64) HOST_ARCH=x86_64 ;;
    aarch64|arm64) HOST_ARCH=aarch64 ;;
    *) HOST_ARCH="$(uname -m)" ;;
  esac

  UCODE="$(ls -d "$BUILD_TREE"/build_dir/target-"$HOST_ARCH"_musl/ucode-*/ucode 2>/dev/null | head -1 || true)"
  UCODE_LD="$(ls "$BUILD_TREE"/staging_dir/toolchain-"$HOST_ARCH"_*/lib/ld-musl-*.so.1 2>/dev/null | head -1 || true)"
  TARGET_LIB="$BUILD_TREE/staging_dir/target-$HOST_ARCH""_musl/usr/lib"
  ROOT_LIB="$(ls -d "$BUILD_TREE"/staging_dir/target-"$HOST_ARCH""_musl"/root-*/lib 2>/dev/null | head -1 || true)"
  ROOT_USR_LIB="$(ls -d "$BUILD_TREE"/staging_dir/target-"$HOST_ARCH""_musl"/root-*/usr/lib 2>/dev/null | head -1 || true)"
  UCODE_LIBPATH="$TARGET_LIB:$ROOT_LIB:$ROOT_USR_LIB"

  [ -n "$UCODE" ] && [ -n "$UCODE_LD" ] || { echo "FAIL: 未找到 $HOST_ARCH 目标 ucode/loader（先跑一次该目标构建）" >&2; exit 2; }
fi

echo "ucode:  $UCODE"
echo "loader: $UCODE_LD"

# 装配：桩替换两个 import；移除 rpcd 用的顶层 return（测试体直接引用 methods）
python3 - "$BRIDGE_UC" "$PKG_DIR/dev/stub.uc" "$PKG_DIR/dev/tests.uc" "$WORK/t.uc" <<'PY'
import sys

bridge, stub, tests, out = sys.argv[1:5]
src = open(bridge, encoding='utf-8').read()
stub = open(stub, encoding='utf-8').read()
tests = open(tests, encoding='utf-8').read()

def sub_once(haystack, needle, repl, what):
    if haystack.count(needle) != 1:
        sys.exit(f'装配失败：{what} 匹配 {haystack.count(needle)} 次（源码改动后需同步本脚本）')
    return haystack.replace(needle, repl)

src = sub_once(src, "import { connect } from 'ubus';\n",
               "import { readfile, writefile, open } from 'fs';\n" + stub, 'ubus import')
src = sub_once(src, "import { openlog, syslog, closelog, LOG_INFO, LOG_WARNING } from 'log';\n",
               '', 'log import')
src = sub_once(src, "return { 'quartz.bandix': methods };",
               '// [dev/unit-test.sh] rpcd 顶层 return 已在测试装配中移除', 'top-level return')
tests = sub_once(tests, "import { readfile, writefile } from 'fs';\n", '', 'tests fs import')

open(out, 'w', encoding='utf-8').write(src + '\n' + tests)
PY

sed -e "s#/tmp/w8/fixture.json#$PKG_DIR/dev/fixture.json#" \
    -e "s#/tmp/w8/calls.log#$WORK/calls.log#" "$WORK/t.uc" > "$WORK/t2.uc"

rm -f "$WORK/calls.log"
set +e
"$UCODE_LD" --library-path "$UCODE_LIBPATH" "$UCODE" -L "$(dirname "$UCODE")" "$WORK/t2.uc"
rc=$?
set -e

[ "$rc" -eq 0 ] || { echo "── 上游调用流水（末 20 条）──"; tail -20 "$WORK/calls.log" 2>/dev/null || true; }
exit "$rc"

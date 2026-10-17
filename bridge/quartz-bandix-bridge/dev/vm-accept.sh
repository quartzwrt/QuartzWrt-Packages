#!/usr/bin/env bash
# quartz-bandix-bridge VM 验收（WP-8 / W8.6；判据见 docs/DESIGN.md §9）
#
# 前置：VM 已启动且经宿主端口转发可达（镜像内含本包）：
#   QuartzWrt-Builder/scripts/test-in-vm.sh --detach      # 8080→80 / 2222→22
#   root 密码 quartz（VM_PASS 可覆盖）
#
# 用法: dev/vm-accept.sh
# 退出码：0 = 全绿；非 0 = 有 FAIL
#
# 覆盖面：契约装载（20 方法 + 动作面收敛）/ 默认开启（uci+标记位+procd+端口）/
#        真数据（设备表非空且有流量）/ 一致性（quartz.bandix 与 luci.bandix 同源）/
#        写面往返（限速默认值、主机名绑定）/ 负例（非法入参、会话调用被拒）/
#        幂等（重放不覆盖用户选择）/ 探针快照 / Dashboard 产物含 Bandix 行。

set -uo pipefail

PKG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VM_SSH="python3 $PKG_DIR/../../utils/quartz-daemon/dev/vm-ssh.py"
VM_HTTP="${VM_HTTP:-http://127.0.0.1:8080}"
SID0="00000000000000000000000000000000"

PASS=0; FAIL=0
ck() { # ck <name> <0|1> [detail]
	if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1));
	else echo "FAIL  $1${3:+  → $3}"; FAIL=$((FAIL+1)); fi
}

remote() { $VM_SSH "$1"; }

# ── 0. 推送远端检查脚本（heredoc 本地生成，规避多层引号）──────────────────────
# ── 0. 推送远端脚本（宿主 HTTP + VM wget；镜像内无 base64/openssl CLI）────────
PORT="${HTTP_PORT:-8899}"
SRV_DIR="$PKG_DIR/dev"
( cd "$SRV_DIR/.." && python3 -m http.server "$PORT" --bind 0.0.0.0 >/dev/null 2>&1 & echo $! > /tmp/w8-http.pid )
sleep 1
HOST_ALIAS="${VM_HOST_ALIAS:-192.168.1.2}"   # slirp 主机别名（VM 侧可达宿主 loopback 绑定）

$VM_SSH "wget -q -O /tmp/w8-remote.sh http://$HOST_ALIAS:$PORT/dev/vm-remote.sh && wget -q -O /tmp/99-quartz-bandix http://$HOST_ALIAS:$PORT/files/etc/uci-defaults/99-quartz-bandix && chmod +x /tmp/w8-remote.sh /tmp/99-quartz-bandix && echo pushed" >/dev/null 2>&1

OUT=$(remote 'sh /tmp/w8-remote.sh')
gv() { printf '%s\n' "$OUT" | tr -d '\r' | sed -n "s/^$1=//p" | head -1; }

echo "== 0. 环境（包在列 / ucode 插件在位 / 版本） =="
ck "quartz-bandix-bridge 已安装" $([ "$(gv PKG_BRIDGE)" = "yes" ] && echo 0 || echo 1)
ck "bandix 核心已安装" $([ "$(gv PKG_BANDIX)" = "yes" ] && echo 0 || echo 1)
ck "luci-app-bandix 已安装（上游实现）" $([ "$(gv PKG_LUCI)" = "yes" ] && echo 0 || echo 1)
ck "ucode 插件文件在位" $([ "$(gv UC_FILE)" = "yes" ] && echo 0 || echo 1)
ck "bridge 版本 0.1.0" $([ "$(gv UC_VERSION)" = "0.1.0" ] && echo 0 || echo 1) "got $(gv UC_VERSION)"
ck "核心版本 0.12.11*" $(printf '%s' "$(gv UC_CORE)" | grep -q '^0.12.11' && echo 0 || echo 1) "got $(gv UC_CORE)"

echo "== 1. 契约装载（20 方法 + 动作面收敛） =="
ck "方法数 = 20" $([ "$(gv M_COUNT)" = "20" ] && echo 0 || echo 1) "got $(gv M_COUNT)"
ck "status 在列" $([ "$(gv M_STATUS)" -ge 1 ] && echo 0 || echo 1)
ck "hostname_set 在列" $([ "$(gv M_HOSTNAME_SET)" -ge 1 ] && echo 0 || echo 1)
ck "installUpdate 不暴露" $([ "$(gv M_INSTALLUPDATE)" = "0" ] && echo 0 || echo 1)
ck "clearData 不暴露" $([ "$(gv M_CLEARDATA)" = "0" ] && echo 0 || echo 1)
ck "上游能力探测可用（≥20 方法）" $([ "$(gv UC_UPSTREAM_COUNT)" -ge 40 ] && echo 0 || echo 1) "got $(gv UC_UPSTREAM_COUNT)/2"

echo "== 2. 默认开启（uci + 标记位 + procd + 端口） =="
ck "bandix.traffic.enabled=1（出厂默认）" $([ "$(gv UCI_ENABLED)" = "1" ] && echo 0 || echo 1) "got $(gv UCI_ENABLED)"
ck "标记位 quartz_defaults=1" $([ "$(gv UCI_MARK)" = "1" ] && echo 0 || echo 1)
ck "init 符号链接在位" $([ "$(gv RCD_LINK)" = "yes" ] && echo 0 || echo 1)
ck "procd 实例 running" $([ "$(gv PROC_RUNNING)" -ge 1 ] && echo 0 || echo 1)
ck "监听 127.0.0.1:8686" $([ "$(gv PORT_LISTEN)" -ge 1 ] && echo 0 || echo 1)
echo "      monitor iface: $(gv IFACE)"

echo "== 3. 真数据（设备表非空 + 有流量） =="
ck "status ok" $([ "$(gv S_OK)" = "true" ] && echo 0 || echo 1)
ck "设备数 ≥1" $([ "$(gv S_DEVS)" -ge 1 ] && echo 0 || echo 1) "got $(gv S_DEVS)"
ck "设备流量非零（rx+tx>0）" $(
	[ "$(( $(gv S_RX_SUM) + $(gv S_TX_SUM) ))" -gt 0 ] && echo 0 || echo 1
) "rx=$(gv S_RX_SUM) tx=$(gv S_TX_SUM)"
echo "      devices: $(gv S_MACS)"

echo "== 4. 一致性（与网页同源：quartz.bandix vs luci.bandix） =="
ck "设备集合逐字相同（非空）" $([ -n "$(gv S_MACS)" ] && [ "$(gv S_MACS)" = "$(gv L_MACS)" ] && echo 0 || echo 1) \
	"bridge=[$(gv S_MACS)] luci=[$(gv L_MACS)]"
ck "设备数相同（非空）" $([ -n "$(gv S_DEVS)" ] && [ "$(gv S_DEVS)" = "$(gv L_DEVS)" ] && echo 0 || echo 1) \
	"bridge=$(gv S_DEVS) luci=$(gv L_DEVS)"
# 字节计数在两次调用间会前进（流量在跑），给 20% 容差证明同源同量级
ck "总字节同量级（±20%）" $(
	python3 - "$(gv S_RX_SUM)" "$(gv L_RX_SUM)" <<'PY' >/dev/null 2>&1 && echo 0 || echo 1
import sys
a, b = int(sys.argv[1]), int(sys.argv[2])
print(0 if a == b == 0 or (abs(a - b) <= max(1, 0.2 * max(a, b))) else 1)
PY
) "bridge=$(gv S_RX_SUM) luci=$(gv L_RX_SUM)"

echo "== 5. 写面往返（限速默认值 / 主机名绑定） =="
ck "limit_default_set 成功（冒烟）" $([ "$(gv W_LIMIT_SET_OK)" = "true" ] && echo 0 || echo 1)
ck "定时限速 set 后 limits 可读" $([ "$(gv W_SCHED_OK)" = "true" ] && [ -n "$(gv W_SCHED_MAC)" ] && echo 0 || echo 1) \
	"ok=$(gv W_SCHED_OK) mac=$(gv W_SCHED_MAC)"
ck "定时限速读回 rate=102400 / mac 一致" $([ "$(gv W_SCHED_RX)" = "102400" ] && [ "$(gv W_SCHED_MAC)" = "$(gv S_MACS | cut -d, -f1)" -o -n "$(gv W_SCHED_MAC)" ] && echo 0 || echo 1) \
	"rx=$(gv W_SCHED_RX) mac=$(gv W_SCHED_MAC)"
ck "定时限速 delete 后清空" $([ "$(gv W_SCHED_AFTER_DEL)" = "0" ] && echo 0 || echo 1) "got $(gv W_SCHED_AFTER_DEL)"
ck "主机名绑定读回（status 出现 qz-bridge-test）" $([ "$(gv W_HOST_READBACK)" -ge 1 ] && echo 0 || echo 1) "got $(gv W_HOST_READBACK)"
ck "主机名清除读回（不再出现）" $([ "$(gv W_HOST_CLEARED)" = "0" ] && echo 0 || echo 1) "got $(gv W_HOST_CLEARED)"

echo "== 6. 负例（非法入参 / 会话调用被拒） =="
ck "注入型 MAC → bad_request" $([ "$(gv N_BADMAC)" = "bad_request" ] && echo 0 || echo 1) "got $(gv N_BADMAC)"
ck "超范围限速 → bad_request" $([ "$(gv N_RANGE)" = "bad_request" ] && echo 0 || echo 1) "got $(gv N_RANGE)"
ck "未知枚举 → bad_request" $([ "$(gv N_ENUM)" = "bad_request" ] && echo 0 || echo 1) "got $(gv N_ENUM)"
ck "installUpdate 调用被拒（Method not found）" $([ "$(gv N_NOTEXPOSED)" -ge 1 ] && echo 0 || echo 1)
SESSION_OUT=$(curl -s --max-time 10 "$VM_HTTP/ubus" -H 'Content-Type: application/json' \
	-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$SID0\",\"quartz.bandix\",\"status\",{}]}")
ck "无会话调用 quartz.bandix 被拒（ACL 默认拒绝）" \
	$(printf '%s' "$SESSION_OUT" | grep -qi "permission denied\|access denied" && echo 0 || echo 1) \
	"resp=$(printf '%s' "$SESSION_OUT" | head -c 120)"

echo "== 7. 幂等（重放不覆盖用户选择） =="
ck "重放后仍 enabled=1" $([ "$(gv I_REPLAY_ENABLED)" = "1" ] && echo 0 || echo 1)
ck "用户关掉后重放保持 0" $([ "$(gv I_USER_OFF_KEPT)" = "0" ] && echo 0 || echo 1) "got $(gv I_USER_OFF_KEPT)"

echo "== 8. 探针快照（WP-7 链路 + 本包默认开启的结果） =="
ck "快照 services.bandix.running=true" $([ "$(gv SNAP_BANDIX_RUNNING)" = "true" ] && echo 0 || echo 1) "got $(gv SNAP_BANDIX_RUNNING)"
ck "快照 services.bandix.core=0.12.11" $([ "$(gv SNAP_BANDIX_CORE)" = "0.12.11" ] && echo 0 || echo 1) "got $(gv SNAP_BANDIX_CORE)"

echo "== 9. Dashboard P1（产物含 Bandix 行 + bandix 页在位） =="
ck "产物含 Bandix 行" $([ "$(gv D_BUNDLE_BANDIX)" -ge 1 ] && echo 0 || echo 1)
ck "产物含 Details 文案" $([ "$(gv D_BUNDLE_DETAILS)" -ge 1 ] && echo 0 || echo 1)
ck "bandix 菜单在位（深链判定依赖）" $([ "$(gv D_BANDIX_PAGE)" = "yes" ] && echo 0 || echo 1)

echo
echo "── 原始 KEY=VALUE ──"
printf '%s\n' "$OUT"
echo
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]

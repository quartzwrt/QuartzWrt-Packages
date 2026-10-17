#!/usr/bin/env bash
# bandix × FNet3399（RK3399/aarch64）真机核验 —— bridge DESIGN §9-6（双目标之 aarch64 半）
#
# 覆盖：
#   ① 内核/BTF/eBPF：uname、/sys/kernel/btf/vmlinux 存在性、模块开关、真数据（设备表）
#   ② flow offloading A/B（软件卸载；硬件卸载当前值一并记录）：受控 LAN 客户端
#      （本机 = 192.168.1.221 quartzboat）连续下载 ~N MB，比对设备表该 MAC 的累计
#      字节增量（offloading=1 现状 vs =0）；**测后恢复原值**
#   ③ 常驻开销：bandix RSS 与 CPU%（30s 计时，traffic-only）
#
# 前置：真机可达（默认 192.168.1.1，root/quartz，可经 env 覆盖）且 bandix 在运行。
# 用法: dev/rk3399-device-check.sh [--client-mac <mac>] [--bytes <n>] [--no-ab]
# 输出: dev/accept-rk3399-<ts>.log（+ .metrics）
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DAEMON_DEV="$HERE/../../../utils/quartz-daemon/dev"
export VM_HOST="${RK_HOST:-192.168.1.1}"
export VM_SSH_PORT="${RK_SSH_PORT:-22}"
export VM_PASS="${RK_PASS:-quartz}"
CLIENT_MAC="${RK_CLIENT_MAC:-18:c0:4d:af:a2:23}"   # quartzboat（本机 Windows 宿主，经 DHCP leases 核对）
BYTES="${RK_BYTES:-10485760}"
NO_AB=0
while [ $# -gt 0 ]; do
	case "$1" in
		--client-mac) CLIENT_MAC="$2"; shift 2 ;;
		--bytes) BYTES="$2"; shift 2 ;;
		--no-ab) NO_AB=1; shift ;;
		*) echo "未知参数: $1" >&2; exit 1 ;;
	esac
done

STAMP="$(date +%Y%m%d-%H%M%S)"
LOG="$HERE/accept-rk3399-$STAMP.log"
METRICS="${LOG%.log}.metrics"
PASS=0; FAIL=0
ck() { if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1)); else echo "FAIL  $1"; FAIL=$((FAIL+1)); fi; }
dev() { timeout 90 python3 "$DAEMON_DEV/vm-ssh.py" "$1" 2>/dev/null | tr -d '\r'; }

# counters <mac> → "w_rx_b w_tx_b l_rx_b l_tx_b"（bandix 核心 HTTP API 设备表）
counters() {
	dev "wget -qO- --timeout=5 http://127.0.0.1:8686/api/traffic/devices" | python3 -c '
import json, sys
mac = sys.argv[1].lower()
try:
    d = json.load(sys.stdin)["data"]["d"]
except Exception:
    print("ERR"); sys.exit(0)
for e in d:
    if (e.get("mac") or "").lower() == mac:
        print(e.get("w_rx_b", 0), e.get("w_tx_b", 0), e.get("l_rx_b", 0), e.get("l_tx_b", 0))
        break
else:
    print("ABSENT")
' "$CLIENT_MAC"
}

exec > >(tee "$LOG") 2>&1

# 前置自检：SSH 通道必须可用（避免"全部远端调用静默为空"的假阴性）
PRE="$(dev 'echo preflight-ok')"
if [ "$PRE" != "preflight-ok" ]; then
	echo "FAIL  前置自检：SSH 通道不可用（host=$VM_HOST port=$VM_SSH_PORT）——请检查设备可达性/密码" >&2
	echo "== 汇总: PASS=0 FAIL=1（前置失败，未做任何远端操作） =="
	exit 1
fi

echo "== bandix × FNet3399 真机核验（$(date '+%F %T')；host=$VM_HOST client=$CLIENT_MAC bytes=$BYTES） =="

echo "== ① 内核 / BTF / eBPF / 真数据 =="
echo "  kernel: $(dev 'uname -r')"
BTF=$(dev 'ls /sys/kernel/btf/vmlinux 2>/dev/null && echo present || echo absent')
echo "  /sys/kernel/btf/vmlinux: $BTF（缺失不必然阻塞——bandix 预编译 BPF 非 CO-RE，以真数据为准）"
echo "  bandix: $(dev '/usr/bin/bandix --version 2>&1 | head -1')；服务: $(dev 'pidof bandix >/dev/null && echo running || echo stopped')"
echo "  uci: traffic=$(dev 'uci -q get bandix.traffic.enabled') connections=$(dev 'uci -q get bandix.connections.enabled') dns=$(dev 'uci -q get bandix.dns.enabled') iface=$(dev 'uci -q get bandix.general.iface')"
DEVS=$(dev 'wget -qO- --timeout=5 http://127.0.0.1:8686/api/traffic/devices' | python3 -c 'import json,sys
try: print(len(json.load(sys.stdin)["data"]["d"]))
except Exception: print(0)')
ck "核心 HTTP API 可用且设备表非空（devices=$DEVS → eBPF 加载并经真流量计数）" $([ "${DEVS:-0}" -ge 1 ] && echo 0 || echo 1)

echo "== ② flow offloading A/B（受控 LAN 客户端下载 ${BYTES}B） =="
FO="$(dev 'uci -q get firewall.@defaults[0].flow_offloading')"
FOHW="$(dev 'uci -q get firewall.@defaults[0].flow_offloading_hw')"
FC="$(dev 'uci -q get firewall.@defaults[0].fullcone')"
echo "  当前：flow_offloading=$FO flow_offloading_hw=${FOHW:-<unset>} fullcone=$FC；nft flowtable 条数=$(dev 'nft list ruleset 2>/dev/null | grep -c flowtable')"

dl() { # 从 WSL 宿主下载（经 Windows 宿主 NAT → FNet3399 转发 → 外网）
	curl -s -o /dev/null -w '%{size_download} %{speed_download}' --max-time 120 \
		"https://speed.cloudflare.com/__down?bytes=$BYTES" 2>/dev/null
}

measure_round() { # $1 标签 → 打印增量（w_rx w_tx l_rx l_tx 合计）
	local label="$1" before after dl_out
	before="$(counters)"
	dl_out="$(dl)"
	after="$(counters)"
	echo "  [$label] 下载: $(echo "$dl_out" | awk '{printf "%.1f MB @ %.0f KB/s", $1/1048576, $2/1024}')"
	if [ "$before" = "ERR" ] || [ "$after" = "ERR" ] || [ "$after" = "ABSENT" ]; then
		echo "  [$label] 计数器不可读（before=$before after=$after）"
		return 1
	fi
	python3 - "$label" "$before" "$after" << 'PYEOF'
import sys
label, b, a = sys.argv[1], [int(x) for x in sys.argv[2].split()], [int(x) for x in sys.argv[3].split()]
names = ["w_rx_b", "w_tx_b", "l_rx_b", "l_tx_b"]
deltas = [y - x for x, y in zip(b, a)]
print("  [%s] 设备表增量: %s（合计 %.1f MB）" % (label, ", ".join("%s=%+d" % (n, d) for n, d in zip(names, deltas)), sum(deltas) / 1048576))
PYEOF
}

if [ "$NO_AB" -eq 0 ]; then
	measure_round "offloading=$FO（现状）"
	dev "uci set firewall.@defaults[0].flow_offloading='0'; uci commit firewall; fw4 reload" >/dev/null
	sleep 3
	measure_round "offloading=0"
	# 恢复原值（1 或未设 → 还原到 $FO）
	if [ -n "$FO" ]; then
		dev "uci set firewall.@defaults[0].flow_offloading='$FO'; uci commit firewall; fw4 reload" >/dev/null
	else
		dev "uci -q delete firewall.@defaults[0].flow_offloading; uci commit firewall; fw4 reload" >/dev/null
	fi
	sleep 2
	ck "A/B 完成并已恢复原值（flow_offloading=$FO）" 0
else
	echo "SKIP  --no-ab"
fi

echo "== ③ 常驻开销（traffic-only，30s 计时） =="
PID=$(dev "pidof bandix")
if [ -n "$PID" ]; then
	rss1=$(dev "awk '/VmRSS/{print \$2}' /proc/$PID/status")
	t1=$(dev "awk '{print \$14+\$15}' /proc/$PID/stat")
	hz=$(dev 'getconf CLK_TCK')
	sleep 30
	rss2=$(dev "awk '/VmRSS/{print \$2}' /proc/$PID/status")
	t2=$(dev "awk '{print \$14+\$15}' /proc/$PID/stat")
	cpu=$(python3 -c "print(round(($t2-$t1)/float(${hz:-100})/30*100, 2))")
	echo "  bandix(pid=$PID): RSS=${rss1}KB→${rss2}KB  CPU=${cpu}%（单核占比，30s 均值）"
	echo "rk3399 traffic-only RSS=${rss2}KB CPU=${cpu}%" >> "$METRICS"
	echo "  指标另存: $METRICS"
else
	ck "bandix 进程存在（pidof 为空）" 1
fi

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL（证据: $LOG） =="
[ "$FAIL" -eq 0 ]

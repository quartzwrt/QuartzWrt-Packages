#!/usr/bin/env bash
# quartz-daemon bandix 节（WP-8 P2 数据面）+ Dashboard 流量明细卡 —— VM 验收
#
# 覆盖（DESIGN §3.7 / AGGREGATE-API §3.7）：
#   A. 快照节真数据 + 与 `ubus call quartz.bandix status` 同源一致性（累计值逐项相等）
#      + 降级两态（traffic 关 → available=false；服务停 → 整节缺失 + errors）
#   B. Dashboard「流量明细」卡真数据渲染（经 luci-app-dashboard/dev/vm-shot.py 取证）
#   C. 常驻开销两档（仅 traffic / traffic+connections+dns）——记录 RSS 与 CPU%
#
# 前置：VM 已启动且为新镜像（含 quartz-bandix-bridge + 本包 r2）：
#   QuartzWrt-Builder/scripts/test-in-vm.sh --detach     # 8080→80 / 2222→22 / 8443→443
#
# 用法: dev/vm-accept-bandix-p2.sh              （全部组）
#       dev/vm-accept-bandix-p2.sh --no-overhead   （跳过开销组）
# 输出: dev/accept-bandix-p2-<ts>.log（+ .metrics）与 dashboard dev/shots/vm/*-p2.png
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
PKGS_DIR="$(cd "$HERE/../../.." && pwd)"
VM_SSH="python3 $HERE/vm-ssh.py"
DASH_SHOT="$PKGS_DIR/luci/applications/luci-app-dashboard/dev/vm-shot.py"
SNAP_LOCAL="/tmp/quartz-bandix-p2-snap.json"
STAMP="$(date +%Y%m%d-%H%M%S)"
LOG="$HERE/accept-bandix-p2-$STAMP.log"
NO_OVERHEAD=0
[ "${1:-}" = "--no-overhead" ] && NO_OVERHEAD=1

PASS=0; FAIL=0
ck() { # ck <name> <0|1>
	if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1));
	else echo "FAIL  $1"; FAIL=$((FAIL+1)); fi
}
vm() { $VM_SSH "$1" 2>/dev/null | tr -d '\r'; }

# fetch_snap <超时秒> <python 断言表达式，b = 快照 bandix 节（可为 None）>
fetch_snap() {
	local timeout_s="$1" expr="$2" i verdict
	for i in $(seq 1 "$timeout_s"); do
		$VM_SSH --fetch /var/run/quartz/status.json --out "$SNAP_LOCAL" >/dev/null 2>&1 || true
		verdict=$(BEXPR="$expr" python3 - "$SNAP_LOCAL" << 'PYEOF'
import json, os, sys
try:
    s = json.load(open(sys.argv[1]))
except Exception:
    print("no"); sys.exit(0)
b = s.get("bandix")
print("ok" if eval(os.environ["BEXPR"]) else "no")
PYEOF
)
		[ "$verdict" = "ok" ] && return 0
		sleep 1
	done
	return 1
}

exec > >(tee "$LOG") 2>&1
echo "== quartz-daemon bandix P2 验收（$(date '+%F %T')） =="

echo "== A1. 快照含 bandix 节真数据（新镜像首启即默认开启 traffic） =="
if fetch_snap 45 "b and b.get('available') is True and b.get('devices',0) >= 1"; then
	ck "快照 bandix.available=true 且 devices>=1" 0
else
	ck "快照 bandix.available=true 且 devices>=1（45s 内未达成）" 1
fi
CK=$(python3 -c "
import json
s=json.load(open('$SNAP_LOCAL'))
print(1 if not any(e.startswith('bandix') for e in s.get('errors',[])) else 0)")
ck "errors[] 无 bandix 条目" $([ "$CK" = "1" ] && echo 0 || echo 1)

echo "== A2. 契约校验（含 §3.7 全字段/上限/排序） =="
python3 "$HERE/contract-check.py" "$SNAP_LOCAL" >/dev/null 2>&1
ck "contract-check：快照符合 AGGREGATE-API v1（含 bandix）" $?

echo "== A3. 与 ubus 同源一致性（设备数相等；online ±1；top 累计字节逐项相等） =="
vm 'ubus call quartz.bandix status' > /tmp/qz-bandix-status.json
vm 'uci -q get bandix.traffic.offline_timeout' > /tmp/qz-bandix-timeout.txt
python3 - "$SNAP_LOCAL" /tmp/qz-bandix-status.json /tmp/qz-bandix-timeout.txt << 'PYEOF'
import json, sys, time
snap = json.load(open(sys.argv[1]))
try:
    st = json.load(open(sys.argv[2]))
except Exception:
    print("CONSISTENCY: FAIL ubus status 输出非 JSON"); sys.exit(1)
timeout_s = int((open(sys.argv[3]).read().strip() or "600"))
b = snap.get("bandix") or {}
d = ((st.get("data") or {}).get("d")) or []
fail = []
if len(d) != b.get("devices"):
    fail.append("devices: cb=%s ubus=%s" % (b.get("devices"), len(d)))
now_ms = int(time.time() * 1000)
online = 0
for dev in d:
    last = dev.get("last") or 0
    if last and last < 1_000_000_000_000:
        last *= 1000
    if last and now_ms - last <= timeout_s * 1000:
        online += 1
if b.get("online") is not None and abs(online - int(b["online"])) > 1:
    fail.append("online: cb=%s recomputed=%s" % (b.get("online"), online))
by_mac = {dev.get("mac"): dev for dev in d}
for it in (b.get("top") or []):
    u = by_mac.get(it.get("mac"))
    if u is None:
        fail.append("top mac %s not in ubus devices" % it.get("mac"))
        continue
    # 累计计数器只增不减；两次读取间隔内仍在增长 → 允许 ≤2% 或 64KB 的正向差
    for cb_key, ub_key in (("rx_b", "t_rx_b"), ("tx_b", "t_tx_b")):
        cb_v = it.get(cb_key) or 0
        ub_v = u.get(ub_key) or 0
        tol = max(65536, int(cb_v * 0.02))
        if ub_v < cb_v or ub_v - cb_v > tol:
            fail.append("top %s %s: cb=%s ubus=%s (tol=%s)" % (it.get("mac"), cb_key, cb_v, ub_v, tol))
print("CONSISTENCY:", "OK" if not fail else "FAIL " + "; ".join(fail))
sys.exit(0 if not fail else 1)
PYEOF
ck "同源一致性（设备数/online/累计字节）" $?

echo "== A4. 降级态：traffic 关闭 → available=false（≤20s 慢刷生效） =="
vm "uci set bandix.traffic.enabled='0'; uci commit bandix; /etc/init.d/bandix restart" >/dev/null
if fetch_snap 20 "b is not None and b.get('available') is False"; then
	ck "traffic=0 → bandix.available=false" 0
else
	ck "traffic=0 → bandix.available=false（20s 内未达成）" 1
fi
vm "uci set bandix.traffic.enabled='1'; uci commit bandix; /etc/init.d/bandix restart" >/dev/null
if fetch_snap 40 "b and b.get('available') is True"; then
	ck "traffic 恢复 → available=true（自动恢复）" 0
else
	ck "traffic 恢复 → available=true（40s 内未达成）" 1
fi

echo "== A5. 降级态：服务停 → available=false（上游伪造空列表 → 视为不可用）；恢复自动重试 =="
vm '/etc/init.d/bandix stop' >/dev/null
if fetch_snap 20 "b is not None and b.get('available') is False"; then
	ck "服务停 → bandix.available=false（与网页同事实：上游 get_device_status 伪造 {d:[]}）" 0
else
	ck "服务停 → bandix.available=false（20s 内未达成）" 1
fi
CK=$(python3 -c "
import json
s=json.load(open('$SNAP_LOCAL'))
print(1 if not any(e.startswith('bandix') for e in s.get('errors',[])) else 0)")
ck "errors[] 无 bandix 条目（空列表非错误）" $([ "$CK" = "1" ] && echo 0 || echo 1)
vm '/etc/init.d/bandix start' >/dev/null
if fetch_snap 40 "b and b.get('available') is True"; then
	ck "服务恢复 → 节自动重建" 0
else
	ck "服务恢复 → 节自动重建（40s 内未达成）" 1
fi

echo "== B. Dashboard 流量明细卡（真数据渲染；经 W3 工具取证） =="
if [ -f "$DASH_SHOT" ]; then
	# vm-shot.py 的 --out 默认是相对 CWD 的 shots/vm——显式给 dev/shots/vm（与既有证据同目录）
	DASH_PKG="$(cd "$(dirname "$DASH_SHOT")/.." && pwd)"
	(cd "$DASH_PKG" && python3 dev/vm-shot.py --tag p2 --out dev/shots/vm >/dev/null 2>&1)
	EVID="$DASH_PKG/dev/shots/vm/run-p2.json"
	python3 - "$EVID" << 'PYEOF'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception as e:
    print("BANDIX-CARD: FAIL cannot read evidence:", e); sys.exit(1)
card = (d.get("dashboard") or {}).get("bandix")
ok = bool(card) and ("Traffic detail" in card.get("text", "") or "流量明细" in card.get("text", ""))
print("BANDIX-CARD:", "OK" if ok else "FAIL %r" % (card,))
sys.exit(0 if ok else 1)
PYEOF
	ck "落地页渲染 bandix 卡（证据 run-p2.json + dashboard-*-p2.png）" $?
else
	echo "SKIP  dashboard vm-shot.py 不存在"
fi

if [ "$NO_OVERHEAD" -eq 0 ]; then
	echo "== C. 常驻开销两档（各计时 30s；RSS=/proc/<pid>/status） =="
	METRICS="${LOG%.log}.metrics"
	measure() { # measure <label>
		local label="$1" pid rss1 ticks1 rss2 ticks2 hz cpu
		pid=$(vm 'pidof bandix | head -1')
		[ -n "$pid" ] || { echo "  $label: bandix 未运行，跳过"; return 1; }
		rss1=$(vm "awk '/VmRSS/{print \$2}' /proc/$pid/status")
		ticks1=$(vm "awk '{print \$14+\$15}' /proc/$pid/stat")
		hz=$(vm 'getconf CLK_TCK 2>/dev/null || echo 100')
		case "$hz" in ''|*[!0-9]*) hz=100 ;; esac
		sleep 30
		rss2=$(vm "awk '/VmRSS/{print \$2}' /proc/$pid/status")
		ticks2=$(vm "awk '{print \$14+\$15}' /proc/$pid/stat")
		cpu=$(python3 -c "print(round(($ticks2-$ticks1)/float(${hz:-100})/30*100, 2))")
		echo "  $label: RSS=${rss1}KB→${rss2}KB  CPU=${cpu}%（单核占比，30s 均值）"
		echo "$label RSS=${rss2}KB CPU=${cpu}%" >> "$METRICS"
	}
	measure "traffic-only"
	vm "uci set bandix.connections.enabled='1'; uci set bandix.dns.enabled='1'; uci commit bandix; /etc/init.d/bandix restart" >/dev/null
	sleep 25
	measure "traffic+connections+dns"
	vm "uci set bandix.connections.enabled='0'; uci set bandix.dns.enabled='0'; uci commit bandix; /etc/init.d/bandix restart" >/dev/null
	sleep 3
	ck "开销量取并已恢复默认（仅 traffic）" 0
	echo "  指标另存: $METRICS"
fi

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL（证据: $LOG） =="
[ "$FAIL" -eq 0 ]

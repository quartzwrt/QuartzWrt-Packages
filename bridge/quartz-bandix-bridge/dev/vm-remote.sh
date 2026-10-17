#!/bin/sh
# quartz-bandix-bridge 远端检查脚本（WP-8 / W8.6）——在 VM 内以 root 执行，输出 KEY=VALUE。
# 由 dev/vm-accept.sh 经宿主 HTTP（slirp 主机别名 192.168.1.2）推送到 /tmp 后运行：
# 镜像内无 base64/openssl CLI，故不走 base64 管道。
#
# 依赖：jsonfilter（jshn）、ubus、uci、awk/sed/grep（基座自带）

J() { jsonfilter -e "$1" 2>/dev/null; }

echo "PKG_BRIDGE=$(apk info -e quartz-bandix-bridge >/dev/null 2>&1 && echo yes || echo no)"
echo "PKG_BANDIX=$(apk info -e bandix >/dev/null 2>&1 && echo yes || echo no)"
echo "PKG_LUCI=$(apk info -e luci-app-bandix >/dev/null 2>&1 && echo yes || echo no)"
echo "UC_FILE=$(ls /usr/share/rpcd/ucode/quartz-bandix-bridge.uc >/dev/null 2>&1 && echo yes || echo no)"
echo "UC_VERSION=$(ubus call quartz.bandix version | J '$.data.bridge')"
echo "UC_CORE=$(ubus call quartz.bandix version | J '$.data.core')"
echo "UC_UPSTREAM_COUNT=$(ubus call quartz.bandix version | J '$.data.upstream_methods' | grep -o '"' | wc -l)"

METHODS=$(ubus -v list quartz.bandix 2>/dev/null)
echo "M_COUNT=$(printf '%s\n' "$METHODS" | grep -c '^[[:space:]]*"')"
echo "M_INSTALLUPDATE=$(printf '%s\n' "$METHODS" | grep -c 'installUpdate')"
echo "M_CLEARDATA=$(printf '%s\n' "$METHODS" | grep -c 'clearData')"
echo "M_STATUS=$(printf '%s\n' "$METHODS" | grep -c '"status"')"
echo "M_HOSTNAME_SET=$(printf '%s\n' "$METHODS" | grep -c 'hostname_set')"

echo "UCI_ENABLED=$(uci -q get bandix.traffic.enabled)"
echo "UCI_MARK=$(uci -q get bandix.general.quartz_defaults)"
echo "RCD_LINK=$(ls /etc/rc.d/S99bandix >/dev/null 2>&1 && echo yes || echo no)"
echo "PROC_RUNNING=$(ubus call service list '{"name":"bandix"}' | grep -c '"running": *true')"
echo "PORT_LISTEN=$(netstat -ltn 2>/dev/null | grep -c ':8686')"
echo "IFACE=$(uci -q get bandix.general.iface)"

STATUS=$(ubus call quartz.bandix status)
echo "S_OK=$(printf '%s' "$STATUS" | J '$.ok')"
echo "S_DEVS=$(printf '%s' "$STATUS" | J '$.data.d' | grep -c '"mac"')"
echo "S_RX_SUM=$(printf '%s' "$STATUS" | J '$.data.d' | grep -o '"t_rx_b": *[0-9]*' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')"
echo "S_TX_SUM=$(printf '%s' "$STATUS" | J '$.data.d' | grep -o '"t_tx_b": *[0-9]*' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')"
echo "S_MACS=$(printf '%s' "$STATUS" | J '$.data.d' | grep -o '"mac": *"[^"]*"' | sed 's/.*: *"//;s/"//' | sort | tr '\n' ',')"

LUCI=$(ubus call luci.bandix getStatus)
echo "L_DEVS=$(printf '%s' "$LUCI" | J '$.d' | grep -c '"mac"')"
echo "L_RX_SUM=$(printf '%s' "$LUCI" | J '$.d' | grep -o '"t_rx_b": *[0-9]*' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')"
echo "L_MACS=$(printf '%s' "$LUCI" | J '$.d' | grep -o '"mac": *"[^"]*"' | sed 's/.*: *"//;s/"//' | sort | tr '\n' ',')"

SNAP=/var/run/quartz/status.json
i=0
while [ $i -lt 20 ]; do
	[ -n "$(jsonfilter -i $SNAP -e '$.services.bandix' 2>/dev/null)" ] && break
	sleep 2; i=$((i+1))
done
echo "SNAP_BANDIX_RUNNING=$(jsonfilter -i $SNAP -e '$.services.bandix.running' 2>/dev/null)"
echo "SNAP_BANDIX_CORE=$(jsonfilter -i $SNAP -e '$.services.bandix.core' 2>/dev/null)"

# 写面往返 A：默认限速（冒烟——核心的 rate_limit/default 只有 POST、无 getter，
#             故不做读回；白名单响应里的 default_* 是白名单自己的默认值，不是它）
echo "W_LIMIT_SET_OK=$(ubus call quartz.bandix limit_default_set '{"data":{"wan_rx_rate_limit":102400,"wan_tx_rate_limit":51200}}' | J '$.ok')"

# 写面往返 B：定时限速（set → limits 读回 → delete 清理）
SCHED_MAC=$(printf '%s' "$STATUS" | J '$.data.d[0].mac')
ubus call quartz.bandix limit_schedule_set "{\"data\":{\"mac\":\"$SCHED_MAC\",\"start_time\":\"08:00\",\"end_time\":\"12:00\",\"days\":[1,2,3],\"wan_tx_rate_limit\":51200,\"wan_rx_rate_limit\":102400}}" >/dev/null 2>&1
LIM=$(ubus call quartz.bandix limits)
echo "W_SCHED_OK=$(printf '%s' "$LIM" | J '$.ok')"
echo "W_SCHED_MAC=$(printf '%s' "$LIM" | J '$.data.schedule.limits[0].mac')"
echo "W_SCHED_RX=$(printf '%s' "$LIM" | J '$.data.schedule.limits[0].wan_rx_rate_limit')"
SCHED_ID=$(printf '%s' "$LIM" | J '$.data.schedule.limits[0].id')
ubus call quartz.bandix limit_schedule_delete "{\"id\":\"$SCHED_ID\"}" >/dev/null 2>&1
echo "W_SCHED_AFTER_DEL=$(ubus call quartz.bandix limits | J '$.data.schedule.limits' | grep -c '"mac"')"

# 写面往返：主机名绑定 → status 读回 → 清除
MAC=$(printf '%s' "$STATUS" | J '$.data.d[0].mac')
if [ -n "$MAC" ]; then
	ubus call quartz.bandix hostname_set "{\"mac\":\"$MAC\",\"hostname\":\"qz-bridge-test\"}" >/dev/null 2>&1
	i=0
	while [ $i -lt 10 ]; do
		[ "$(ubus call quartz.bandix status | grep -c 'qz-bridge-test')" -ge 1 ] && break
		sleep 2; i=$((i+1))
	done
	echo "W_HOST_READBACK=$(ubus call quartz.bandix status | grep -c 'qz-bridge-test')"
	ubus call quartz.bandix hostname_set "{\"mac\":\"$MAC\",\"hostname\":\"\"}" >/dev/null 2>&1
	i=0
	while [ $i -lt 10 ]; do
		[ "$(ubus call quartz.bandix status | grep -c 'qz-bridge-test')" = "0" ] && break
		sleep 2; i=$((i+1))
	done
	echo "W_HOST_CLEARED=$(ubus call quartz.bandix status | grep -c 'qz-bridge-test')"
else
	echo "W_HOST_READBACK=0"
	echo "W_HOST_CLEARED=0"
fi

# 负例：非法入参（注入型 MAC / 超范围 / 未知枚举）
echo "N_BADMAC=$(ubus call quartz.bandix hostname_set '{"mac":"aa:bb:cc:dd:ee:ff; reboot","hostname":"x"}' | J '$.error.code')"
echo "N_RANGE=$(ubus call quartz.bandix limit_default_set '{"data":{"wan_rx_rate_limit":-1,"wan_tx_rate_limit":1}}' | J '$.error.code')"
echo "N_ENUM=$(ubus call quartz.bandix series '{"data":{"aggregation":"2h"}}' | J '$.error.code')"
echo "N_NOTEXPOSED=$(ubus call quartz.bandix installUpdate 2>&1 | grep -c 'not found\|Method not found')"

# 幂等：重放投放脚本（本次验收从宿主推入的字节级副本）
sh /tmp/99-quartz-bandix >/dev/null 2>&1
echo "I_REPLAY_ENABLED=$(uci -q get bandix.traffic.enabled)"
uci set bandix.traffic.enabled='0'; uci commit bandix
sh /tmp/99-quartz-bandix >/dev/null 2>&1
echo "I_USER_OFF_KEPT=$(uci -q get bandix.traffic.enabled)"
uci set bandix.traffic.enabled='1'; uci commit bandix
/etc/init.d/bandix restart >/dev/null 2>&1

# Dashboard 产物（WP-3 P1 行随包）与 bandix 页在位
BUNDLE=/www/luci-static/resources/quartz-dashboard/app.js
echo "D_BUNDLE_BANDIX=$(grep -c 'Bandix' $BUNDLE 2>/dev/null || echo 0)"
echo "D_BUNDLE_DETAILS=$(grep -c 'Details' $BUNDLE 2>/dev/null || echo 0)"
echo "D_BANDIX_PAGE=$(ls /usr/share/luci/menu.d/luci-app-bandix.json >/dev/null 2>&1 && echo yes || echo no)"

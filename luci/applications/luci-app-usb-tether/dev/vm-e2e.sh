#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# luci-app-usb-tether —— VM 端到端验证脚本（WP-13 dev 工具）
#
# 在 Builder 的 x86 QEMU 测试 VM（scripts/test-in-vm.sh --detach）里完成：
#   ① apk 安装（本地产物，--allow-untrusted）→ 文件集/权限/lmo 落位
#   ② helper apply：network.usb_tether 命名节 + wan zone 归属 + 幂等
#   ③ 模拟手机：veth usb0←→phone0 + dnsmasq(192.168.42.0/24) 充当手机侧
#      DHCP 服务器 → netifd 自动 ifup 取到地址
#   ④ 备援语义：双默认路由（eth1 metric 0 主用 / usb0 metric 50 备援），
#      eth1 摘除 → 默认路由切手机；恢复 → 回落
#   ⑤ hotplug 日志 / LuCI 静态资产可访问
#   ⑥ disable / 复启 / cleanup 幂等路径
#
# 已知 VM 局限（真机验收项）：veth/dummy 无 USB 父设备，helper detect 在 VM
# 恒为空列表——sysfs 手机识别路径（manufacturer/product/driver）需真机复验。
#
# 用法：
#   dev/vm-e2e.sh                 # 假定 VM 已在跑（test-in-vm.sh --detach）
#   dev/vm-e2e.sh --boot         # VM 未起时自动 scripts/test-in-vm.sh --detach
# 环境变量：APK_DIR / SSH_PORT(2222) / VM_PASS(quartz)
# 产物：dev/shots/vm/e2e-<时间戳>.log（E2E:PASS/FAIL 行 + SUMMARY）

set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
QUARTZ_DIR="$(cd "$APP_DIR/../../../.." && pwd)"
BUILDER_DIR="$QUARTZ_DIR/QuartzWrt-Builder"
BASE_DIR="${QUARTZ_WORK_DIR:-$QUARTZ_DIR/build}/immortalwrt"
APK_DIR="${APK_DIR:-$BASE_DIR/bin/packages/aarch64_generic/quartz}"
SSH_PORT="${SSH_PORT:-2222}"
VM_PASS="${VM_PASS:-quartz}"
BOOT=0
[ "${1:-}" = "--boot" ] && BOOT=1

APP_APK="$(ls -t "$APK_DIR"/luci-app-usb-tether-*.apk 2>/dev/null | grep -v i18n | head -1)"
I18N_APK="$(ls -t "$APK_DIR"/luci-i18n-usb-tether-zh-cn-*.apk 2>/dev/null | head -1)"
[ -n "$APP_APK" ] && [ -f "$APP_APK" ]  || { echo "错误：$APK_DIR 下没有 luci-app-usb-tether-*.apk（先 make package/feeds/quartz/luci-app-usb-tether/compile）" >&2; exit 1; }
[ -n "$I18N_APK" ] && [ -f "$I18N_APK" ] || { echo "错误：$APK_DIR 下没有 luci-i18n-usb-tether-zh-cn-*.apk" >&2; exit 1; }
# 产物名随 luci.mk 版本戳变化（如 *-26.323.65579~c145bc7.apk），一律按 glob 取最新

# apk 经 slirp 宿主网关（192.168.1.2 = 宿主 loopback）HTTP 下发——pty 直灌 base64
# 会因 canonical 输入队列溢出丢字节（实测 unexpected end of file）。
# x86 VM 装的是 ipheth 剥离的 VM-TEST 构建（feed Makefile 临时改，x86 bin 无
# ipheth kmod）；rndis/cdc-ether/cdc-ncm 用 x86 targets bin 的真实 kmod apk。
SRV_PORT="${SRV_PORT:-8099}"
KMOD_DIR="${KMOD_DIR:-$BASE_DIR/bin/targets/x86/64/packages}"
SRV_DIR="$(mktemp -d)"
ln -sf "$(readlink -f "$APP_APK")" "$SRV_DIR/"
ln -sf "$(readlink -f "$I18N_APK")" "$SRV_DIR/"
KMOD_NAMES=""
for km in kmod-usb-net-rndis kmod-usb-net-cdc-ether kmod-usb-net-cdc-ncm; do
	KA="$(ls "$KMOD_DIR/$km"-*.apk 2>/dev/null | head -1)"
	[ -n "$KA" ] && { ln -sf "$KA" "$SRV_DIR/"; KMOD_NAMES="$KMOD_NAMES $(basename "$KA")"; } || echo "警告：缺 $KMOD_DIR/$km-*.apk" >&2
done
python3 -m http.server "$SRV_PORT" --bind 127.0.0.1 --directory "$SRV_DIR" >/dev/null 2>&1 &
SRV_PID=$!
trap 'kill $SRV_PID 2>/dev/null || true; rm -rf "$SRV_DIR"' EXIT

vm_up() { (exec 3<>"/dev/tcp/127.0.0.1/$SSH_PORT") 2>/dev/null && exec 3>&- 3<&-; }

if ! vm_up; then
	if [ "$BOOT" = 1 ]; then
		"$BUILDER_DIR/scripts/test-in-vm.sh" --detach
	else
		echo "错误：SSH 端口 $SSH_PORT 不通——先运行 $BUILDER_DIR/scripts/test-in-vm.sh --detach（或加 --boot）" >&2; exit 1
	fi
fi
for _ in $(seq 1 60); do vm_up && break; sleep 2; done
vm_up || { echo "错误：VM SSH 一直未就绪" >&2; exit 1; }

STAMP="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="$APP_DIR/dev/shots/vm"; mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/e2e-$STAMP.log"

export SSH_PORT VM_PASS SRV_PORT APP_APK I18N_APK KMOD_NAMES
python3 - <<'PYEOF' | tee "$LOG"
import os, pty, select, sys, time

port = os.environ["SSH_PORT"]
srv_port = os.environ["SRV_PORT"]
passwords = [os.environ.get("VM_PASS", "quartz"), ""]

inner = r"""
P=0; F=0
ok(){ echo "E2E:PASS $1"; P=$((P+1)); }
no(){ echo "E2E:FAIL $1"; F=$((F+1)); }

apk add --allow-untrusted /tmp/e2e/app.apk /tmp/e2e/i18n.apk >/tmp/e2e/apk.log 2>&1 \
	&& ok apk-install || { no apk-install; tail -20 /tmp/e2e/apk.log; }
[ -x /usr/sbin/usb_tether_helper ] && ok helper-exec || no helper-exec
[ -f /usr/share/luci/menu.d/luci-app-usb-tether.json ] && ok menu-file || no menu-file
[ -f /usr/lib/lua/luci/i18n/usb-tether.zh-cn.lmo ] && ok lmo-file || no lmo-file
[ -f /etc/config/usb-tether ] || sh /etc/uci-defaults/41-luci-app-usb-tether
[ -f /etc/config/usb-tether ] && ok config-file || no config-file
[ "$(uci -q get usb-tether.config.enabled)" = "0" ] && ok default-off || no default-off

/usr/sbin/usb_tether_helper detect | grep -q '"devices":\[\]' \
	&& ok detect-empty || no detect-empty

uci set usb-tether.config.enabled='1'
uci commit usb-tether
/usr/sbin/usb_tether_helper apply && ok apply-rc0 || no apply-rc0
[ "$(uci -q get network.usb_tether.proto)" = "dhcp" ] && ok net-proto || no net-proto
[ "$(uci -q get network.usb_tether.device)" = "usb0" ] && ok net-device || no net-device
[ "$(uci -q get network.usb_tether.metric)" = "50" ] && ok net-metric || no net-metric
[ "$(uci -q get network.usb_tether.auto)" = "1" ] && ok net-auto || no net-auto

ZREF=""
for i in 0 1 2 3 4 5 6 7 8 9; do
	[ "$(uci -q get firewall.@zone[$i].name)" = "wan" ] && { ZREF="@zone[$i]"; break; }
done
[ -n "$ZREF" ] && uci -q get "firewall.$ZREF.network" | tr ' ' '\n' | grep -qx usb_tether \
	&& ok zone-member || no zone-member

/usr/sbin/usb_tether_helper apply
[ -z "$(uci changes network; uci changes firewall)" ] && ok idempotent-apply || no idempotent-apply

# --- init reload 链（uci apply→procd→reload 的生产分发由 dev/ui-saveapply-test.py
#     用真实点击覆盖——CLI 无 rpcd 会话，ubus uci apply 直调必 Invalid argument）---
wait_reload() {
	local i ev av
	for i in $(seq 1 10); do
		ev="$(uci -q get usb-tether.config.enabled)"
		av="$(uci -q get network.usb_tether.auto)"
		[ "$ev" = "$1" ] && [ "$av" = "$2" ] && return 0
		sleep 1
	done
	return 1
}
[ -x /etc/init.d/usb-tether ] && ok init-script || no init-script
/etc/init.d/usb-tether enabled && ok init-enabled || no init-enabled
uci set usb-tether.config.enabled='0'
uci commit usb-tether
/etc/init.d/usb-tether reload
wait_reload 0 0 && ok init-reload-off || no init-reload-off
uci set usb-tether.config.enabled='1'
uci commit usb-tether
/etc/init.d/usb-tether reload
wait_reload 1 1 && ok init-reload-on || no init-reload-on

ip link add usb0 type veth peer name phone0 2>/dev/null || true
ip link set usb0 up
ip link set phone0 up
# phone0 的地址必须在 apply（network reload）之后补——netifd reload 会清掉裸 veth 上的地址（实测）
ip addr add 192.168.42.129/24 dev phone0 2>/dev/null || true
# 假手机入口放行：phone0 无 zone，fw4 默认丢弃入站 DHCP（真实方向 usb0∈wan 由
# 基座 Allow-DHCP-Renew 覆盖，无需产品侧改动）；fw4 reload 会重建规则表，故必须在
# 最后一次 firewall reload（即上面 apply）之后插入
nft insert rule inet fw4 input iifname "phone0" udp dport 67 accept comment "e2e-phone-dhcp" 2>/dev/null
dnsmasq --interface=phone0 --bind-interfaces \
	--dhcp-range=192.168.42.100,192.168.42.200,255.255.255.0,2m \
	--dhcp-option=option:router,192.168.42.129 --port=0 --pid-file=/tmp/e2e/dhcp.pid \
	>/tmp/e2e/dm.log 2>&1
UP=0
for i in $(seq 1 30); do
	if ifstatus usb_tether | grep -q '"up": *true'; then UP=1; break; fi
	sleep 1
done
[ "$UP" = 1 ] && ok phone-ifup || no phone-ifup
ifstatus usb_tether | grep -q '192\.168\.42\.' && ok lease-ip || no lease-ip

ip -4 route show default | grep 'dev usb0' | grep -q 'metric 50' \
	&& ok metric-route || { no metric-route; ip -4 route show default; }
ip -4 route show default | grep -q 'dev eth1' && ok wan-primary || no wan-primary

ip link set eth1 down
sleep 3
ip -4 route show default | grep -q 'dev usb0' && ip -4 route show default | grep -qv 'dev eth1' \
	&& ok failover-active || no failover-active
ip link set eth1 up
for i in $(seq 1 20); do
	ip -4 route show default | grep -q 'dev eth1' && break
	sleep 1
done
ip -4 route show default | grep -q 'dev eth1' && ok fallback-restore || no fallback-restore

logread | grep -q 'usb-tether' && ok hotplug-log || no hotplug-log

uclient-fetch -q -O /tmp/e2e/status.js http://127.0.0.1/luci-static/resources/view/usb-tether/status.js 2>/dev/null \
	&& grep -q renderContent /tmp/e2e/status.js && ok asset-status || no asset-status
uclient-fetch -q -O /tmp/e2e/setting.js http://127.0.0.1/luci-static/resources/view/usb-tether/setting.js 2>/dev/null \
	&& grep -q "require form" /tmp/e2e/setting.js && ok asset-setting || no asset-setting

/usr/sbin/usb_tether_helper disable
[ "$(uci -q get network.usb_tether.auto)" = "0" ] && ok disable-auto0 || no disable-auto0
uci -q get "firewall.$ZREF.network" | tr ' ' '\n' | grep -qx usb_tether && no disable-zone || ok disable-zone

# 复启：disable 置 enabled=0，须先显式置回 1 再 apply（helper 按当前 uci 语义同步）
uci set usb-tether.config.enabled='1'
uci commit usb-tether
/usr/sbin/usb_tether_helper apply
[ "$(uci -q get network.usb_tether.auto)" = "1" ] && ok reenable-auto1 || no reenable-auto1
uci -q get "firewall.$ZREF.network" | tr ' ' '\n' | grep -qx usb_tether \
	&& ok reenable-zone || no reenable-zone

/usr/sbin/usb_tether_helper cleanup
[ -z "$(uci -q get network.usb_tether)" ] && ok cleanup-net || no cleanup-net
uci -q get "firewall.$ZREF.network" | tr ' ' '\n' | grep -qx usb_tether && no cleanup-zone || ok cleanup-zone

echo "E2E:SUMMARY ok=$P fail=$F"
"""

kmod_lines = []
for km in os.environ.get("KMOD_NAMES", "").split():
	kmod_lines.append("uclient-fetch -q -O /tmp/e2e/kmod.apk http://192.168.1.2:%s/%s" % (srv_port, km))
	kmod_lines.append("apk add --allow-untrusted /tmp/e2e/kmod.apk >/tmp/e2e/kmod.log 2>&1 || true")

payload = "\n".join([
	"mkdir -p /tmp/e2e",
] + kmod_lines + [
	"uclient-fetch -q -O /tmp/e2e/app.apk http://192.168.1.2:%s/%s" % (srv_port, os.path.basename(os.environ["APP_APK"])),
	"uclient-fetch -q -O /tmp/e2e/i18n.apk http://192.168.1.2:%s/%s" % (srv_port, os.path.basename(os.environ["I18N_APK"])),
	"cat > /tmp/e2e/run.sh <<'RUNEOF'",
	inner, "RUNEOF",
	"sh /tmp/e2e/run.sh",
	"exit",
]) + "\n"

cmd = ["ssh", "-p", port,
       "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
       "-o", "PreferredAuthentications=password,none", "-o", "PubkeyAuthentication=no",
       "-o", "LogLevel=ERROR", "root@127.0.0.1"]

pid, fd = pty.fork()
if pid == 0:
	os.execvp(cmd[0], cmd)

buf = b""
sent_pass = 0
sent_payload = False
idle_after_auth = 0
deadline = time.time() + 300

def send(chunk):
	try:
		os.write(fd, chunk.encode() if isinstance(chunk, str) else chunk)
	except OSError:
		pass

def send_payload():
	# 逐行节流写入（canonical 模式下单行远小于 4096 缓冲；整体 ~4KB）
	for line in payload.splitlines(keepends=True):
		send(line)
		time.sleep(0.03)
	time.sleep(1)

while time.time() < deadline:
	r, _, _ = select.select([fd], [], [], 1)
	if not r:
		# 提示符未匹配但已静默 2 秒（MOTD/提示符变体）——兜底发车
		if not sent_payload and idle_after_auth >= 2 and b"ssh:" not in buf.lower():
			send_payload()
			sent_payload = True
		idle_after_auth += 1
		continue
	try:
		data = os.read(fd, 4096)
	except OSError:
		break
	if not data:
		break
	buf += data
	# 远程输出透传到 stdout（经外层 tee 落日志）
	sys.stdout.write(data.decode(errors="replace"))
	sys.stdout.flush()
	low = buf.lower()
	if b"password:" in low and sent_pass < len(passwords):
		send(passwords[sent_pass] + "\n")
		sent_pass += 1
		buf = b""
	elif not sent_payload and (low.rstrip().endswith(b"# ") or low.rstrip().endswith(b"$ ")):
		# 登录成功，整段 payload 逐行喂入
		send_payload()
		sent_payload = True
	idle_after_auth = 0

if not sent_payload:
	print("E2E:FAIL ssh-login（密码均不可用？VM_PASS=quartz 或先 passwd）")
	sys.exit(1)

sys.exit(0)
PYEOF

grep -c "^E2E:PASS" "$LOG" >/dev/null
FAILS=$(grep -c "^E2E:FAIL" "$LOG" || true)
echo
if [ "${FAILS:-0}" = "0" ] && grep -q "E2E:SUMMARY" "$LOG"; then
	echo "✅ VM e2e 全绿（$(grep -c '^E2E:PASS' "$LOG") 项）—— 日志: $LOG"
else
	echo "❌ VM e2e 有失败（$FAILS 项）—— 日志: $LOG"
	exit 1
fi

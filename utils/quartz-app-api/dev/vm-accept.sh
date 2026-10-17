#!/usr/bin/env bash
# quartz-app-api VM 验收（WP-5 / W5.5；DESIGN §8 六组 + 残面）
#
# 前置：VM 已启动且经宿主端口转发可达：
#   QuartzWrt-Builder/scripts/test-in-vm.sh --detach     # 8080→80 / 2222→22 / 8443→443
#   root 密码 quartz（VM_PASS 可覆盖）
#
# 用法: dev/vm-accept.sh          （全部组）
#       dev/vm-accept.sh --no-reboot   （跳过 system.reboot 破坏性末组）
set -uo pipefail

API="https://127.0.0.1:8443/ubus"
API_PLAIN="http://127.0.0.1:8080/ubus"
SID0="00000000000000000000000000000000"
VM_SSH="python3 $(cd "$(dirname "$0")/../../quartz-daemon/dev" && pwd)/vm-ssh.py"
NO_REBOOT=0
[ "${1:-}" = "--no-reboot" ] && NO_REBOOT=1

PASS=0; FAIL=0
ck() { # ck <name> <0|1>
	if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1));
	else echo "FAIL  $1"; FAIL=$((FAIL+1)); fi
}

call() { # call <sid> <object> <method> <args-json>
	curl -sk --max-time 15 "$API" -H 'Content-Type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$1\",\"$2\",\"$3\",$4]}"
}
call_plain() { # 同 call 但走主实例 80（残面验证）
	curl -s --max-time 15 "$API_PLAIN" -H 'Content-Type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$1\",\"$2\",\"$3\",$4]}"
}
jget() { # jget result 1 ok   （逐层取值，避免嵌套引号的 eval 坑）
	python3 -c '
import sys, json
d = json.load(sys.stdin)
for k in sys.argv[1:]:
    d = d[int(k)] if k.lstrip("-").isdigit() else d[k]
print(d)' "$@" 2>/dev/null
}

echo "== 0. 环境（HTTPS 443 / 证书 / mDNS 文件） =="
OUT=$(call "$SID0" quartz-app-api info '{}')
ck "info 可达（HTTPS 443）" $([ "$(echo "$OUT" | jget result 1 ok)" = "True" ] && echo 0 || echo 1)
NAME=$(echo "$OUT" | jget result 1 name)
echo "      router: $NAME"

echo "== 0b. 根口令前提（坑位 #28：未设口令时 rpcd 接受任意密码，认证断言无意义） =="
PWSET=$(echo "$OUT" | jget result 1 password_set)
if [ "$PWSET" != "True" ]; then
	HASH=$(openssl passwd -6 -salt "qz$(date +%s)" quartz)
	$VM_SSH "sed -i 's|^root:[^:]*:|root:$HASH:|' /etc/shadow" >/dev/null 2>&1
	sleep 1
	OUT=$(call "$SID0" quartz-app-api info '{}')
	PWSET=$(echo "$OUT" | jget result 1 password_set)
fi
ck "根口令已设置（password_set=true）" $([ "$PWSET" = "True" ] && echo 0 || echo 1)

OUT=$($VM_SSH 'ls -l /etc/uhttpd.crt /etc/umdns/quartz-app.json 2>&1; sha256sum /etc/uhttpd.crt 2>/dev/null; cat /etc/umdns/quartz-app.json 2>/dev/null; pgrep -x umdns >/dev/null && echo umdns-running')
FP_SHA=$(printf '%s\n' "$OUT" | sed -n 's/^\([0-9a-f]\{64\}\) .*/\1/p' | head -1)
ck "umdns 服务文件存在且含 _quartz-app._tcp" $(printf '%s' "$OUT" | grep -q '_quartz-app._tcp.local' && echo 0 || echo 1)
ck "umdns 进程运行中" $(printf '%s' "$OUT" | grep -q 'umdns-running' && echo 0 || echo 1)
ck "TXT fp 与 sha256(DER 证书) 一致" $(printf '%s' "$OUT" | grep -q "fp=$FP_SHA" && [ -n "$FP_SHA" ] && echo 0 || echo 1)
echo "      pin=$FP_SHA"

echo "== 1. ACL 权限边界（未授权对象必须被 rpcd 拒绝） =="
OUT=$(call "$SID0" file read '{"path":"/etc/openwrt_release"}')
ck "默认 sid 调 file.read → Access denied" $(printf '%s' "$OUT" | grep -q '\-32002' && echo 0 || echo 1)
OUT=$(call "$SID0" uci get '{"config":"quartz"}')
ck "默认 sid 调 uci.get → Access denied" $(printf '%s' "$OUT" | grep -q '\-32002' && echo 0 || echo 1)
OUT=$(call "$SID0" session access '{"scope":"ubus","object":"quartz-app-api","function":"info"}')
ck "默认 sid 对 quartz-app-api 有 ACL 授权" $(printf '%s' "$OUT" | grep -q '"access":true' && echo 0 || echo 1)
OUT=$(call_plain "$SID0" quartz-app-api info '{}')
ck "残面：主实例 80 亦可调（设计已声明，§6.4）" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)

echo "== 2. 认证（login / status / 篡改 / devices） =="
OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"definitely-wrong","device_name":"bad"}')
ck "错误密码 → invalid_credentials" $(printf '%s' "$OUT" | grep -q 'invalid_credentials' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-A"}')
TOK_A=$(echo "$OUT" | jget result 1 access_token)
REF_A=$(echo "$OUT" | jget result 1 refresh_token)
DEV_A=$(echo "$OUT" | jget result 1 device_id)
ck "正确密码 → 双令牌 + device_id" $([ -n "$TOK_A" ] && [ -n "$REF_A" ] && [ -n "$DEV_A" ] && echo 0 || echo 1)
ck "router 身份随 login 返回" $(printf '%s' "$OUT" | grep -q '"router"' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_A\"}")
ck "status → 快照 v=1（与 daemon 同数据模型）" $(printf '%s' "$OUT" | grep -q '"snapshot"' && printf '%s' "$OUT" | grep -q '"v":1' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"${TOK_A%??}AA\"}")
ck "篡改 access → token_invalid" $(printf '%s' "$OUT" | grep -q 'token_invalid' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api devices "{\"access_token\":\"$TOK_A\"}")
ck "devices 列表含 current" $(printf '%s' "$OUT" | grep -q '"current":true' && echo 0 || echo 1)

echo "== 3. 旋转 / 复用检测 / 吊销 / 登出 =="
OUT=$(call "$SID0" quartz-app-api refresh "{\"device_id\":\"$DEV_A\",\"refresh_token\":\"$REF_A\"}")
TOK_A2=$(echo "$OUT" | jget result 1 access_token)
REF_A2=$(echo "$OUT" | jget result 1 refresh_token)
ck "refresh 旋转换发（新旧均不同）" $([ -n "$TOK_A2" ] && [ "$REF_A2" != "$REF_A" ] && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api refresh "{\"device_id\":\"$DEV_A\",\"refresh_token\":\"$REF_A\"}")
ck "旧 refresh 复用 → device_revoked（复用检测）" $(printf '%s' "$OUT" | grep -q 'device_revoked' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_A2\"}")
ck "吊销后 access 立即失效（device_revoked）" $(printf '%s' "$OUT" | grep -q 'device_revoked' && echo 0 || echo 1)

OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-B"}')
TOK_B=$(echo "$OUT" | jget result 1 access_token)
DEV_B=$(echo "$OUT" | jget result 1 device_id)
OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-C"}')
TOK_C=$(echo "$OUT" | jget result 1 access_token)
DEV_C=$(echo "$OUT" | jget result 1 device_id)
OUT=$(call "$SID0" quartz-app-api revoke "{\"access_token\":\"$TOK_B\",\"device_id\":\"$DEV_C\"}")
ck "B 吊销 C → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_C\"}")
ck "C 被吊销后 status → device_revoked" $(printf '%s' "$OUT" | grep -q 'device_revoked' && echo 0 || echo 1)

OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-D"}')
TOK_D=$(echo "$OUT" | jget result 1 access_token)
REF_D=$(echo "$OUT" | jget result 1 refresh_token)
DEV_D=$(echo "$OUT" | jget result 1 device_id)
OUT=$(call "$SID0" quartz-app-api logout "{\"device_id\":\"$DEV_D\",\"refresh_token\":\"$REF_D\"}")
ck "logout → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_D\"}")
ck "登出后 access → token_invalid" $(printf '%s' "$OUT" | grep -q 'token_invalid' && echo 0 || echo 1)

echo "== 4. 限速（全局限定 5 次 → 锁定 + 退避） =="
for i in 1 2 3 4 5; do
	OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"wrong","device_name":"rl"}')
done
OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"quartz","device_name":"rl"}')
ck "连续失败 → rate_limited（附 retry_after）" $(printf '%s' "$OUT" | grep -q 'rate_limited' && printf '%s' "$OUT" | grep -q 'retry_after' && echo 0 || echo 1)
$VM_SSH 'rm -f /var/run/quartz-app-api/ratelimit.json' >/dev/null 2>&1
OUT=$(call "$SID0" quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-E"}')
TOK_E=$(echo "$OUT" | jget result 1 access_token)
ck "清限速状态后恢复登录（总键可复位）" $([ -n "$TOK_E" ] && [ "$TOK_E" != "None" ] && echo 0 || echo 1)

echo "== 5. 数据面降级（daemon 停机 → data_unavailable） =="
$VM_SSH '/etc/init.d/quartz-daemon stop' >/dev/null 2>&1
sleep 1
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_E\"}")
ck "daemon 停机 → data_unavailable" $(printf '%s' "$OUT" | grep -q 'data_unavailable' && echo 0 || echo 1)
$VM_SSH '/etc/init.d/quartz-daemon start' >/dev/null 2>&1
sleep 3
OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_E\"}")
ck "daemon 恢复 → status 正常" $(printf '%s' "$OUT" | grep -q '"snapshot"' && echo 0 || echo 1)

echo "== 6. 控制面（白名单 / 执行 / 拒否 / 依赖缺失） =="
OUT=$(call "$SID0" quartz-app-api action "{\"access_token\":\"$TOK_E\",\"action\":\"service.restart\",\"args\":{\"name\":\"dnsmasq\"}}")
ck "service.restart dnsmasq → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
OUT=$($VM_SSH 'sleep 1; pgrep -x dnsmasq >/dev/null && echo alive')
ck "dnsmasq 重启后存活（控制后可视化由 2s 快照承接）" $(printf '%s' "$OUT" | grep -q alive && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api action "{\"access_token\":\"$TOK_E\",\"action\":\"service.stop\",\"args\":{\"name\":\"dropbear\"}}")
ck "白名单外（dropbear）→ forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api action "{\"access_token\":\"$TOK_E\",\"action\":\"service.restart\",\"args\":{\"name\":\"openclash; rm -rf /\"}}")
ck "注入参数 → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)
OUT=$(call "$SID0" quartz-app-api action "{\"access_token\":\"$TOK_E\",\"action\":\"docker.container.restart\",\"args\":{\"id\":\"abc123\"}}")
ck "docker 未装 → action_failed（依赖缺失如实报错）" $(printf '%s' "$OUT" | grep -q 'action_failed' && echo 0 || echo 1)

echo "== 6b. bandix 写面转发项（WP-8 P2；DESIGN §5.1） =="
# 前置：数据面必须在线（脚本自包含——先确保 bandix 运行）
$VM_SSH 'pidof bandix >/dev/null || /etc/init.d/bandix start; sleep 2' >/dev/null 2>&1
ACTION() { # ACTION <token> <action-id> <args-json>
	call "$SID0" quartz-app-api action "{\"access_token\":\"$1\",\"action\":\"$2\",\"args\":$3}"
}
OUT=$(ACTION "$TOK_E" bandix.limits '{}')
ck "bandix.limits 读条目 → ok（含 whitelist 聚合）" $(printf '%s' "$OUT" | grep -q '"ok":true' && printf '%s' "$OUT" | grep -q 'whitelist' && echo 0 || echo 1)

OUT=$(ACTION "$TOK_E" bandix.limit_default_set '{"wan_rx_rate_limit":102400,"wan_tx_rate_limit":51200}')
ck "limit_default_set 写 → ok（数值参数 JSON number 直收）" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.limits '{}')
ck "写后回读一致（102400/51200）" $(printf '%s' "$OUT" | grep -q '102400' && printf '%s' "$OUT" | grep -q '51200' && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.limit_default_set '{"wan_rx_rate_limit":0,"wan_tx_rate_limit":0}')
ck "复位 0/0 → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)

TEST_MAC="02:00:00:00:00:01"
OUT=$(ACTION "$TOK_E" bandix.whitelist_add "{\"mac\":\"$TEST_MAC\"}")
ck "whitelist_add → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.limits '{}')
ck "白名单回读包含测试 MAC" $(printf '%s' "$OUT" | grep -qi "$TEST_MAC" && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.whitelist_delete "{\"mac\":\"$TEST_MAC\"}")
ck "whitelist_delete → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.limits '{}')
ck "白名单回读不含测试 MAC" $(printf '%s' "$OUT" | grep -qi "$TEST_MAC" && echo 1 || echo 0)

# 主机名绑定：取设备表第一台真设备做往返（设置 → 设备表回读轮询 ≤20s → 清除）
DEV_MAC=$($VM_SSH 'ubus call quartz.bandix status' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["data"]["d"][0]["mac"] if d.get("data",{}).get("d") else "")' 2>/dev/null)
if [ -n "$DEV_MAC" ]; then
	OUT=$(ACTION "$TOK_E" bandix.hostname_set "{\"mac\":\"$DEV_MAC\",\"hostname\":\"qze2e\"}")
	ck "hostname_set → ok（$DEV_MAC）" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
	hit=1
	for i in $(seq 1 10); do
		sleep 2
		OUT=$($VM_SSH 'ubus call quartz.bandix status')
		printf '%s' "$OUT" | grep -q '"qze2e"' && { hit=0; break; }
	done
	ck "设备表回读新主机名（轮询 ≤20s）" $hit
	OUT=$(ACTION "$TOK_E" bandix.hostname_set "{\"mac\":\"$DEV_MAC\",\"hostname\":\"\"}")
	ck "hostname_set 清绑定 → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
else
	echo "SKIP  hostname 往返（设备表为空）"
fi

OUT=$(ACTION "$TOK_E" bandix.whitelist_delete '{"mac":"not-a-mac"}')
ck "非法 MAC → bad_request（转发项校验）" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.installUpdate '{}')
ck "未注册动作（installUpdate）→ forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)
OUT=$(ACTION "$TOK_E" bandix.hostname_set '{"mac":"02:00:00:00:00:01","hostname":"x","evil":1}')
ck "未声明参数键 → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)

OUT=$(ACTION "$TOK_E" service.restart '{"name":"bandix"}')
ck "允许集：service.restart bandix → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
sleep 3
OUT=$($VM_SSH 'pidof bandix >/dev/null && echo alive')
ck "bandix 重启后存活" $(printf '%s' "$OUT" | grep -q alive && echo 0 || echo 1)

if [ "$NO_REBOOT" -eq 0 ]; then
	echo "== 7. system.reboot（末组：触发 → 重启 → 状态持久化复验） =="
	OUT=$(call "$SID0" quartz-app-api action "{\"access_token\":\"$TOK_E\",\"action\":\"system.reboot\"}")
	ck "system.reboot → ok" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
	echo "      等待重启（~90s）..."
	ok=1
	for i in $(seq 1 30); do
		sleep 5
		if curl -sk --max-time 3 "$API" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$SID0\",\"quartz-app-api\",\"info\",{}]}" 2>/dev/null | grep -q '"ok":true'; then ok=0; break; fi
	done
	ck "重启后 443 服务恢复" $ok
	OUT=$(call "$SID0" quartz-app-api status "{\"access_token\":\"$TOK_E\"}")
	ck "重启后设备注册表/令牌持久（access 仍有效）" $(printf '%s' "$OUT" | grep -q '"snapshot"' && echo 0 || echo 1)
	# 等 SSH 就绪（dropbear 可能略晚于 uhttpd——曾致本条误报）
	for i in $(seq 1 20); do
		$VM_SSH 'true' >/dev/null 2>&1 && break
		sleep 3
	done
	OUT=$($VM_SSH 'sha256sum /etc/uhttpd.crt | cut -d" " -f1')
	ck "重启后证书 pin 不变（DER 持久）" $(printf '%s' "$OUT" | grep -q "$FP_SHA" && echo 0 || echo 1)
fi

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" -eq 0 ]

#!/usr/bin/env bash
# 蜂窝轮（W6.12）VM 验收：daemon `cellular` 节空态 + app-api `qmodem.*` 转发条目链路。
#
# 前置：VM 已启动且宿主端口转发可达（test-in-vm.sh --detach；8080→80 / 2222→22 / 8443→443），
#       且已装入本轮的新包（quartz-daemon r4 / quartz-app-api r3）。
# 说明：**VM 无 5G 模组**（x86 镜像不预装 qmodem），故只能验：① 快照空态与契约；
#       ② 转发条目的校验/白名单/错误映射三段链路（用临时 uci section 模拟"有模组"）。
#       真数据（信号/拨号/短信/AT）必须在带模组的真机上验——见 W6.12 设计稿 §6。
set -uo pipefail

API="https://127.0.0.1:8443/ubus"
SID0="00000000000000000000000000000000"
HERE="$(cd "$(dirname "$0")" && pwd)"
VM_SSH="python3 $HERE/vm-ssh.py"
LOG="$HERE/accept-cellular-$(date +%Y%m%d-%H%M%S).log"
CK_TMP="$(mktemp)"

PASS=0; FAIL=0
ck() { # ck <name> <0|1>
	if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1))
	else echo "FAIL  $1"; FAIL=$((FAIL+1)); fi
}
call() { # call <object> <method> <args-json>
	curl -sk --max-time 15 "$API" -H 'Content-Type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$SID0\",\"$1\",\"$2\",$3]}"
}
act() { # act <token> <action> <args-json>  → 输出 <ok>|<code>|<message>
	call quartz-app-api action "{\"access_token\":\"$1\",\"action\":\"$2\",\"args\":$3}" | python3 -c '
import sys, json
try:
    p = json.load(sys.stdin)["result"][1]
except Exception:
    print("?|parse_error|"); raise SystemExit
err = p.get("error") or {}
print("%s|%s|%s" % (p.get("ok"), err.get("code", ""), (err.get("message") or "")[:70]))'
}

exec > >(tee "$LOG") 2>&1
echo "== 蜂窝轮 VM 验收（$(date -Iseconds)）=="

echo "== 1. daemon cellular 节（无模组 → 空态，契约通过） =="
$VM_SSH '/etc/init.d/quartz-daemon restart' >/dev/null 2>&1
sleep 3
$VM_SSH --fetch /var/run/quartz/status.json --out "$CK_TMP" 'true' >/dev/null 2>&1
ck "快照含 cellular 节" $(grep -q '"cellular"' "$CK_TMP" && echo 0 || echo 1)
ck "无 qmodem → available=false（tab 与卡片隐藏）" $(grep -q '"cellular":{"available":false}' "$CK_TMP" && echo 0 || echo 1)
if python3 "$HERE/contract-check.py" "$CK_TMP" >/dev/null 2>&1; then ck "契约校验器通过（§3.9 形状）" 0; else ck "契约校验器通过（§3.9 形状）" 1; fi
ck "无模组时零 ubus 调用（不报 error）" $(grep -q '"errors":\[\]' "$CK_TMP" && echo 0 || echo 1)

echo "== 2. 认证 =="
TOK=$(call quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-cellular"}' | python3 -c '
import sys, json
try: print(json.load(sys.stdin)["result"][1].get("access_token",""))
except Exception: print("")')
ck "登录取得 access_token" $([ -n "$TOK" ] && echo 0 || echo 1)

echo "== 3. 转发条目：未注册 / 白名单 / 校验 / 上游缺失 =="
OUT=$(act "$TOK" qmodem.scan_all '{}')
ck "未注册条目 qmodem.scan_all → forbidden_action" $(printf '%s' "$OUT" | grep -q '^False|forbidden_action' && echo 0 || echo 1)

# 临时造一个"模组"section：让 section 白名单生效（验完删除）
$VM_SSH 'touch /etc/config/qmodem; uci set qmodem.modem1=modem-device; uci commit qmodem' >/dev/null 2>&1
OUT=$(act "$TOK" qmodem.dial '{"config_section":"modem2"}')
ck "白名单外 section → bad_request" $(printf '%s' "$OUT" | grep -q '^False|bad_request' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.dial '{"config_section":"modem1"}')
ck "合法 section 但 qmodem 未装 → data_unavailable" $(printf '%s' "$OUT" | grep -q 'data_unavailable' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.send_at '{"config_section":"modem1","params":"{\"at\":\"ATI;reboot\",\"port\":\"/dev/ttyUSB2\"}"}')
ck "AT 注入负例 → bad_request" $(printf '%s' "$OUT" | grep -q '^False|bad_request' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.set_imei '{"config_section":"modem1","imei":"123"}')
ck "IMEI 位数负例 → bad_request" $(printf '%s' "$OUT" | grep -q '^False|bad_request' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.set_sim_slot '{"config_section":"modem1","slot":"3"}')
ck "SIM 槽位非枚举 → bad_request" $(printf '%s' "$OUT" | grep -q '^False|bad_request' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.at_cfg '{"config_section":"modem1","lang":"de"}')
ck "at_cfg 非法 lang → bad_request" $(printf '%s' "$OUT" | grep -q '^False|bad_request' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.at_cfg '{"config_section":"modem1","lang":"zh"}')
ck "at_cfg 合法 lang 通过校验（上游未装 → data_unavailable）" $(printf '%s' "$OUT" | grep -q 'data_unavailable' && echo 0 || echo 1)
OUT=$(act "$TOK" qmodem.sms_list '{"config_section":"modem1","limit":"20"}')
ck "短信对象条目（sectionKey=modem_id）链路可达（上游未装 → data_unavailable）" $(printf '%s' "$OUT" | grep -q 'data_unavailable' && echo 0 || echo 1)

echo "== 4. bandix 条目回归（同机制，改造后不得回归） =="
OUT=$(act "$TOK" bandix.limits '{}')
ck "bandix.limits 仍返回 ok=true" $(printf '%s' "$OUT" | grep -q '^True|' && echo 0 || echo 1)
OUT=$(act "$TOK" bandix.whitelist_add '{"mac":"zz:zz"}')
ck "bandix 参数校验仍拒非法 MAC → bad_request" $(printf '%s' "$OUT" | grep -q '^False|bad_request' && echo 0 || echo 1)

echo "== 5. 审计与清理 =="
OUT=$($VM_SSH 'logread | grep -c "fwd action=qmodem"' | tr -d '\r\n')
ck "qmodem 转发尝试落审计（logd 含 fwd action=qmodem，计 ${OUT:-0} 条）" $([ "${OUT:-0}" -ge 1 ] 2>/dev/null && echo 0 || echo 1)
$VM_SSH 'uci -q delete qmodem.modem1; uci commit qmodem; rm -f /etc/config/qmodem; killall -HUP rpcd' >/dev/null 2>&1
OUT=$($VM_SSH 'uci -q show qmodem | wc -l' | tr -d '\r\n')
ck "临时 section 已清理" $([ "${OUT:-0}" -eq 0 ] 2>/dev/null && echo 0 || echo 1)
rm -f "$CK_TMP"

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL（日志 $LOG）=="
[ "$FAIL" -eq 0 ]

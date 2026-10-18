#!/usr/bin/env bash
# 路由器 tab 后端验收（W6.10）：luci 启动项/时区转发条目 + **config.rollback**。
#
# 前置：VM 已启动；已装入本轮 quartz-app-api（含 `luci.*` 条目与 `config.rollback`）。
# 安全：配置改动只落 `system.@system[0].description`（无害字段），结束复原。
set -uo pipefail

API="https://127.0.0.1:8443/ubus"
SID0="00000000000000000000000000000000"
HERE="$(cd "$(dirname "$0")" && pwd)"
VM_SSH="python3 $(cd "$HERE/../../quartz-daemon/dev" && pwd)/vm-ssh.py"
LOG="$HERE/accept-router-$(date +%Y%m%d-%H%M%S).log"
PASS=0; FAIL=0
ck() { if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1)); else echo "FAIL  $1"; FAIL=$((FAIL+1)); fi; }
call() { curl -sk --max-time 20 "$API" -H 'Content-Type: application/json' \
	-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$SID0\",\"$1\",\"$2\",$3]}"; }
jget() { python3 -c '
import sys, json
d = json.load(sys.stdin)
try:
    p = d["result"][1]
except Exception:
    print(""); raise SystemExit
for k in sys.argv[1:]:
    d = d[int(k)] if k.lstrip("-").isdigit() else d.get(k)
    if d is None: break
print("" if d is None else d)' "$@"; }
vmv() { $VM_SSH "$1" 2>/dev/null | tr -d '\r\n'; }

exec > >(tee "$LOG") 2>&1
echo "== 路由器 tab 后端验收（$(date -Iseconds)）=="
ORIG=$(vmv 'uci -q get system.@system[0].description')
restore() {
	if [ -z "$ORIG" ]; then
		$VM_SSH 'uci -q delete system.@system[0].description; uci commit system' >/dev/null 2>&1
	else
		$VM_SSH "uci set system.@system[0].description='$ORIG'; uci commit system" >/dev/null 2>&1
	fi
}
trap restore EXIT

echo "== 1. 认证 =="
OUT=""
for i in 1 2 3 4 5 6; do
	OUT=$(call quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-router"}')
	printf '%s' "$OUT" | grep -q access_token && break
	sleep 2
done
TOK=$(printf '%s' "$OUT" | jget result 1 access_token)
DEVID=$(printf '%s' "$OUT" | jget result 1 device_id)
REFTOK=$(printf '%s' "$OUT" | jget result 1 refresh_token)
ck "登录成功" $([ -n "$TOK" ] && echo 0 || echo 1)

echo "== 2. luci 启动项（读/动作/负例） =="
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"luci.init_list\"}")
ck "init_list 返回启动项（含 cron/uhttpd）" $(printf '%s' "$OUT" | grep -q '"cron"' && printf '%s' "$OUT" | grep -q '"uhttpd"' && echo 0 || echo 1)
ck "init_list 形状（index/enabled）" $(printf '%s' "$OUT" | grep -q '"index"' && printf '%s' "$OUT" | grep -q '"enabled"' && echo 0 || echo 1)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"luci.init_action\",\"args\":{\"name\":\"cron\",\"action\":\"restart\"}}")
ck "init_action restart cron 成功" $(printf '%s' "$OUT" | grep -q '"ok":true' && echo 0 || echo 1)
sleep 1
OUT=$(vmv 'ubus call luci getInitList "{\"name\":\"cron\"}"')
ck "动作后 cron 仍为 enabled（读回）" $(printf '%s' "$OUT" | grep -q '"enabled": true' && echo 0 || echo 1)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"luci.init_action\",\"args\":{\"name\":\"../../etc/passwd\",\"action\":\"start\"}}")
ck "脚本名路径注入 → bad_request" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"luci.init_action\",\"args\":{\"name\":\"cron\",\"action\":\"rm\"}}")
ck "非法动作 → bad_request" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"luci.init_action\",\"args\":{\"name\":\"cron\",\"action\":\"start\",\"extra\":\"1\"}}")
ck "未声明参数 → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)

echo "== 3. luci 时区列表 =="
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"luci.timezones\"}")
ck "timezones 含 Asia/Shanghai 与 CST-8" $(printf '%s' "$OUT" | grep -q 'Asia/Shanghai' && printf '%s' "$OUT" | grep -q 'CST-8' && echo 0 || echo 1)

echo "== 4. config.rollback（apply 后立即回滚） =="
call quartz-app-api config.write "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"description\",\"value\":\"qz-rollback\"}" >/dev/null
OUT=$(call quartz-app-api config.apply "{\"access_token\":\"$TOK\",\"rollback\":true,\"timeout\":60}")
ck "apply(rollback,60s) 成功" $(printf '%s' "$OUT" | grep -q '"applied":true' && echo 0 || echo 1)
ck "apply 后描述已生效（qz-rollback）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "qz-rollback" ] && echo 0 || echo 1)
OUT=$(call quartz-app-api config.rollback "{\"access_token\":\"$TOK\"}")
ck "config.rollback 成功" $(printf '%s' "$OUT" | grep -q '"rolled_back":true' && echo 0 || echo 1)
sleep 1
if [ -z "$ORIG" ]; then
	NOW=$(vmv 'uci -q get system.@system[0].description')
	ck "立即回滚后恢复原值（空）" $([ -z "$NOW" ] && echo 0 || echo 1)
else
	ck "立即回滚后恢复原值（$ORIG）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "$ORIG" ] && echo 0 || echo 1)
fi
OUT=$(call quartz-app-api config.confirm "{\"access_token\":\"$TOK\"}")
ck "回滚后 confirm 不再改变状态（幂等/无待确认）" $(printf '%s' "$OUT" | grep -q '"ok":true' || printf '%s' "$OUT" | grep -q 'action_failed' && echo 0 || echo 1)

echo "== 5. 清理 =="
restore
ck "现场复原（description 回原值）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "$ORIG" ] && echo 0 || echo 1)
call quartz-app-api logout "{\"device_id\":\"$DEVID\",\"refresh_token\":\"$REFTOK\"}" >/dev/null 2>&1

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL（日志 $LOG）=="
[ "$FAIL" -eq 0 ]

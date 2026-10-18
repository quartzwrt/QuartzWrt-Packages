#!/usr/bin/env bash
# 配置面（W6.10 `config.*`）VM 验收：读/写/暂存/应用/回滚安全网/会话缺失/负例。
#
# 前置：VM 已启动且宿主端口转发可达；已装入本轮 `quartz-app-api`（含 `config.*`）。
# 安全：全部改动落在 `system.@system[0].description`（无害字段），且验证结束恢复原值。
set -uo pipefail

API="https://127.0.0.1:8443/ubus"
SID0="00000000000000000000000000000000"
HERE="$(cd "$(dirname "$0")" && pwd)"
VM_SSH="python3 $(cd "$HERE/../../quartz-daemon/dev" && pwd)/vm-ssh.py"
LOG="$HERE/accept-config-$(date +%Y%m%d-%H%M%S).log"
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
echo "== 配置面 VM 验收（$(date -Iseconds)）=="

echo "== 1. 认证与配置会话（login 时开启保留会话） =="
# rpcd 重载窗口：登录重试（最多 6 次 × 2s）
OUT=""
for i in 1 2 3 4 5 6; do
	OUT=$(call quartz-app-api login '{"username":"root","password":"quartz","device_name":"accept-config"}')
	printf '%s' "$OUT" | grep -q access_token && break
	sleep 2
done
TOK=$(printf '%s' "$OUT" | jget result 1 access_token)
DEVID=$(printf '%s' "$OUT" | jget result 1 device_id)
REFTOK=$(printf '%s' "$OUT" | jget result 1 refresh_token)
ck "登录成功（并开启配置会话）" $([ -n "$TOK" ] && echo 0 || echo 1)
SIDLEN=$(vmv 'grep -o "config_sid" /etc/quartz-app-api/state.json | wc -l')
ck "设备记录已持久化配置会话 sid（命中 ${SIDLEN}）" $([ "${SIDLEN:-0}" -ge 1 ] 2>/dev/null && echo 0 || echo 1)

echo "== 2. 读面（注册表白名单 + 敏感掩码语义） =="
EXP_HN=$(vmv 'uci -q get system.@system[0].hostname')
OUT=$(call quartz-app-api config.read "{\"access_token\":\"$TOK\",\"config\":\"system\"}")
ck "读 system 全量（含真实 hostname=$EXP_HN）" $(printf '%s' "$OUT" | grep -q "\"hostname\":\"$EXP_HN\"" && echo 0 || echo 1)
EXP_IP=$(vmv 'uci -q get network.lan.ipaddr')
EXP_IP_BASE=${EXP_IP%%/*}
OUT=$(call quartz-app-api config.read "{\"access_token\":\"$TOK\",\"config\":\"network\",\"section\":\"lan\",\"option\":\"ipaddr\"}")
ck "读 network.lan.ipaddr（含真实值 $EXP_IP_BASE）" $(printf '%s' "$OUT" | grep -q "$EXP_IP_BASE" && echo 0 || echo 1)
OUT=$(call quartz-app-api config.read "{\"access_token\":\"$TOK\",\"config\":\"firewall\"}")
ck "未注册配置 firewall → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)
OUT=$(call quartz-app-api config.read "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"rootpw\"}")
ck "未注册选项 → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)

echo "== 3. 写（暂存）+ 负例 =="
ORIG=$(vmv 'uci -q get system.@system[0].description')
OUT=$(call quartz-app-api config.write "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"description\",\"value\":\"qz-accept\"}")
ck "写入 description 返回待应用改动" $(printf '%s' "$OUT" | grep -q '"changes"' && echo 0 || echo 1)
ck "真实配置未被修改（暂存语义）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "$ORIG" ] && echo 0 || echo 1)
OUT=$(call quartz-app-api config.write "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"hostname\",\"value\":\"bad;name\"}")
ck "非法字符值（hostname 含分号）→ bad_request" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)
OUT=$(call quartz-app-api config.write "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"rootpw\",\"value\":\"x\"}")
ck "未注册选项写入 → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)

echo "== 4. 应用 + 回滚安全网（不确认 → 自动回滚） =="
OUT=$(call quartz-app-api config.apply "{\"access_token\":\"$TOK\",\"rollback\":true,\"timeout\":10}")
ck "apply(rollback,10s) 成功" $(printf '%s' "$OUT" | grep -q '"applied":true' && echo 0 || echo 1)
ck "apply 后真实配置生效（description=qz-accept）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "qz-accept" ] && echo 0 || echo 1)
sleep 14
ck "10s 超时未确认 → 自动回滚到原值" $([ "$(vmv 'uci -q get system.@system[0].description')" = "$ORIG" ] && echo 0 || echo 1)

echo "== 5. 应用 + 确认（落盘） =="
call quartz-app-api config.write "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"description\",\"value\":\"qz-accept2\"}" >/dev/null
call quartz-app-api config.apply "{\"access_token\":\"$TOK\",\"rollback\":true,\"timeout\":30}" >/dev/null
OUT=$(call quartz-app-api config.confirm "{\"access_token\":\"$TOK\"}")
ck "confirm 成功" $(printf '%s' "$OUT" | grep -q '"confirmed":true' && echo 0 || echo 1)
sleep 3
ck "确认后值保留（qz-accept2）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "qz-accept2" ] && echo 0 || echo 1)
# 复原（写回原值 → apply 不带回滚）
call quartz-app-api config.write "{\"access_token\":\"$TOK\",\"config\":\"system\",\"section\":\"@system[0]\",\"option\":\"description\",\"value\":\"$ORIG\"}" >/dev/null
call quartz-app-api config.apply "{\"access_token\":\"$TOK\",\"rollback\":false}" >/dev/null
ck "现场复原（description 回原值）" $([ "$(vmv 'uci -q get system.@system[0].description')" = "$ORIG" ] && echo 0 || echo 1)

echo "== 6. 会话缺失（logout 销毁会话 → 配置写入需重新登录） =="
call quartz-app-api logout "{\"device_id\":\"$DEVID\",\"refresh_token\":\"$REFTOK\"}" >/dev/null 2>&1
STILL=$(vmv "grep -c '$DEVID' /etc/quartz-app-api/state.json")
ck "logout 后本设备记录已移除（sid 随之清除）" $([ "${STILL:-1}" = "0" ] && echo 0 || echo 1)

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL（日志 $LOG）=="
[ "$FAIL" -eq 0 ]

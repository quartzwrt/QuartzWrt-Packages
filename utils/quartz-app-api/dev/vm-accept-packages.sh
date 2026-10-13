#!/usr/bin/env bash
# quartz-app-api 软件包管理面 VM 验收（WP-5 扩展；DESIGN §4.5/§8 补充组）
#
# 前置：
#   QuartzWrt-Builder/scripts/test-in-vm.sh --detach   # 8080→80 / 2222→22 / 8443→443
#   root 密码 quartz（VM_PASS 可覆盖）
#
# 测试源（可切换 stage，模拟「上游发布新版本」的真实升级路径）：
#   dev/testrepo/serve.py 由本脚本自动拉起（端口 9990；已在内网可达即可）。
#   VM 经 QEMU 用户网络访问宿主：192.168.1.2:9990（LAN 侧）/ 10.0.2.2（WAN 侧）。
#
# 用法: dev/vm-accept-packages.sh
set -uo pipefail

API="https://127.0.0.1:8443/ubus"
SID0="00000000000000000000000000000000"
DEV_DIR="$(cd "$(dirname "$0")" && pwd)"
VM_SSH="python3 $(cd "$DEV_DIR/../../quartz-daemon/dev" && pwd)/vm-ssh.py"
REPO_HOST="${QZ_REPO_HOST:-192.168.1.2}"
REPO_PORT="${QZ_REPO_PORT:-9990}"
REPO_URL="http://$REPO_HOST:$REPO_PORT/x86_64/packages.adb"

PASS=0; FAIL=0
ck() { # ck <name> <0|1>
	if [ "$2" -eq 0 ]; then echo "PASS  $1"; PASS=$((PASS+1));
	else echo "FAIL  $1"; FAIL=$((FAIL+1)); fi
}

call() { # call <object> <method> <args-json>
	curl -sk --max-time 65 "$API" -H 'Content-Type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"call\",\"params\":[\"$SID0\",\"$1\",\"$2\",$3]}"
}
jget() { # jget result 1 items 0 name ...
	python3 -c '
import sys, json
d = json.load(sys.stdin)
for k in sys.argv[1:]:
    d = d[int(k)] if k.lstrip("-").isdigit() else d[k]
print(d)' "$@" 2>/dev/null
}
vsh() { # vsh <远端命令>（剥 \r，pty 输出）
	$VM_SSH "$1" 2>/dev/null | tr -d '\r'
}
stage() { # stage <stage1|stage2>
	curl -s --max-time 5 "http://127.0.0.1:$REPO_PORT/swap/$1" >/dev/null 2>&1
	# 等 apk 侧可见（同 URL，本步只为脚本自证）
	curl -s --max-time 5 "http://127.0.0.1:$REPO_PORT/x86_64/packages.adb" >/dev/null 2>&1
}

echo "== -1. 测试源（serve.py；可切换 stage1=仅 1.0.0 / stage2=含 1.0.1） =="
if ! curl -s --max-time 3 "http://127.0.0.1:$REPO_PORT/x86_64/packages.adb" >/dev/null 2>&1; then
	echo "      拉起 dev/testrepo/serve.py (:$REPO_PORT)…"
	nohup python3 "$DEV_DIR/testrepo/serve.py" --port "$REPO_PORT" >/tmp/qz-testrepo.log 2>&1 &
	sleep 1
fi
ck "测试源可达（$REPO_URL）" $(curl -s --max-time 3 "http://127.0.0.1:$REPO_PORT/x86_64/packages.adb" >/dev/null 2>&1 && echo 0 || echo 1)

echo "== 0. 前置（登录 / 测试包按名安装 → world 无钉） =="
OUT=$(call quartz-app-api info '{}')
PWSET=$(echo "$OUT" | jget result 1 password_set)
if [ "$PWSET" != "True" ]; then
	HASH=$(openssl passwd -6 -salt "qz$(date +%s)" quartz)
	vsh "sed -i 's|^root:[^:]*:|root:$HASH:|' /etc/shadow" >/dev/null 2>&1
	sleep 1
	OUT=$(call quartz-app-api info '{}')
	PWSET=$(echo "$OUT" | jget result 1 password_set)
fi
ck "根口令已设置（坑位 #28 前提）" $([ "$PWSET" = "True" ] && echo 0 || echo 1)

OUT=$(call quartz-app-api login '{"username":"root","password":"quartz","device_name":"pkg-accept"}')
TOK=$(echo "$OUT" | jget result 1 access_token)
ck "login → access_token" $([ -n "$TOK" ] && [ "$TOK" != "None" ] && echo 0 || echo 1)

stage stage1
vsh "
# 环境前置（已知产品缺陷，见 WORKPLAN 记录）：出厂 distfeeds 里 quartz/thirdparty/video
# 三条 feed 指向 VERSION_REPO 下不存在的路径（feed 未发布）——apk update 标 unavailable，
# 且 apk upgrade 会直接拒绝（'stale/unavailable repositories'），add/del 不受影响。
# 此处按「设备管理员排障口径」移除不可达 feed 后再验收；真实修复归 Builder/WP-9。
sed -i '/x86_64\/quartz\//d; /x86_64\/thirdparty\//d; /x86_64\/video\//d' /etc/apk/repositories.d/distfeeds.list 2>/dev/null
wget -q -O /etc/apk/keys/qz-test.pem http://$REPO_HOST:$REPO_PORT/qz-test.pem
echo '$REPO_URL' > /etc/apk/repositories.d/qz-test.list
apk del quartz-pkgtest >/dev/null 2>&1
apk update >/dev/null 2>&1
apk add quartz-pkgtest >/dev/null 2>&1" >/dev/null
W=$(vsh "grep quartz-pkgtest /etc/apk/world")
IV=$(vsh 'apk list -I quartz-pkgtest 2>/dev/null | head -1')
ck "测试包按名安装（world=裸名无钉、实装 1.0.0-r1）" \
	$(printf '%s' "$W" | grep -qx 'quartz-pkgtest' && printf '%s' "$IV" | grep -q 'quartz-pkgtest-1.0.0-r1' && echo 0 || echo 1)

echo "== 1. packages 读方法（认证 / 分页 / scope / 搜索） =="
OUT=$(call quartz-app-api packages '{"scope":"installed"}')
ck "无令牌 → token_invalid" $(printf '%s' "$OUT" | grep -q 'token_invalid' && echo 0 || echo 1)

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"installed\",\"per_page\":5}")
NR=$(echo "$OUT" | jget result 1 total)
NP=$(echo "$OUT" | jget result 1 pages)
N0=$(echo "$OUT" | jget result 1 items 0 name)
NI=$(echo "$OUT" | jget result 1 items 0 installed)
ck "installed 分页（total>=340, pages=ceil(total/5), item.installed=true）" \
	$([ "${NR:-0}" -ge 340 ] && [ "$NP" -eq $(( (NR + 4) / 5 )) ] && [ -n "$N0" ] && [ "$NI" = "True" ] && echo 0 || echo 1)
echo "      total=$NR pages=$NP first=$N0"

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"installed\",\"per_page\":5,\"page\":3}")
P3=$(echo "$OUT" | jget result 1 page)
ck "page 回显（page=3）" $([ "$P3" = "3" ] && echo 0 || echo 1)

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"installed\",\"per_page\":200}")
PP=$(echo "$OUT" | jget result 1 per_page)
ck "per_page 上限夹紧（200→100）" $([ "$PP" = "100" ] && echo 0 || echo 1)

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"nonsense\"}")
ck "未知 scope → bad_request" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"available\",\"query\":\"tail\",\"per_page\":100}")
NT=$(echo "$OUT" | jget result 1 total)
ck "available 子串搜索（tail→命中含 tailscale）" \
	$([ "${NT:-0}" -ge 2 ] && printf '%s' "$OUT" | grep -q 'tailscale' && echo 0 || echo 1)
echo "      available(tail) total=$NT"

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"available\",\"query\":\"a*b\"}")
ck "搜索词非法字符（apk 模式通配）→ bad_request" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)

echo "== 2. upgradable 语义（上游发布新版 → 版本 diff 数据面） =="
stage stage2
vsh "apk update >/dev/null 2>&1" >/dev/null
OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"upgradable\",\"query\":\"quartz-pkgtest\"}")
UV=$(echo "$OUT" | jget result 1 items 0 version)
IV2=$(echo "$OUT" | jget result 1 items 0 installed_version)
UG=$(echo "$OUT" | jget result 1 items 0 upgradable)
ck "upgradable 含 1.0.0-r1 → 1.0.1-r1（target/installed/upgradable 三字段）" \
	$([ "$UV" = "1.0.1-r1" ] && [ "$IV2" = "1.0.0-r1" ] && [ "$UG" = "True" ] && echo 0 || echo 1)

OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"installed\",\"query\":\"quartz-pkgtest\"}")
UG2=$(echo "$OUT" | jget result 1 items 0 upgradable)
TV=$(echo "$OUT" | jget result 1 items 0 target_version)
ck "installed 范围内同项带 upgradable + target_version" $([ "$UG2" = "True" ] && [ "$TV" = "1.0.1-r1" ] && echo 0 || echo 1)

echo "== 3. 动作（refresh / upgrade 全部与单包 / remove / 拒否） =="
T0=$(date +%s)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.refresh\"}")
T1=$(date +%s)
ck "packages.refresh → ok（含 duration_ms）" $(printf '%s' "$OUT" | grep -q '"ok":true' && printf '%s' "$OUT" | grep -q 'duration_ms' && echo 0 || echo 1)
echo "      refresh 耗时 $((T1 - T0))s（uhttpd /ubus 单调用窗口 60s 内）"

OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.upgrade\",\"args\":{}}")
OUTV=$(vsh 'cat /usr/share/quartz-pkgtest/version')
ck "packages.upgrade（全部）→ ok 且实装 1.0.1-r1（真执行）" \
	$(printf '%s' "$OUT" | grep -q '"ok":true' && [ "$OUTV" = "1.0.1-r1" ] && echo 0 || echo 1)

# 单包升级：回到旧版再升（复用 stage 切换，world 保持裸名）
stage stage1
vsh "apk del quartz-pkgtest >/dev/null 2>&1; apk update >/dev/null 2>&1; apk add quartz-pkgtest >/dev/null 2>&1" >/dev/null
stage stage2
vsh "apk update >/dev/null 2>&1" >/dev/null
BEFORE=$(vsh 'cat /usr/share/quartz-pkgtest/version')
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.upgrade\",\"args\":{\"name\":\"quartz-pkgtest\"}}")
OUTV=$(vsh 'cat /usr/share/quartz-pkgtest/version')
ck "packages.upgrade name=…（单包，$BEFORE→$OUTV）→ ok" \
	$(printf '%s' "$OUT" | grep -q '"ok":true' && [ "$BEFORE" = "1.0.0-r1" ] && [ "$OUTV" = "1.0.1-r1" ] && echo 0 || echo 1)

OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.remove\",\"args\":{\"name\":\"quartz-pkgtest\"}}")
LEFT=$(vsh 'apk list -I quartz-pkgtest 2>/dev/null | wc -l')
ck "packages.remove quartz-pkgtest → ok 且已卸载" \
	$(printf '%s' "$OUT" | grep -q '"ok":true' && [ "$LEFT" = "0" ] && echo 0 || echo 1)

OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.remove\",\"args\":{\"name\":\"quartz-pkgtest; rm -rf /\"}}")
ck "注入式包名 → forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.remove\",\"args\":{}}")
ck "remove 缺 name → bad_request" $(printf '%s' "$OUT" | grep -q 'bad_request' && echo 0 || echo 1)
OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.install\",\"args\":{\"name\":\"nano\"}}")
ck "未注册动作 packages.install（v1 后置）→ forbidden_action" $(printf '%s' "$OUT" | grep -q 'forbidden_action' && echo 0 || echo 1)

OUT=$(call quartz-app-api action "{\"access_token\":\"$TOK\",\"action\":\"packages.remove\",\"args\":{\"name\":\"no-such-pkg-xyz\"}}")
ck "卸载不存在包 → action_failed（附 apk 错误摘录）" \
	$(printf '%s' "$OUT" | grep -q 'action_failed' && printf '%s' "$OUT" | grep -q 'no-such-pkg-xyz' && echo 0 || echo 1)

echo "== 4. 操作记录（history scope，结构化近 30 条） =="
OUT=$(call quartz-app-api packages "{\"access_token\":\"$TOK\",\"scope\":\"history\"}")
NH=$(echo "$OUT" | jget result 1 total)
H0=$(echo "$OUT" | jget result 1 items 0 action)
H0T=$(echo "$OUT" | jget result 1 items 0 target)
H0OK=$(echo "$OUT" | jget result 1 items 0 ok)
H0TS=$(echo "$OUT" | jget result 1 items 0 ts)
ck "history 有条目且最新在前（显式字段 action/target/ok/ts）" \
	$([ "${NH:-0}" -ge 3 ] && [ "$H0" = "packages.remove" ] && [ "$H0OK" = "False" ] && [ "${H0TS:-0}" -gt 0 ] && echo 0 || echo 1)
echo "      history total=$NH latest=${H0} target=${H0T} ok=${H0OK}"
ck "history 含成功与失败两类（升级记录存在）" \
	$(printf '%s' "$OUT" | grep -q '"action":"packages.upgrade"' && printf '%s' "$OUT" | grep -q '"ok":false' && echo 0 || echo 1)

OUT=$(vsh 'ls -l /etc/quartz-app-api/history.json 2>/dev/null')
ck "history.json 落盘（0600）" $(printf '%s' "$OUT" | grep -q -- '-rw-------' && echo 0 || echo 1)

echo "== 5. ACL 面（packages 方法在授权内，其余对象仍拒） =="
OUT=$(call session access '{"scope":"ubus","object":"quartz-app-api","function":"packages"}')
ck "默认 sid 对 packages 有 ACL 授权" $(printf '%s' "$OUT" | grep -q '"access":true' && echo 0 || echo 1)
OUT=$(call file read '{"path":"/etc/passwd"}')
ck "对照：file.read 仍 Access denied" $(printf '%s' "$OUT" | grep -q '\-32002' && echo 0 || echo 1)

echo
echo "== 汇总: PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" -eq 0 ]

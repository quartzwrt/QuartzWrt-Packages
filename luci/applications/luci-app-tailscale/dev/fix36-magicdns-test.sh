#!/bin/sh
# #36 v5 验证（VM）：MagicDNS 胶水修正
#   address=（直接应答）→ server=（转发）+ rebind_domain 豁免（含 CGNAT 拦截联动）
#   覆盖：A/B 对照、旧条目迁移、多值列表成员判定、幂等、cleanup 零残留、现场复原。
# 仿真：Quad100 由外部解析器（替身）独家持有=tailscaled 生产角色；测试窗口内主 dnsmasq 临时
# 排除 lo（消除与替身的绑定竞态），测试后复原。
SUF=tailXXXX.ts.net
NAME=quartzwrt-vm.$SUF
WANT_MD5="cf30ed9b018be86cb17c45b2e2fa54b3"
PASS=0; FAIL=0
ok(){ PASS=$((PASS+1)); echo "  [PASS] $*"; }
bad(){ FAIL=$((FAIL+1)); echo "  [FAIL] $*"; }
hasline(){ echo "$1" | grep "$2" | grep -qF "$3"; }
ckline(){ if hasline "$2" "$3" "$4"; then ok "$1"; else bad "$1（实际：$2）"; fi; }
md5of(){ md5sum "$1" 2>/dev/null | cut -d' ' -f1; }
suffix_hits(){ if [ -f /tmp/standin.log ]; then grep -c "$NAME" /tmp/standin.log; else echo 0; fi; }
wait_dns(){ i=0; while [ $i -lt 25 ]; do nslookup www.baidu.com 192.168.1.1 >/dev/null 2>&1 && return 0; sleep 1; i=$((i+1)); done; return 1; }

echo "### #36 v5：address=→server=（+rebind_domain）验证 ###"
echo "== 环境 =="
uname -a | cut -c1-80
apk list -I 2>/dev/null | grep luci-app-tailscale
MD5=$(md5of /usr/sbin/tailscale_helper)
[ "$MD5" = "$WANT_MD5" ] && ok "安装的 helper 即修复版（md5=$MD5）" || bad "helper md5 不符（$MD5 != $WANT_MD5）"
DPID=$(pgrep -f "^/usr/sbin/dnsmasq -C" | head -1)
[ -n "$DPID" ] || { bad "主 dnsmasq 未运行"; echo "### FAIL-EXIT ###"; exit 1; }
echo "  主 dnsmasq pid=$DPID"
uci show dhcp > /tmp/dhcp.before 2>/dev/null

echo
echo "== Phase 0：仿真 tailscaled 独家持有 Quad100（主 dnsmasq 临时排除 lo 消除竞态）=="
uci add_list dhcp.@dnsmasq[0].notinterface='lo'
uci commit dhcp
/etc/init.d/dnsmasq reload >/dev/null 2>&1
wait_dns || bad "排除 lo 后 dnsmasq 未就绪"
grep -q "except-interface=lo" /var/etc/dnsmasq.conf.* 2>/dev/null && ok "配置已含 except-interface=lo" || bad "except-interface=lo 未生效"
ip addr add 100.100.100.100/32 dev lo 2>/dev/null
dnsmasq --conf-file=/dev/null --port=53 --listen-address=100.100.100.100 --bind-interfaces \
	--no-resolv --no-hosts --address=/$NAME/100.64.123.45 --address=/$NAME/fd7a:115c:a1e0::1 \
	--log-queries --log-facility=/tmp/standin.log --pid-file=/tmp/standin.pid
sleep 1
SPID=$(cat /tmp/standin.pid 2>/dev/null)
if [ -z "$SPID" ] || ! kill -0 "$SPID" 2>/dev/null; then
	bad "替身未能启动"; netstat -lnup 2>/dev/null | grep "100.100.100.100" | sed 's/^/  /'
	echo "### FAIL-EXIT ###"; exit 1
fi
LINES=$(netstat -lnup 2>/dev/null | grep -c "100.100.100.100:53")
OWNER=$(netstat -lnup 2>/dev/null | grep "100.100.100.100:53" | awk '{print $NF}' | head -1)
[ "$LINES" = "1" ] && [ "$OWNER" = "$SPID/dnsmasq" ] \
	&& ok "Quad100:53 唯一属主=替身（pid=$SPID）" \
	|| { netstat -lnup 2>/dev/null | grep "100.100.100.100" | sed 's/^/  /'; bad "Quad100:53 属主异常（行数=$LINES owner=$OWNER）"; }
pgrep -f "^/usr/sbin/dnsmasq -C" >/dev/null && ok "主 dnsmasq 存活" || bad "主 dnsmasq 已退出"
wait_dns && ok "主 dnsmasq 正常服务 LAN" || bad "主 dnsmasq 未服务 LAN"
DOUT=$(nslookup $NAME 100.100.100.100 2>&1); echo "直查替身："; echo "$DOUT"
ckline "直查替身得到 100.64.123.45（替身独家应答）" "$DOUT" "Address" "100.64.123.45"

echo
echo "== Phase 1（A/B 之 A）：旧写法 address= —— 直接应答、不转发（缺陷复现）=="
H0=$(suffix_hits)
uci add_list dhcp.@dnsmasq[0].address="/$SUF/100.100.100.100"
uci commit dhcp
/etc/init.d/dnsmasq reload >/dev/null 2>&1
wait_dns || bad "reload 后 dnsmasq 未就绪"
echo "生成配置条目："; grep -hn "$SUF" /var/etc/dnsmasq.conf.* 2>/dev/null
OUT=$(nslookup $NAME 192.168.1.1 2>&1); echo "LAN 客户端查询（VM→192.168.1.1）："; echo "$OUT"
ckline "A：旧写法得到 100.100.100.100（原缺陷复现）" "$OUT" "Address" "100.100.100.100"
H1=$(suffix_hits)
[ "$H1" = "$H0" ] && ok "A：替身零命中（address= 从不转发；$H0→$H1）" || bad "A：替身被查询（$H0→$H1）"

echo
echo "== Phase 2：升级迁移（修复版 magicdns_reconcile，源自在装文件）=="
uci set tailscale.settings._ts_dns_suffix="$SUF"; uci commit tailscale
sed '/^main "\$@"$/d' /usr/sbin/tailscale_helper > /tmp/h.sh
. /tmp/h.sh
TS=/tmp/ts-stub
load_settings
echo "  accept_dns=$accept_dns"
magicdns_reconcile; echo "  reconcile rc=$?"
echo "  _ts_dns_suffix=$(uci -q get tailscale.settings._ts_dns_suffix)"
uci show dhcp | grep -E "\.(address|server|rebind_domain)" | sed 's/^/  /'
if uci -q get dhcp.@dnsmasq[0].address 2>/dev/null | tr ' ' '\n' | grep -qxF "/$SUF/100.100.100.100"; then bad "迁移：address= 残留"; else ok "迁移：旧 address= 条目已清除"; fi
ckline "迁移：server= 条目已写入" "$(uci show dhcp)" "server" "$SUF/100.100.100.100"
ckline "迁移：rebind_domain 豁免已写入" "$(uci show dhcp)" "rebind_domain" "/$SUF/"
wait_dns || bad "reconcile 后 dnsmasq 未就绪"
echo "生成的 dnsmasq 配置："; grep -hn "$SUF" /var/etc/dnsmasq.conf.* 2>/dev/null
ckline "配置翻译为 server=" "$(grep -h "$SUF" /var/etc/dnsmasq.conf.* 2>/dev/null)" "server=" "$SUF/100.100.100.100"
ckline "配置含 rebind-domain-ok 豁免" "$(grep -h "$SUF" /var/etc/dnsmasq.conf.* 2>/dev/null)" "rebind-domain-ok" "/$SUF/"
OUT=$(nslookup $NAME 192.168.1.1 2>&1); echo "LAN 客户端查询："; echo "$OUT"
ckline "B：修复后解析出真 tailnet IP 100.64.123.45（经转发+rebind 豁免）" "$OUT" "Address" "100.64.123.45"
H2=$(suffix_hits)
[ "$H2" -gt "$H1" ] && ok "B：替身收到转发查询（$H1→$H2）" || {
	bad "B：替身未被查询（server= 未转发？$H1→$H2）"
	logread 2>/dev/null | grep -i "nameserver\|ignoring" | tail -5 | sed 's/^/  diag: /'
}

echo
echo "== Phase 3：幂等（二跑零漂移）=="
uci show dhcp > /tmp/dhcp.1
( . /tmp/h.sh; TS=/tmp/ts-stub; load_settings; magicdns_reconcile ) >/dev/null 2>&1
uci show dhcp > /tmp/dhcp.2
if [ "$(md5of /tmp/dhcp.1)" = "$(md5of /tmp/dhcp.2)" ]; then ok "二跑 uci 零漂移"; else bad "二跑出现漂移"; fi

echo
echo "== Phase 4：后缀变化 + 多值列表（旧后缀清、新条目不得重复）=="
uci set tailscale.settings._ts_dns_suffix="old.example.net"
uci add_list dhcp.@dnsmasq[0].server="/old.example.net/100.100.100.100"
uci add_list dhcp.@dnsmasq[0].rebind_domain="/old.example.net/"
uci commit dhcp; uci commit tailscale
( . /tmp/h.sh; TS=/tmp/ts-stub; load_settings; magicdns_reconcile ) >/dev/null 2>&1
uci show dhcp | grep -E "\.(address|server|rebind_domain)" | sed 's/^/  /'
if uci show dhcp | grep -q "old.example.net"; then bad "旧后缀条目残留"; else ok "旧后缀条目被清（server+rebind_domain）"; fi
[ "$(uci -q get tailscale.settings._ts_dns_suffix)" = "$SUF" ] && ok "后缀键更新为新值" || bad "后缀键未更新"
N=$(uci -q get dhcp.@dnsmasq[0].server | tr ' ' '\n' | grep -cxF "/$SUF/100.100.100.100")
[ "$N" = "1" ] && ok "server 条目恰好 1 份（多值列表下无重复添加）" || bad "server 条目 $N 份（应为 1）"
R=$(uci -q get dhcp.@dnsmasq[0].rebind_domain | tr ' ' '\n' | grep -cxF "/$SUF/")
[ "$R" = "1" ] && ok "rebind_domain 条目恰好 1 份" || bad "rebind_domain 条目 $R 份（应为 1）"

echo
echo "== Phase 5：cleanup 零残留（真实安装文件，离线路径）=="
echo "-- 5a：address= 残留 + server=（含重复值）+ rebind_domain 并存 → cleanup"
uci add_list dhcp.@dnsmasq[0].address="/$SUF/100.100.100.100"
uci add_list dhcp.@dnsmasq[0].server="/$SUF/100.100.100.100"
uci commit dhcp
/usr/sbin/tailscale_helper cleanup; echo "  cleanup rc=$?"
wait_dns || bad "cleanup 后 dnsmasq 未就绪"
LEFT=$(uci show dhcp 2>/dev/null | grep -c "100.100.100.100")
[ "$LEFT" = "0" ] && ok "cleanup 后 server/address 零残留（重复值全清）" || { bad "残留 $LEFT 行"; uci show dhcp | grep 100.100.100.100 | sed 's/^/  /'; }
if uci show dhcp | grep -q "$SUF"; then bad "rebind_domain 等仍有残留"; else ok "cleanup 后 uci 零残留（含 rebind_domain）"; fi
[ -z "$(uci -q get tailscale.settings._ts_dns_suffix)" ] && ok "_ts_dns_suffix 已清" || bad "_ts_dns_suffix 残留"
if grep -q "$SUF" /var/etc/dnsmasq.conf.* 2>/dev/null; then bad "生成配置仍含后缀"; else ok "生成配置已无后缀条目"; fi
echo "-- 5b：仅旧版 address= 残留 → cleanup"
uci set tailscale.settings._ts_dns_suffix="$SUF"
uci add_list dhcp.@dnsmasq[0].address="/$SUF/100.100.100.100"
uci commit dhcp; uci commit tailscale
/usr/sbin/tailscale_helper cleanup >/dev/null 2>&1
LEFT=$(uci show dhcp 2>/dev/null | grep -c "100.100.100.100")
[ "$LEFT" = "0" ] && ok "旧版 address= 残留被清除（零残留）" || bad "旧版残留 $LEFT 行"

echo
echo "== Phase 6：收尾/复原 =="
kill "$SPID" 2>/dev/null; sleep 1
ip addr del 100.100.100.100/32 dev lo 2>/dev/null
uci del_list dhcp.@dnsmasq[0].notinterface='lo'
uci commit dhcp
/etc/init.d/dnsmasq reload >/dev/null 2>&1
wait_dns && ok "主 dnsmasq 服务正常（收尾）" || bad "主 dnsmasq 异常（收尾）"
netstat -lnup 2>/dev/null | grep -q "127.0.0.1:53" && ok "127.0.0.1:53 已恢复监听" || bad "127.0.0.1:53 未恢复"
echo -n "  Quad100 监听残留数："; netstat -lnup 2>/dev/null | grep -c "100.100.100.100"
rm -f /tmp/h.sh /tmp/standin.log /tmp/standin.pid /tmp/ts-stub /tmp/tsapp.apk
uci show dhcp > /tmp/dhcp.after 2>/dev/null
if [ "$(md5of /tmp/dhcp.before)" = "$(md5of /tmp/dhcp.after)" ]; then ok "现场复原（dhcp 与测试前零差异）"; else bad "dhcp 有净变化"; fi
echo
echo "### 结果：PASS=$PASS FAIL=$FAIL ###"
[ "$FAIL" = "0" ] && echo "### ALL PASS ###" || echo "### 存在失败项 ###"

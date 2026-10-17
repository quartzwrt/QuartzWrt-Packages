// ── 宿主单测体（与 bridge 源码拼接后执行；不进安装包）────────────────────────
import { readfile, writefile } from 'fs';

let pass = 0, fail = 0;

function t(name, cond, extra) {
	if (cond) {
		pass++;
		print(sprintf('PASS %s\n', name));
	}
	else {
		fail++;
		print(sprintf('FAIL %s%s\n', name, extra ? ('  → ' + extra) : ''));
	}
}

function calls() {
	return readfile('/tmp/w8/calls.log') ?? '';
}

function reset_calls() {
	writefile('/tmp/w8/calls.log', '');
}

// 调用一个方法：校验失败 → 立即返回错误信封；校验通过 → 走桩的 defer 回调，
// 由 fake request.reply() 捕获信封（与 rpcd 的异步完成语义一致）。
function call(name, fields) {
	// 声明里只有 data 的方法（数值型）：字段收进 data 对象（见源码"入参形态纪律"）
	let decl = methods[name].args ?? {};
	let args = ('data' in decl && length(keys(decl)) == 1)
		? { data: fields ?? {} }
		: (fields ?? {});

	let out = null;
	let req = {
		args: args,
		reply: function(v) { out = v; },
		error: function(v) { out = { __error: v }; }
	};

	let r = methods[name].call(req);

	return (out != null) ? out : r;
}

// 1. 形状
t('plugin shape: object with methods', type(methods) == 'object');
t('method count = 20', length(keys(methods)) == 20,
  sprintf('got %d: %s', length(keys(methods)), join(',', sort(keys(methods)))));
let all_callable = true;
for (let k in methods)
	if (type(methods[k].call) != 'function')
		all_callable = false;
t('all methods callable', all_callable);

// 2. 动作面收敛
for (let forbidden in ['installUpdate', 'clearData', 'deleteDevice', 'checkUpdate', 'restartService'])
	t('not exposed: ' + forbidden, !(forbidden in methods));

// 3. 读面 happy path + 参数转发
reset_calls();
let r = call('status', null);
t('status ok', r.ok === true && type(r.data) == 'object' && r.data.d[0].mac == 'aa:bb:cc:dd:ee:ff',
  sprintf('%J', r));
t('status forwarded getStatus', match(calls(), /luci\.bandix\tgetStatus\t/));

reset_calls();
r = call('status', { start_ms: 1759990000000, end_ms: 1760000000000 });
t('status with range ok', r.ok === true);
t('status forwarded range args', match(calls(), /start_ms/) && match(calls(), /end_ms/));

r = call('status', { start_ms: 1760000000000, end_ms: 1759990000000 });
t('status rejects end<start', r.ok === false && r.error.code == 'bad_request', sprintf('%J', r));

r = call('status', { start_ms: 0, end_ms: 7776000001 });
t('status rejects span>90d', r.ok === false && r.error.code == 'bad_request');

// 4. MAC 归一化与拒绝
reset_calls();
r = call('device_metrics', { mac: 'AA:BB:CC:DD:EE:FF' });
t('device_metrics accepts upper MAC', r.ok === true && r.data.retention_seconds == 600);
t('device_metrics lowercased', match(calls(), /aa:bb:cc:dd:ee:ff/));

for (let bad in ['aa:bb:cc:dd:ee', 'zz:bb:cc:dd:ee:ff', "aa:bb:cc:dd:ee:ff'; rm -rf /"])
	t('device_metrics rejects mac "' + bad + '"',
	  call('device_metrics', { mac: bad }).ok === false);

t('device_metrics with empty mac = no filter',
  call('device_metrics', { mac: '' }).ok === true);

// 5. 两形态归一
r = call('connections', null);
t('connections unwraps {status,data}', r.ok === true && r.data.cnt == 1, sprintf('%J', r));

r = call('dns_stats', null);
t('dns_stats unwraps {status,data}', r.ok === true && r.data.total_queries == 100, sprintf('%J', r));

// 6. 上游错误形态（success:false）→ data_unavailable 且带出原因
r = call('connection_flows', null);
t('connection_flows maps upstream failure',
  r.ok === false && r.error.code == 'data_unavailable' &&
  match(r.error.message, /not enabled/), sprintf('%J', r));

r = call('connection_flows', { ip: '192.168.1.10', protocol: 'tcp', state: 'ESTABLISHED', page: 1, page_size: 50 });
t('connection_flows forwards filters', r.ok === false);  // 上游仍未启用，但校验应通过（非 bad_request）

r = call('connection_flows', { protocol: 'icmp' });
t('connection_flows rejects bad protocol', r.ok === false && r.error.code == 'bad_request', sprintf('%J', r));

r = call('connection_flows', { page_size: 101 });
t('connection_flows rejects page_size>100', r.ok === false && r.error.code == 'bad_request');

r = call('connection_flows', { ip: "1.1.1.1; reboot" });
t('connection_flows rejects ip injection', r.ok === false && r.error.code == 'bad_request');

// 7. DNS 查询参数
r = call('dns_queries', { domain: 'example.com', is_query: 1, page: 1, page_size: 20 });
t('dns_queries ok', r.ok === true, sprintf('%J', r));
r = call('dns_queries', { domain: 'bad domain!' });
t('dns_queries rejects bad domain', r.ok === false && r.error.code == 'bad_request');
r = call('dns_queries', { is_query: 5 });
t('dns_queries rejects is_query=5', r.ok === false && r.error.code == 'bad_request');

// 8. limits 聚合
r = call('limits', null);
t('limits aggregates three reads',
  r.ok === true && type(r.data.schedule) == 'array' && r.data.schedule[0].id == 'abc123' &&
  r.data.whitelist.enabled === true && type(r.data.quotas) == 'object', sprintf('%J', r));

// 9. version + 能力探测（缓存）
reset_calls();
r = call('version', null);
t('version reports core/luci/bridge',
  r.ok === true && r.data.core == '0.12.11' && r.data.luci_app == '0.12.12' &&
  r.data.bridge == '0.1.0', sprintf('%J', r));
t('version exposes upstream method list',
  type(r.data.upstream_methods) == 'array' && r.data.upstream_methods[0] == 'addRateLimitWhitelist',
  sprintf('%J', r.data.upstream_methods));
reset_calls();
call('version', null);
t('capability probe cached (no 2nd ubus lookup)', !match(calls(), /\tlist\t/));

// 10. 写面：默认限速
reset_calls();
r = call('limit_default_set', { wan_rx_rate_limit: 1024, wan_tx_rate_limit: 512 });
t('limit_default_set ok', r.ok === true, sprintf('%J', r));
t('limit_default_set forwarded', match(calls(), /luci\.bandix\tsetDefaultRateLimit\t.*1024/));
r = call('limit_default_set', { wan_rx_rate_limit: 1024 });
t('limit_default_set requires both', r.ok === false && r.error.code == 'bad_request');
r = call('limit_default_set', { wan_rx_rate_limit: -1, wan_tx_rate_limit: 1 });
t('limit_default_set rejects negative', r.ok === false && r.error.code == 'bad_request');
r = call('limit_default_set', { wan_rx_rate_limit: 1099511627777, wan_tx_rate_limit: 1 });
t('limit_default_set rejects >2^40', r.ok === false && r.error.code == 'bad_request');

// 11. 写面：定时限速
reset_calls();
r = call('limit_schedule_set', { mac: 'aa:bb:cc:dd:ee:ff', start_time: '08:00',
                                 end_time: '12:00', days: [1,2,3],
                                 wan_tx_rate_limit: 512, wan_rx_rate_limit: 1024 });
t('limit_schedule_set ok', r.ok === true, sprintf('%J', r));
t('limit_schedule_set forwards days array', match(calls(), /\[ 1, 2, 3 \]/), calls());
r = call('limit_schedule_set', { mac: 'aa:bb:cc:dd:ee:ff', start_time: '25:00',
                                 end_time: '12:00', days: [1], wan_tx_rate_limit: 1, wan_rx_rate_limit: 1 });
t('limit_schedule_set rejects bad time', r.ok === false && r.error.code == 'bad_request');
r = call('limit_schedule_set', { mac: 'aa:bb:cc:dd:ee:ff', start_time: '08:00',
                                 end_time: '12:00', days: [9], wan_tx_rate_limit: 1, wan_rx_rate_limit: 1 });
t('limit_schedule_set rejects day=9', r.ok === false && r.error.code == 'bad_request');
r = call('limit_schedule_set', { mac: 'aa:bb:cc:dd:ee:ff', start_time: '08:00',
                                 end_time: '12:00', days: ['x'], wan_tx_rate_limit: 1, wan_rx_rate_limit: 1 });
t('limit_schedule_set rejects non-int day', r.ok === false && r.error.code == 'bad_request');
r = call('limit_schedule_update', { id: 'abc123', mac: 'aa:bb:cc:dd:ee:ff', start_time: '08:00',
                                    end_time: '12:00', days: [1], wan_tx_rate_limit: 1, wan_rx_rate_limit: 1 });
t('limit_schedule_update ok', r.ok === true, sprintf('%J', r));
r = call('limit_schedule_update', { id: 'bad id!', mac: 'aa:bb:cc:dd:ee:ff', start_time: '08:00',
                                    end_time: '12:00', days: [1], wan_tx_rate_limit: 1, wan_rx_rate_limit: 1 });
t('limit_schedule_update rejects bad id', r.ok === false && r.error.code == 'bad_request');
r = call('limit_schedule_delete', { id: 'abc123' });
t('limit_schedule_delete ok', r.ok === true, sprintf('%J', r));
r = call('limit_schedule_delete', { id: '' });
t('limit_schedule_delete rejects empty id', r.ok === false && r.error.code == 'bad_request');

// 12. 写面：白名单
r = call('whitelist_add', { mac: 'aa:bb:cc:dd:ee:ff' });
t('whitelist_add ok', r.ok === true, sprintf('%J', r));
r = call('whitelist_enabled', { enabled: 1 });
t('whitelist_enabled ok', r.ok === true, sprintf('%J', r));
r = call('whitelist_enabled', { enabled: 2 });
t('whitelist_enabled rejects 2', r.ok === false && r.error.code == 'bad_request');
r = call('whitelist_delete', { mac: 'nope' });
t('whitelist_delete rejects bad mac', r.ok === false && r.error.code == 'bad_request');

// 13. 写面：配额
reset_calls();
r = call('quota_set', { mac: 'aa:bb:cc:dd:ee:ff', minute_bytes: 0, hourly_bytes: 0,
                        daily_bytes: 1073741824, weekly_bytes: 0, monthly_bytes: 0,
                        total_bytes: 0 });
t('quota_set ok', r.ok === true, sprintf('%J', r));
t('quota_set forwarded', match(calls(), /setTrafficQuota/));
r = call('quota_set', { mac: 'aa:bb:cc:dd:ee:ff', minute_bytes: 0, hourly_bytes: 0,
                        daily_bytes: '1G', weekly_bytes: 0, monthly_bytes: 0, total_bytes: 0 });
t('quota_set rejects non-int', r.ok === false && r.error.code == 'bad_request');
r = call('quota_delete', { mac: 'aa:bb:cc:dd:ee:ff' });
t('quota_delete ok', r.ok === true, sprintf('%J', r));

// 14. 写面：主机名（空串 = 清除绑定）
reset_calls();
r = call('hostname_set', { mac: 'aa:bb:cc:dd:ee:ff', hostname: 'my-laptop' });
t('hostname_set ok', r.ok === true, sprintf('%J', r));
t('hostname_set forwarded', match(calls(), /my-laptop/));
r = call('hostname_set', { mac: 'aa:bb:cc:dd:ee:ff', hostname: '' });
t('hostname_set empty = clear binding', r.ok === true && match(calls(), /"hostname": *""/), sprintf('%J', r));
r = call('hostname_set', { mac: 'aa:bb:cc:dd:ee:ff', hostname: 'bad host!' });
t('hostname_set rejects bad hostname', r.ok === false && r.error.code == 'bad_request');
let long_host = '';
for (let i = 0; i < 70; i++)
	long_host += 'b';
r = call('hostname_set', { mac: 'aa:bb:cc:dd:ee:ff', hostname: long_host });
t('hostname_set rejects >63 chars', r.ok === false && r.error.code == 'bad_request');

// 15. 上游整体不可用（未知方法 → stub 返回 null）
reset_calls();
r = call('ranking', null);
t('ranking ok (fixture present)', r.ok === true, sprintf('%J', r));
r = call('series', { aggregation: '1h' });
t('series ok (fixture present)', r.ok === true, sprintf('%J', r));
r = call('series', { aggregation: '2h' });
t('series rejects bad aggregation', r.ok === false && r.error.code == 'bad_request');

print(sprintf('\n%d passed, %d failed\n', pass, fail));
exit(fail == 0 ? 0 : 1);

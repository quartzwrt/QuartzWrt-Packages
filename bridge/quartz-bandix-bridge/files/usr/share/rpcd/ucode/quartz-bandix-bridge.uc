#!/usr/bin/env ucode
// quartz-bandix-bridge —— bandix 的 Bridge 适配层（WP-8，TDD §5 模式 B）
//
// 形态：rpcd ucode 插件。rpcd 启动时扫描 /usr/share/rpcd/ucode/，本脚本返回的
//       { '<对象名>': methods } 即注册 ubus 对象 —— 对象名取返回表的键（与 exec
//       插件"文件名即对象名"不同）。基座自带同款先例：luci-app-ddns 的 ddns.uc。
//
// 上游：内部委托 luci-app-bandix 的 rpcd 薄壳 luci.bandix（数据源 = bandix 核心
//       本地 HTTP API 127.0.0.1:8686）。网页端与 App 端因此落在同一份实现上。
//
// **异步纪律（实测坑）**：luci.bandix 与我们是**同一个 rpcd 进程**提供的对象——
//       在 ucode 插件里同步 ubus.call 会让 rpcd 的事件循环被本插件占住，嵌套调用
//       永远等不到应答（实测：`ubus call quartz.bandix version` → Request timed out）。
//       故上游调用一律走 conn.defer()（异步）+ request.reply() 完成；返回的
//       deferred 资源交回 rpcd，由它的 pending_replies 机制等待（带守护超时）。
//
// **入参形态纪律（实测坑）**：rpcd 的 ucode 策略要求每个传入参数都在 args 里声明、
//       且 blobmsg 类型精确匹配；而传输层按数值大小自动选 int32/int64（blobmsg_json：
//       int32 范围内 → INT32，超出 → INT64），单一数值提示必然拒掉一半调用
//       （实测：声明 64 时 `ubus call ... limit_default_set '{"wan_rx_rate_limit":102400}'`
//        → Invalid argument）。故**含数值字段的方法统一收进 `data: {}` 对象**
//       （与基座 luci-app-tailscale 的 form_data 同款做法）：对象内部字段不做策略检查，
//       数值合法性由本层校验器把关（int32/int64 在 ucode 里同为 'int'）。
//
// 纪律：① 入参全量白名单校验（字符集/范围/枚举），校验不过不转发；
//       ② 不拼接 shell（ucode 内存内 JSON + ubus defer）；
//       ③ 动作面收敛：不暴露 installUpdate（无签名校验的 apk add --allow-untrusted）、
//          clearData、deleteDevice、checkUpdate（联网非确定性）；
//       ④ 写动作落 logd 一行。
//
// 设计规范：docs/DESIGN.md v1.0（§12 记录本异步机制与死锁坑）

'use strict';

import { connect } from 'ubus';
import { openlog, syslog, closelog, LOG_INFO, LOG_WARNING } from 'log';

const BRIDGE_VERSION = '0.1.0';
const API_VERSION = 1;

const UPSTREAM_OBJECT = 'luci.bandix';
const CAP_CACHE_TTL = 300;	// 上游能力探测缓存（秒）
const MAX_RATE_LIMIT = 1099511627776;	// 2^40：Byte/s 与字节数的上界
const MAX_RANGE_MS = 7776000000;	// 90 天
const MAX_PAGE = 10000;
const MAX_PAGE_SIZE = 100;

// ── 连接（懒建；rpcd 装载脚本时 ubusd 未必就绪，装载期不连）──────────────────
let _conn = null;

function conn() {
	if (_conn == null)
		_conn = connect();

	return _conn;
}

// ── 信封 ─────────────────────────────────────────────────────────────────────
// 上游最近一次失败的机器可读原因（normalize 写入；随信封的 error.message 带出）
let _last_error = null;

function ok(data) {
	return { ok: true, api: API_VERSION, data: data };
}

function err(code, message) {
	return { ok: false, api: API_VERSION, error: { code: code, message: message } };
}

const BAD_REQUEST = (m) => err('bad_request', m);
const DATA_UNAVAILABLE = () => err('data_unavailable', _last_error ?? 'bandix upstream unavailable');
const ACTION_FAILED = (m) => err('action_failed', m);

// 上游两形态归一：unwrap 后的 data（getStatus 等）或包封 {status,data}（getConnection 等）；
// 错误形态 {success:false,error:"…"} / {status:"error",…} 归一到 null 并带出原因。
function normalize(r) {
	_last_error = null;

	if (r == null) {
		_last_error = 'upstream call failed';
		return null;
	}

	if (type(r) != 'object')
		return r;

	if (r.success === false) {
		_last_error = r.error ?? 'upstream reported failure';
		return null;
	}

	if ('status' in r && 'data' in r) {
		if (r.status == 'success')
			return r.data;

		_last_error = r.error ?? r.message ?? ('upstream status: ' + r.status);
		return null;
	}

	return r;
}

// ── 上游调用（异步）──────────────────────────────────────────────────────────
// finish(request, data)：拿到归一后的 data（失败为 null）后完成本 ubus 请求。
// 返回值为 rpcd 需要的 deferred 资源（由方法闭包原样返回）。
function upstream_async(request, method, args, finish) {
	let call = {
		object: UPSTREAM_OBJECT,
		method: method,
		cb: function(ret, reply) {
			let data = (ret == 0) ? normalize(reply) : normalize(null);

			finish(request, data);
		}
	};

	if (args != null && length(keys(args)) > 0)
		call.data = args;

	return conn().defer(call);
}

// ── 入参校验（白名单；返回 null = 非法，'' / null = 未提供）───────────────────
function v_mac(s) {
	if (type(s) != 'string' || s == '')
		return null;

	s = lc(trim(s));

	return match(s, /^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/) ? s : null;
}

function v_int(v, min, max) {
	if (v == null)
		return null;

	if (type(v) != 'int' || v < min || v > max)
		return null;

	return v;
}

function v_enum(v, allowed) {
	if (v == null || v == '')
		return null;

	return allowed[v] ? v : null;
}

function v_str(v, re) {
	if (type(v) != 'string' || v == '')
		return null;

	return match(v, re) ? v : null;
}

function v_time(v) {
	return v_str(v, /^([01][0-9]|2[0-3]):[0-5][0-9]$/);
}

function v_days(v) {
	if (type(v) != 'array' || length(v) < 1 || length(v) > 7)
		return null;

	let out = [];

	for (let d in v) {
		if (type(d) != 'int' || d < 0 || d > 6)
			return null;

		push(out, d);
	}

	return out;
}

// 区间参数：未提供 → null（上游用默认区间）；提供则校验先后与跨度。
function v_range(start_ms, end_ms) {
	if (start_ms == null && end_ms == null)
		return { start: null, end: null };

	if (start_ms != null && v_int(start_ms, 0, 9007199254740991) == null)
		return null;

	if (end_ms != null && v_int(end_ms, 0, 9007199254740991) == null)
		return null;

	if (start_ms != null && end_ms != null) {
		if (end_ms < start_ms || (end_ms - start_ms) > MAX_RANGE_MS)
			return null;
	}

	return { start: start_ms, end: end_ms };
}

// 把 {start,end} 还原成上游入参（省略未提供的键，让上游用默认值）
function range_args(range) {
	let a = {};

	if (range.start != null)
		a.start_ms = range.start;

	if (range.end != null)
		a.end_ms = range.end;

	return a;
}

function audit(action, target, okflag, detail) {
	openlog('quartz-bandix-bridge');
	syslog(okflag ? LOG_INFO : LOG_WARNING,
	       'bandix-bridge action=%s target=%s %s%s',
	       action, target ?? '-', okflag ? 'ok' : 'fail',
	       detail ? (' detail=' + detail) : '');
	closelog();
}

// 数值型方法的入参对象（见顶部"入参形态纪律"）
function data_args(request) {
	return (request.args ?? {}).data ?? {};
}

// ── 通用完成器 ───────────────────────────────────────────────────────────────
// 成功判据 = _last_error 为空（上游写面常返回 {"status":"success","data":null}：
// 归一后的 data 为 null 但调用成功，不能拿 data == null 当失败）
function reply_read(request, data) {
	request.reply((_last_error != null) ? DATA_UNAVAILABLE() : ok(data));
}

function reply_write(request, action, target, data) {
	if (_last_error != null) {
		audit(action, target, false, _last_error);
		request.reply(ACTION_FAILED(_last_error));
		return;
	}

	audit(action, target, true, null);
	request.reply(ok(data));
}

// ── 能力探测（上游方法名集合；缓存 300s）─────────────────────────────────────
let _cap = null;
let _cap_ts = 0;

function cap_cached() {
	let now = time();

	if (_cap != null && (now - _cap_ts) < CAP_CACHE_TTL)
		return _cap;

	return null;
}

// conn.list(name) 返回**签名数组**：[{ "<method>": { "<arg>": "<type>" }, … }]
function cap_store(r) {
	let methods = [];

	if (type(r) == 'array') {
		for (let sig in r) {
			if (type(sig) != 'object')
				continue;

			for (let m in sig)
				push(methods, m);
		}
	}
	else if (type(r) == 'object') {
		for (let m in r)
			push(methods, m);
	}

	sort(methods);

	_cap = length(methods) ? methods : null;
	_cap_ts = time();

	return _cap;
}

// 能力探测：① 对象是否存在（无参 list = 对象路径数组；缺失不抛异常）
//           ② 取方法签名。两者都是对 **ubusd** 的同步查询，不经 rpcd，无死锁风险。
// 注：上游 exec 插件不暴露 list 方法（实测 `ubus call luci.bandix list` → Method not
//     found），所以不能拿 `list` 当上游调用。
function cap_probe() {
	let objs = conn().list();
	let found = false;

	if (type(objs) == 'array') {
		for (let o in objs)
			if (o == UPSTREAM_OBJECT)
				found = true;
	}

	if (!found)
		return cap_store(null);

	return cap_store(conn().list(UPSTREAM_OBJECT));
}

// ── 读方法：校验（返回 {args} 或 {error}）+ 异步取数 ──────────────────────────
function v_range_only(args) {
	let range = v_range(args.start_ms, args.end_ms);

	if (range == null)
		return { error: BAD_REQUEST('invalid start_ms/end_ms') };

	return { args: range_args(range) };
}

function v_mac_opt(args) {
	if (args.mac == null || args.mac == '')
		return { args: {} };

	let mac = v_mac(args.mac);

	if (mac == null)
		return { error: BAD_REQUEST('invalid mac') };

	return { args: { mac: mac } };
}

function v_ranking(args) {
	let base = v_range_only(args);

	if (base.error)
		return base;

	let net = v_enum(args.network_type, { wan: 1, lan: 1, all: 1 });

	if (args.network_type != null && args.network_type != '' && net == null)
		return { error: BAD_REQUEST('invalid network_type') };

	if (net != null)
		base.args.network_type = net;

	return base;
}

function v_series(args) {
	let base = v_range_only(args);

	if (base.error)
		return base;

	let agg = v_enum(args.aggregation, { '1m': 1, '5m': 1, '1h': 1, '1d': 1 });

	if (args.aggregation != null && args.aggregation != '' && agg == null)
		return { error: BAD_REQUEST('invalid aggregation') };

	if (agg != null)
		base.args.aggregation = agg;

	let net = v_enum(args.network_type, { wan: 1, lan: 1, all: 1 });

	if (args.network_type != null && args.network_type != '' && net == null)
		return { error: BAD_REQUEST('invalid network_type') };

	if (net != null)
		base.args.network_type = net;

	let mac = v_mac_opt(args);

	if (mac.error)
		return mac;

	if (mac.args.mac != null)
		base.args.mac = mac.args.mac;

	return base;
}

function v_paging(args, a) {
	if (args.page != null) {
		let p = v_int(args.page, 1, MAX_PAGE);

		if (p == null)
			return 'invalid page';

		a.page = p;
	}

	if (args.page_size != null) {
		let ps = v_int(args.page_size, 1, MAX_PAGE_SIZE);

		if (ps == null)
			return 'invalid page_size';

		a.page_size = ps;
	}

	return null;
}

function v_flows(args) {
	let a = {};

	let ip = null;

	if (args.ip != null && args.ip != '') {
		ip = v_str(args.ip, /^[0-9A-Fa-f:.]{2,45}$/);

		if (ip == null)
			return { error: BAD_REQUEST('invalid ip') };
	}

	let proto = v_enum(args.protocol, { tcp: 1, udp: 1 });

	if (args.protocol != null && args.protocol != '' && proto == null)
		return { error: BAD_REQUEST('invalid protocol') };

	let state = v_enum(args.state, {
		ESTABLISHED: 1, SYN_SENT: 1, SYN_RECV: 1, FIN_WAIT: 1, FIN_WAIT2: 1,
		TIME_WAIT: 1, CLOSE_WAIT: 1, LAST_ACK: 1, CLOSE: 1
	});

	if (args.state != null && args.state != '' && state == null)
		return { error: BAD_REQUEST('invalid state') };

	let perr = v_paging(args, a);

	if (perr)
		return { error: BAD_REQUEST(perr) };

	if (ip != null)
		a.ip = ip;

	if (proto != null)
		a.protocol = proto;

	if (state != null)
		a.state = state;

	return { args: a };
}

function v_dns_queries(args) {
	let a = {};
	let domain = v_str(args.domain, /^[A-Za-z0-9._*-]{1,253}$/);

	if (args.domain != null && args.domain != '' && domain == null)
		return { error: BAD_REQUEST('invalid domain') };

	let device = null;

	if (args.device != null && args.device != '') {
		device = v_str(args.device, /^[A-Za-z0-9._:-]{1,64}$/);

		if (device == null)
			return { error: BAD_REQUEST('invalid device') };
	}

	let is_query = null;

	if (args.is_query != null) {
		is_query = v_int(args.is_query, 0, 1);

		if (is_query == null)
			return { error: BAD_REQUEST('invalid is_query') };
	}

	let server = null;

	if (args.dns_server != null && args.dns_server != '') {
		server = v_str(args.dns_server, /^[A-Za-z0-9._:-]{1,64}$/);

		if (server == null)
			return { error: BAD_REQUEST('invalid dns_server') };
	}

	let qtype = null;

	if (args.query_type != null && args.query_type != '') {
		qtype = v_str(args.query_type, /^[A-Za-z0-9]{1,16}$/);

		if (qtype == null)
			return { error: BAD_REQUEST('invalid query_type') };
	}

	let perr = v_paging(args, a);

	if (perr)
		return { error: BAD_REQUEST(perr) };

	if (domain != null)
		a.domain = domain;

	if (device != null)
		a.device = device;

	if (is_query != null)
		a.is_query = is_query;

	if (server != null)
		a.dns_server = server;

	if (qtype != null)
		a.query_type = qtype;

	return { args: a };
}

// ── 写方法校验 ───────────────────────────────────────────────────────────────
function v_limit_default(args) {
	let rx = v_int(args.wan_rx_rate_limit, 0, MAX_RATE_LIMIT);
	let tx = v_int(args.wan_tx_rate_limit, 0, MAX_RATE_LIMIT);

	if (rx == null || tx == null)
		return { error: BAD_REQUEST('invalid wan_rx_rate_limit/wan_tx_rate_limit') };

	return { args: { wan_rx_rate_limit: rx, wan_tx_rate_limit: tx } };
}

function v_schedule(args, need_id) {
	let a = {};

	if (need_id) {
		let id = v_str(args.id, /^[A-Za-z0-9_-]{1,64}$/);

		if (id == null)
			return { error: BAD_REQUEST('invalid id') };

		a.id = id;
	}

	let mac = v_mac(args.mac);

	if (mac == null)
		return { error: BAD_REQUEST('invalid mac') };

	let start = v_time(args.start_time);
	let end = v_time(args.end_time);
	let days = v_days(args.days);
	let rx = v_int(args.wan_rx_rate_limit, 0, MAX_RATE_LIMIT);
	let tx = v_int(args.wan_tx_rate_limit, 0, MAX_RATE_LIMIT);

	if (start == null || end == null || days == null || rx == null || tx == null)
		return { error: BAD_REQUEST('invalid schedule limit parameters') };

	a.mac = mac;
	a.start_time = start;
	a.end_time = end;
	a.days = days;
	a.wan_rx_rate_limit = rx;
	a.wan_tx_rate_limit = tx;

	return { args: a };
}

function v_id(args) {
	let id = v_str(args.id, /^[A-Za-z0-9_-]{1,64}$/);

	if (id == null)
		return { error: BAD_REQUEST('invalid id') };

	return { args: { id: id } };
}

function v_mac_only(args) {
	let mac = v_mac(args.mac);

	if (mac == null)
		return { error: BAD_REQUEST('invalid mac') };

	return { args: { mac: mac } };
}

function v_whitelist_enabled(args) {
	let enabled = v_int(args.enabled, 0, 1);

	if (enabled == null)
		return { error: BAD_REQUEST('invalid enabled (0|1)') };

	return { args: { enabled: enabled } };
}

function v_quota(args) {
	let mac = v_mac(args.mac);

	if (mac == null)
		return { error: BAD_REQUEST('invalid mac') };

	let a = { mac: mac };

	for (let k in ['minute_bytes', 'hourly_bytes', 'daily_bytes',
	               'weekly_bytes', 'monthly_bytes', 'total_bytes']) {
		let v = v_int(args[k], 0, MAX_RATE_LIMIT);

		if (v == null)
			return { error: BAD_REQUEST('invalid ' + k) };

		a[k] = v;
	}

	return { args: a };
}

function v_hostname(args) {
	let mac = v_mac(args.mac);

	if (mac == null)
		return { error: BAD_REQUEST('invalid mac') };

	let hostname = '';

	if (args.hostname != null && args.hostname != '') {
		hostname = v_str(args.hostname, /^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$/);

		if (hostname == null)
			return { error: BAD_REQUEST('invalid hostname') };
	}

	return { args: { mac: mac, hostname: hostname } };
}

// ── 方法表（args 的取值即 rpcd 类型提示：'' = string / 0 = int32 / 64 = int64 / [] = array）──
// 约定：校验失败 → 直接 return 错误信封（rpcd 立即回）；校验通过 → return upstream_async(...)
//       的 deferred 资源（rpcd 等回调，回调里 request.reply() 完成）。
const methods = {
	// 读面
	status: {
		args: { data: {} },
		call: function(request) {
			let v = v_range_only(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'getStatus', v.args, reply_read);
		}
	},
	device_metrics: {
		args: { mac: '' },
		call: function(request) {
			let v = v_mac_opt(request.args ?? {});

			if (v.error)
				return v.error;

			return upstream_async(request, 'getMetrics', v.args, reply_read);
		}
	},
	ranking: {
		args: { data: {} },
		call: function(request) {
			let v = v_ranking(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'getTrafficUsageRanking', v.args, reply_read);
		}
	},
	series: {
		args: { data: {} },
		call: function(request) {
			let v = v_series(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'getTrafficUsageIncrements', v.args, reply_read);
		}
	},
	connections: {
		call: function(request) {
			return upstream_async(request, 'getConnection', null, reply_read);
		}
	},
	connection_flows: {
		args: { data: {} },
		call: function(request) {
			let v = v_flows(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'getConnectionFlows', v.args, reply_read);
		}
	},
	dns_queries: {
		args: { data: {} },
		call: function(request) {
			let v = v_dns_queries(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'getDnsQueries', v.args, reply_read);
		}
	},
	dns_stats: {
		call: function(request) {
			return upstream_async(request, 'getDnsStats', null, reply_read);
		}
	},
	limits: {
		call: function(request) {
			let acc = {};

			// 三次上游读串行链（每次回调里发下一次；最终 reply 完成本请求）
			return upstream_async(request, 'getScheduleLimits', null, function(req, s) {
				acc.schedule = s;
				acc.schedule_error = _last_error;

				return upstream_async(req, 'getRateLimitWhitelist', null, function(req2, w) {
					acc.whitelist = w;
					acc.whitelist_error = _last_error;

					return upstream_async(req2, 'getTrafficQuotas', null, function(req3, q) {
						acc.quotas = q;
						acc.quotas_error = _last_error;

						if (acc.schedule_error != null && acc.whitelist_error != null &&
						    acc.quotas_error != null)
							req3.reply(DATA_UNAVAILABLE());
						else
							req3.reply(ok(acc));
					});
				});
			});
		}
	},
	version: {
		call: function(request) {
			let finish = function(req, v) {
				req.reply(ok({
					bridge: BRIDGE_VERSION,
					core: v?.bandix_version ?? null,
					luci_app: v?.luci_app_version ?? null,
					upstream_methods: cap_cached() ?? _cap
				}));
			};

			if (cap_cached() == null)
				cap_probe();

			return upstream_async(request, 'getVersion', null, finish);
		}
	},

	// 写面（白名单；不暴露 installUpdate / clearData / deleteDevice / checkUpdate）
	limit_default_set: {
		args: { data: {} },
		call: function(request) {
			let v = v_limit_default(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'setDefaultRateLimit', v.args, function(req, d) {
				reply_write(req, 'limit_default_set', 'default', d);
			});
		}
	},
	limit_schedule_set: {
		args: { data: {} },
		call: function(request) {
			let v = v_schedule(data_args(request), false);

			if (v.error)
				return v.error;

			return upstream_async(request, 'setScheduleLimit', v.args, function(req, d) {
				reply_write(req, 'limit_schedule_set', v.args.mac, d);
			});
		}
	},
	limit_schedule_update: {
		args: { data: {} },
		call: function(request) {
			let v = v_schedule(data_args(request), true);

			if (v.error)
				return v.error;

			return upstream_async(request, 'updateScheduleLimit', v.args, function(req, d) {
				reply_write(req, 'limit_schedule_update', v.args.id, d);
			});
		}
	},
	limit_schedule_delete: {
		args: { id: '' },
		call: function(request) {
			let v = v_id(request.args ?? {});

			if (v.error)
				return v.error;

			return upstream_async(request, 'deleteScheduleLimit', v.args, function(req, d) {
				reply_write(req, 'limit_schedule_delete', v.args.id, d);
			});
		}
	},
	whitelist_add: {
		args: { mac: '' },
		call: function(request) {
			let v = v_mac_only(request.args ?? {});

			if (v.error)
				return v.error;

			return upstream_async(request, 'addRateLimitWhitelist', v.args, function(req, d) {
				reply_write(req, 'whitelist_add', v.args.mac, d);
			});
		}
	},
	whitelist_delete: {
		args: { mac: '' },
		call: function(request) {
			let v = v_mac_only(request.args ?? {});

			if (v.error)
				return v.error;

			return upstream_async(request, 'deleteRateLimitWhitelist', v.args, function(req, d) {
				reply_write(req, 'whitelist_delete', v.args.mac, d);
			});
		}
	},
	whitelist_enabled: {
		args: { data: {} },
		call: function(request) {
			let v = v_whitelist_enabled(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'setRateLimitWhitelistEnabled', v.args, function(req, d) {
				reply_write(req, 'whitelist_enabled', v.args.enabled ? '1' : '0', d);
			});
		}
	},
	quota_set: {
		args: { data: {} },
		call: function(request) {
			let v = v_quota(data_args(request));

			if (v.error)
				return v.error;

			return upstream_async(request, 'setTrafficQuota', v.args, function(req, d) {
				reply_write(req, 'quota_set', v.args.mac, d);
			});
		}
	},
	quota_delete: {
		args: { mac: '' },
		call: function(request) {
			let v = v_mac_only(request.args ?? {});

			if (v.error)
				return v.error;

			return upstream_async(request, 'deleteTrafficQuota', v.args, function(req, d) {
				reply_write(req, 'quota_delete', v.args.mac, d);
			});
		}
	},
	hostname_set: {
		args: { mac: '', hostname: '' },
		call: function(request) {
			let v = v_hostname(request.args ?? {});

			if (v.error)
				return v.error;

			return upstream_async(request, 'setHostname', v.args, function(req, d) {
				reply_write(req, 'hostname_set', v.args.mac, d);
			});
		}
	}
};

return { 'quartz.bandix': methods };

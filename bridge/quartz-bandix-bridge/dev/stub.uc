// [测试装配] 替换 import { connect } from 'ubus' 与 log 导入（不进安装包）
//
// 桩：fixture.json 提供各上游方法的应答，未知方法返回 null（模拟调用失败）。
// 注意 bridge 走 **异步 defer**（见源码顶部"异步纪律"）——桩的 defer 同步调用
// 回调，使单测仍可线性断言。
function stub_log(obj, method, args) {
	let f = open('/tmp/w8/calls.log', 'a');

	if (f) {
		f.write(sprintf('%s\t%s\t%s\n', obj, method, args == null ? '-' : sprintf('%J', args)));
		f.close();
	}
}

function stub_dispatch(method) {
	let fixture = json(readfile('/tmp/w8/fixture.json') ?? '{}');

	if (!(method in fixture))
		return null;

	return fixture[method];
}

function connect() {
	return {
		// 能力探测用：无参 = 对象路径数组；带名 = 签名数组（fixture 的 list 键 = 上游方法集合）
		list: function(obj) {
			stub_log(obj ?? '-', 'list', null);

			if (obj == null)
				return ['system', 'service', 'luci.bandix', 'quartz.bandix'];

			let f = json(readfile('/tmp/w8/fixture.json') ?? '{}');

			return [ f['list'] ?? {} ];
		},

		call: function(obj, method, args) {
			stub_log(obj, method, args ?? null);

			return stub_dispatch(method);
		},

		defer: function(call) {
			stub_log(call.object, call.method, call.data ?? null);

			let reply = stub_dispatch(call.method);

			if (type(call.cb) == 'function')
				call.cb(reply == null ? 4 : 0, reply);   // 4 = UBUS_STATUS_NOT_FOUND

			return { __deferred: true };
		}
	};
}

function openlog(name) {}
function syslog(prio, fmt) {}
function closelog() {}
const LOG_INFO = 6;
const LOG_WARNING = 4;

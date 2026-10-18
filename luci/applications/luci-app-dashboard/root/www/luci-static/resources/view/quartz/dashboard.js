'use strict';
/*
 * luci-app-dashboard — LuCI 视图壳（手写，不参与前端构建）
 *
 * 职责（docs/DESIGN.md §1.2 / §2.3）：
 *   1. 以 <script>/<link> 注入前端构建产物（htdocs/luci-static/resources/quartz-dashboard/）
 *   2. 向产物注入宿主适配器 host（取数 / 轮询 / 翻译 / 深链存在性判定）
 *   3. 产物缺失或渲染异常时给出可读提示（不白屏）
 *
 * 产物契约：window.QuartzDashboard.mount(el, host)
 * host 为版本化接口：v1 = readStatus / poll / t / hasPage / url / version（只读）
 *
 * 说明：LuCI 资源加载器为 XHR + 伪 import 扫描 + eval（源码核实 2026-10-12），
 * 构建产物不能走 LuCI 的 require 体系，故此处用 <script> 注入；
 * 产物内部不得引用 LuCI 内部模块，依赖一律经 host。
 */

'require view';
'require fs';
'require ui';
'require poll';

/* 产物入口与构建标识：dev/build.sh 构建后以产物哈希改写 BUILD（配合 L.env.resource_version 破缓存） */
var BUNDLE = 'quartz-dashboard/app.js';
var STYLES = 'quartz-dashboard/app.css';
var BUILD = '8aefd9f2';

return view.extend({
	load() {
		/* 菜单树：host.hasPage() 的深链存在性判定用（基座既有 API） */
		return ui.menu.load().catch(function () { return null; });
	},

	render(tree) {
		var self = this;

		/* ---- 宿主适配器（DESIGN.md §2.3） ---- */
		var host = {
			version: BUILD,

			/* 翻译（页面已加载 LuCI translations） */
			t: function (s) { return _(s); },

			/* 读聚合快照：区分"未运行"与"数据损坏"（DESIGN.md §2.2 四态） */
			readStatus: function () {
				return fs.read('/var/run/quartz/status.json').then(function (raw) {
					try {
						return JSON.parse(raw);
					} catch (e) {
						var pe = new Error('status parse failed');
						pe.kind = 'broken';
						throw pe;
					}
				}, function () {
					var me = new Error('status file missing');
					me.kind = 'missing';
					throw me;
				});
			},

			/* 轮询：包装可见性判断——LuCI Poll 只做 setInterval、不判断 hidden（源码核实），
			 * 故页面隐藏时不取数、恢复可见立即补取一次，由本适配器负责 */
			poll: function (fn, sec) {
				var wrapped = function () {
					if (document.hidden)
						return;
					return fn();
				};
				var onVis = function () {
					if (!document.hidden)
						fn();
				};
				document.addEventListener('visibilitychange', onVis);
				poll.add(wrapped, sec || 5);
				return function () {
					document.removeEventListener('visibilitychange', onVis);
					poll.remove(wrapped);
				};
			},

			/* 深链存在性判定：菜单树遍历；不存在返回 false → 卡片隐藏入口（不死链，§6.3-1） */
			hasPage: function (path) {
				var node = tree;
				var parts = String(path || '').split('/');
				for (var i = 0; i < parts.length && node; i++)
					node = node.children ? node.children[parts[i]] : null;
				return !!(node && (node.action || node.children));
			},

			/* 目标页 URL（存在性判定通过后取用） */
			url: function (path) {
				try { return L.url(path); } catch (e) { return null; }
			}
		};

		var root = E('div', { 'id': 'qz-dashboard' });
		var bust = '?v=' + encodeURIComponent([ L.env.resource_version || '', BUILD ].filter(Boolean).join('-'));

		document.head.appendChild(E('link', {
			'rel': 'stylesheet',
			'href': L.resource(STYLES) + bust
		}));

		var script = E('script', { 'src': L.resource(BUNDLE) + bust, 'async': 'async' });

		script.onload = function () {
			if (!window.QuartzDashboard || typeof window.QuartzDashboard.mount !== 'function') {
				root.appendChild(self.errorBox(
					_('Dashboard failed to render'),
					_('Bundle entry point window.QuartzDashboard.mount() is missing.')));
				return;
			}
			try {
				window.QuartzDashboard.mount(root, host);
			} catch (e) {
				root.appendChild(self.errorBox(_('Dashboard failed to render'), String((e && e.message) || e)));
			}
		};

		script.onerror = function () {
			root.appendChild(self.errorBox(
				_('Dashboard bundle is missing'),
				_('This is a development state: build src/ and commit the bundle to htdocs/luci-static/resources/quartz-dashboard/ (see docs/DESIGN.md §4).')));
		};

		document.head.appendChild(script);
		return root;
	},

	/* 可读错误块（沿用主题告警样式，不白屏） */
	errorBox(title, msg) {
		return E('div', { 'class': 'alert-message error' }, [
			E('h4', {}, title),
			E('p', {}, msg)
		]);
	},

	/* 只读页：抑制 LuCI 的页面操作条（保存并应用 / 保存 / 复位）。
	 * luci.js 的 View.__init__ 对每个 JS 视图无条件调用 addFooter()，是否出现按钮取
	 * 决于三个 handle* 是否非空，而基类默认实现非空——不置空就会在只读页长出保存条
	 * （DESIGN.md §0 边界：v1 只展示、不写系统）。基座只读视图同款写法（status/index.js 等） */
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

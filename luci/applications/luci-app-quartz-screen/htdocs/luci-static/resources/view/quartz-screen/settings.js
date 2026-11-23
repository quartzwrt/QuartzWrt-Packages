/* SPDX-License-Identifier: Apache-2.0
 *
 * luci-app-quartz-screen — 小屏显示设置（WP-16）
 *
 * 本基座（25.12）uci set 直接落盘（无 stage/apply 语义），保存后调用
 * `quartz-screen apply` 重启守护进程；handleSave 按 View 基类实现内联复刻
 * （WP-13 坑位：form.Map.prototype 上没有 handleSave/handleSaveApply）。
 */

'use strict';
'require view';
'require form';
'require uci';
'require fs';
'require dom';
'require ui';

function applyService() {
	return fs.exec('/usr/sbin/quartz-screen', [ 'apply' ]).then(function(res) {
		if (res.code !== 0) {
			ui.addNotification(null, E('p', {},
				_('Failed to apply screen settings: %s').format(res.stderr || res.code)), 'error');
		}
		return res;
	});
}

var SCREEN_PAGES = [
	[ 'overview', _('Overview') ],
	[ 'network', _('Network') ],
	[ 'wireless', _('Wireless') ],
	[ 'cellular', _('Cellular') ],
	[ 'system', _('System') ],
	[ 'clock', _('Clock') ],
	[ 'note', _('Note') ]
];

function pageEnabled(id) {
	var pages = L.toArray(uci.get('quartz-screen', 'main', 'pages'));
	return pages.indexOf(id) >= 0;
}

function timeValidator(section_id, value) {
	if (value == null || value === '')
		return true;
	return /^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(value) ? true : _('Expected HH:MM');
}

return view.extend({
	load: function() {
		return uci.load('quartz-screen');
	},

	handleSave: function(ev) {
		var tasks = [];
		document.getElementById('maincontent')
			.querySelectorAll('.cbi-map').forEach(function(map) {
				tasks.push(dom.callClassMethod(map, 'save'));
			});
		return Promise.all(tasks).then(function() {
			var enabled = {}, pages = [];
			SCREEN_PAGES.forEach(function(p) {
				enabled[p[0]] = (uci.get('quartz-screen', 'main', 'page_' + p[0]) == '1');
			});
			L.toArray(uci.get('quartz-screen', 'main', 'pages')).forEach(function(id) {
				if (enabled[id] && pages.indexOf(id) < 0)
					pages.push(id);
			});
			SCREEN_PAGES.forEach(function(p) {
				if (enabled[p[0]] && pages.indexOf(p[0]) < 0)
					pages.push(p[0]);
			});
			uci.set('quartz-screen', 'main', 'pages', pages);
			return uci.save();
		}).then(function() {
			/* 本基座 uci set 只进 changes 缓冲，必须 apply 才落盘。 */
			return uci.apply();
		}).then(applyService);
	},

	handleSaveApply: function(ev, mode) {
		return this.handleSave(ev);
	},

	render: function() {
		var m = new form.Map('quartz-screen', _('Screen Display'),
			_('Built-in 1.47" screen (320×172): pages, brightness, night dimming and content.'));

		/* ---- 基本 ---- */
		var s = m.section(form.NamedSection, 'main', 'screen', _('Display & pages'));

		var o = s.option(form.Flag, 'enabled', _('Enable'));
		o.default = '1';
		o.rmempty = false;

		o = s.option(form.Value, 'brightness', _('Brightness'),
			_('Daytime backlight level (1–100%).'));
		o.datatype = 'range(1,100)';
		o.default = '80';
		o.rmempty = false;

		o = s.option(form.ListValue, 'theme', _('Theme'));
		o.value('dark', _('Dark'));
		o.value('light', _('Light'));
		o.default = 'dark';

		o = s.option(form.ListValue, 'lang', _('Language'));
		o.value('auto', _('Follow LuCI'));
		o.value('zh', _('中文'));
		o.value('en', _('English'));
		o.default = 'auto';

		o = s.option(form.ListValue, 'rotate', _('Orientation'));
		o.value('0', _('Normal'));
		o.value('180', _('Flipped (180°)'));
		o.default = '0';

		/* ---- 轮播与页面 ---- */
		o = s.option(form.ListValue, 'interval', _('Page interval'));
		o.value('0', _('Off (stay on first page)'));
		o.value('3', '3 s');
		o.value('5', '5 s');
		o.value('8', '8 s');
		o.value('10', '10 s');
		o.value('15', '15 s');
		o.value('30', '30 s');
		o.default = '8';
		o.rmempty = false;

		SCREEN_PAGES.forEach(function(p) {
			var pf = s.option(form.Flag, 'page_' + p[0], p[1]);
			pf.default = '0';
			pf.cfgvalue = function() { return pageEnabled(p[0]) ? '1' : '0'; };
		});

		/* ---- 页面内容 ---- */
		o = s.option(form.Value, 'title', _('Custom title'),
			_('Overrides the overview top-bar title.'));
		o.placeholder = 'QuartzWrt';
		o.maxlength = 32;

		o = s.option(form.ListValue, 'operator', _('Operator icon'),
			_('Auto follows the modem; override when it reports an unknown operator.'));
		o.value('auto', _('Auto (from modem)'));
		o.value('mobile', _('China Mobile'));
		o.value('unicom', _('China Unicom'));
		o.value('telecom', _('China Telecom'));
		o.value('broadnet', _('China Broadnet'));
		o.default = 'auto';

		o = s.option(form.ListValue, 'temp_unit', _('Temperature unit'));
		o.value('c', '°C');
		o.value('f', '°F');
		o.default = 'c';

		o = s.option(form.Flag, 'clock_24h', _('24-hour clock'));
		o.default = '1';

		o = s.option(form.Flag, 'show_ipv6', _('Show IPv6 on network page'));
		o.default = '1';

		o = s.option(form.Flag, 'show_peaks', _('Show peak rates'));
		o.default = '1';

		o = s.option(form.Flag, 'show_clients', _('Show client count'));
		o.default = '1';

		/* ---- 夜间 ---- */
		var sn = m.section(form.NamedSection, 'night', 'night', _('Night mode'));

		o = sn.option(form.Flag, 'enabled', _('Enable night window'));
		o.default = '1';

		o = sn.option(form.Value, 'start', _('Start'), _('HH:MM, 24-hour.'));
		o.placeholder = '23:00';
		o.validate = timeValidator;

		o = sn.option(form.Value, 'end', _('End'), _('HH:MM, 24-hour.'));
		o.placeholder = '07:00';
		o.validate = timeValidator;

		o = sn.option(form.Value, 'brightness', _('Night brightness'));
		o.datatype = 'range(0,100)';
		o.default = '10';

		o = sn.option(form.Flag, 'off', _('Turn the screen off at night'));
		o.default = '0';

		/* ---- 便签 ---- */
		var st = m.section(form.NamedSection, 'note', 'note', _('Note page'),
			_('Shown on the "note" page. Add the page to the carousel list above to display it.'));

		o = st.option(form.Value, 'title', _('Title'));
		o.placeholder = _('Note');
		o.maxlength = 32;

		[ 'line1', 'line2', 'line3', 'line4' ].forEach(function(key, idx) {
			var lo = st.option(form.Value, key, _('Line %d').format(idx + 1));
			lo.maxlength = 48;
		});

		return m.render();
	}
});

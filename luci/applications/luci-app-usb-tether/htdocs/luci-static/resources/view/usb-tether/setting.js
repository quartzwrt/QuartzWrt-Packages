/* SPDX-License-Identifier: Apache-2.0
 *
 * luci-app-usb-tether — 手机 USB 共享上网（QuartzWrt，WP-13）
 *
 * 保存链路（本基座实测语义，docs/DESIGN.md §2）：
 *   - 页脚「保存并应用」下拉 → 视图 handleSaveApply → handleSave：
 *     基座 25.12 的 ubus uci set **直接落盘**（无 stage/apply/rollback 语义，
 *     uci.changes 恒空、uci.apply 返回 5=NOT_FOUND），故 helper 即 apply——
 *     保存后直接调 usb_tether_helper 幂等同步 network/firewall；
 *   - handleSave 按 luci.js View 基类实现内联复刻（form.Map.prototype 上
 *     并无 handleSave/handleSaveApply，对其 .call() 会当场崩成
 *     "Cannot read properties of undefined" → 按钮永久转圈、零请求——真机
 *     实测坑，上游 asvow 模板同款写法在此基座不可用）；
 *   - /etc/init.d/usb-tether 保留开机自愈 + 兼容未来 apply 触发链回归。
 */

'use strict';
'require dom';
'require form';
'require fs';
'require uci';
'require ui';
'require view';

function applyHelper() {
	return fs.exec('/usr/sbin/usb_tether_helper', ['apply']).then(function(res) {
		if (res.code !== 0) {
			ui.addNotification(null, E('p', {},
				_('Failed to apply USB tethering settings: %s').format(res.stderr || res.code)), 'error');
		}
		return res;
	});
}

function parseDetect(res) {
	if (!res || res.code !== 0 || !res.stdout || res.stdout.trim() === '')
		return { devices: [] };
	try {
		const data = JSON.parse(res.stdout);
		return Array.isArray(data.devices) ? data : { devices: [] };
	} catch (e) {
		return { devices: [] };
	}
}

return view.extend({
	load() {
		return Promise.all([
			uci.load('usb-tether'),
			uci.load('firewall'),
			fs.exec('/usr/sbin/usb_tether_helper', ['detect']).catch(function() { return null; })
		]);
	},

	handleSave: function(ev) {
		/* 内联复刻 luci.js View 基类 handleSave（遍历 .cbi-map 调 Map.save，
		 * 基座 uci.set 即落盘）+ helper 同步胶水。注意：模块作用域里 dom 为
		 * 小写（'require dom'），基座源码里的 DOM 是其内部别名，照抄会
		 * ReferenceError 当场死（实测坑） */
		const tasks = [];

		document.getElementById('maincontent')
			.querySelectorAll('.cbi-map').forEach(function(map) {
				tasks.push(dom.callClassMethod(map, 'save'));
			});

		return Promise.all(tasks).then(function() {
			return applyHelper();
		});
	},

	handleSaveApply: function(ev, mode) {
		/* 本基座无 apply/rollback 语义（uci.apply 恒 NOT_FOUND），
		 * handleSave（含 helper）即完整应用 */
		return this.handleSave(ev);
	},

	render(data) {
		const detect = parseDetect(data[2]);
		const curdev = uci.get('usb-tether', 'config', 'device') || 'usb0';

		let m, s, o;

		m = new form.Map('usb-tether', _('USB Tethering'),
			_('Use your phone as a WAN source: connect it to the router USB port, enable USB tethering on the phone, then turn this feature on. The router obtains internet access from the phone via DHCP.'));

		s = m.section(form.NamedSection, 'config', 'tether');

		o = s.option(form.Flag, 'enabled', _('Enable'));
		o.default = o.disabled;
		o.rmempty = false;
		o.description = _('When enabled, a DHCP interface on the selected USB device is created and attached to the chosen firewall zone (metric applies). Re-enabling after unplugging the phone requires no further action — the interface follows the device automatically.');

		o = s.option(form.ListValue, 'device', _('USB Device'),
			_('Detected USB network devices. Phones expose RNDIS / CDC-NCM / CDC-Ether (Android) or ipheth (iPhone) once USB tethering is switched on.'));
		o.rmempty = false;
		/* 当前配置值不在位时仍保留为候选（手机未插也能保存配置） */
		o.value(curdev, '%s — %s'.format(curdev, _('configured')));
		detect.devices.forEach(function(dev) {
			if (dev.ifname === curdev)
				return;
			let label = dev.ifname;
			if (dev.manufacturer || dev.product)
				label += ' · %s %s'.format(dev.manufacturer || '', dev.product || '');
			if (dev.driver)
				label += ' (%s)'.format(dev.driver);
			if (dev.tetherable !== true)
				label += ' [%s]'.format(_('modem device'));
			else if (dev.used_by)
				label += ' [%s: %s]'.format(_('in use'), dev.used_by);
			o.value(dev.ifname, label);
		});
		if (curdev !== 'usb0')
			o.value('usb0', 'usb0 — %s'.format(_('default')));
		o.default = 'usb0';

		o = s.option(form.Value, 'metric', _('Route Metric'),
			_('Default 50 = standby (fallback): the phone is used only when other WAN connections are down. Set a lower value than your main WAN to make the phone the primary exit.'));
		o.datatype = 'range(0,9999)';
		o.placeholder = '50';
		o.default = '50';
		o.rmempty = false;

		o = s.option(form.ListValue, 'zone', _('Firewall Zone'),
			_('Firewall zone for the tethering interface. Keep it in the WAN zone unless you know what you are doing.'));
		const zones = uci.sections('firewall', 'zone')
			.map(function(z) { return z.name; })
			.filter(function(n) { return (n && n.length); });
		(zones.length ? zones : [ 'wan' ]).forEach(function(z) {
			o.value(z, z);
		});
		o.default = 'wan';
		o.rmempty = false;

		return m.render();
	}
});

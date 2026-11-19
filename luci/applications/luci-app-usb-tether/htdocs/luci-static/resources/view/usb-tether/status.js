/* SPDX-License-Identifier: Apache-2.0
 *
 * luci-app-usb-tether — 手机 USB 共享上网（QuartzWrt，WP-13）
 *
 * 状态页：只读轮询（detect + ip addr/route），无表单保存；
 * 模式同 luci-app-tailscale/interface.js（fs.exec + poll + dom.content）。
 */

'use strict';
'require dom';
'require fs';
'require poll';
'require uci';
'require ui';
'require view';

function safeJsonParse(res) {
	if (!res || res.code !== 0 || !res.stdout || res.stdout.trim() === '')
		return null;
	try {
		return JSON.parse(res.stdout);
	} catch (e) {
		return null;
	}
}

function parseDetect(res) {
	const data = safeJsonParse(res);
	if (!data || !Array.isArray(data.devices))
		return [];
	return data.devices;
}

function bold(text, color) {
	return E('strong', { 'style': 'color:%s'.format(color) }, text);
}

return view.extend({
	load() {
		return Promise.all([
			uci.load('usb-tether'),
			fs.exec('/usr/sbin/usb_tether_helper', ['detect']).catch(function() { return null; }),
			fs.exec('/sbin/ip', ['-s', '-j', 'ad']).catch(function() { return null; }),
			fs.exec('/sbin/ip', ['-j', 'route', 'show', 'default']).catch(function() { return null; })
		]);
	},

	renderContent(data) {
		const enabled = (uci.get('usb-tether', 'config', 'enabled') === '1');
		const device = uci.get('usb-tether', 'config', 'device') || 'usb0';
		const metric = uci.get('usb-tether', 'config', 'metric') || '50';

		const devices = parseDetect(data[1]);
		const phoneDev = devices.filter(function(d) { return d.ifname === device; })[0];
		const others = devices.filter(function(d) { return d.ifname !== device; });

		const ipAd = safeJsonParse(data[2]);
		const ipEntry = Array.isArray(ipAd)
			? ipAd.filter(function(i) { return i.ifname === device; })[0] : null;
		const addrInfo = ipEntry ? (ipEntry.addr_info || []) : [];
		const ip4 = addrInfo.filter(function(a) { return a.family === 'inet'; })[0];
		const ip6 = addrInfo.filter(function(a) { return a.family === 'inet6'; })[0];

		const routes = safeJsonParse(data[3]) || [];
		let bestRoute = null;
		routes.forEach(function(r) {
			if (!r.dev)
				return;
			const m = parseInt(r.metric || 0);
			if (!bestRoute || m < parseInt(bestRoute.metric || 0))
				bestRoute = r;
		});
		const viaPhone = (bestRoute !== null && bestRoute.dev === device);
		const phoneRoute = routes.filter(function(r) { return r.dev === device; })[0];

		const rows = [];
		const addRow = function(label, value) {
			rows.push(E('tr', { 'class': 'tr' }, [
				E('td', { 'class': 'td left', 'width': '25%' }, label),
				E('td', { 'class': 'td left' }, value)
			]));
		};

		/* 手机 / USB 设备 */
		let phoneCell;
		if (phoneDev && phoneDev.tetherable !== false) {
			let info = '%s %s'.format(phoneDev.manufacturer || '', phoneDev.product || '');
			if (phoneDev.driver)
				info += phoneDev.manufacturer || phoneDev.product ? ' (%s)'.format(phoneDev.driver) : phoneDev.driver;
			phoneCell = E([], [ bold(_('Detected'), 'green'), E('br'),
				E('span', { 'class': 'cbi-value-description' }, info) ]);
		} else if (phoneDev) {
			phoneCell = E([], [ bold(_('Modem device'), 'orange'), E('br'),
				E('span', { 'class': 'cbi-value-description' },
					_('This device looks like a 5G modem data port. Pick another one in Settings.')) ]);
		} else {
			phoneCell = E([], [ bold(_('Not detected'), 'red'), E('br'),
				E('span', { 'class': 'cbi-value-description' },
					_('Plug the phone into the router USB port and enable USB tethering on the phone, then select the device in Settings.')) ]);
			if (others.length > 0) {
				phoneCell.appendChild(E('br'));
				phoneCell.appendChild(E('span', { 'class': 'cbi-value-description' },
					_('Detected other USB devices: %s').format(
						others.map(function(d) { return d.ifname; }).join(', '))));
			}
		}
		addRow(_('Phone / USB Device'), phoneCell);

		/* 接口状态 */
		let ifaceCell;
		if (!enabled) {
			ifaceCell = bold(_('Disabled'), 'red');
		} else if (!ipEntry) {
			ifaceCell = bold(_('Enabled, waiting for device …'), 'orange');
		} else if (ip4) {
			ifaceCell = bold(_('Connected'), 'green');
		} else {
			ifaceCell = E([], [ bold(_('Device present, waiting for DHCP …'), 'orange'), E('br'),
				E('span', { 'class': 'cbi-value-description' },
					_('Check that USB tethering is switched on on the phone.')) ]);
		}
		addRow(_('Interface'), ifaceCell);

		addRow(_('IPv4 Address'), ip4 ? '%s/%d'.format(ip4.local, ip4.prefixlen) : '—');
		if (ip6)
			addRow(_('IPv6 Address'), '%s/%d'.format(ip6.local, ip6.prefixlen));

		addRow(_('Gateway'),
			(phoneRoute && phoneRoute.via) ? phoneRoute.via : '—');

		/* 流量（stats64 可能缺失，例如极老 busybox ip） */
		let rxCell = '—', txCell = '—';
		if (ipEntry && ipEntry.stats64) {
			rxCell = '%1024mB'.format(ipEntry.stats64.rx.bytes);
			txCell = '%1024mB'.format(ipEntry.stats64.tx.bytes);
		}
		addRow(_('Total Download'), rxCell);
		addRow(_('Total Upload'), txCell);

		/* 当前上网出口 */
		let exitCell;
		if (viaPhone) {
			exitCell = E([], [ bold(_('Phone (%s) — active').format(device), 'green'), E('br'),
				E('span', { 'class': 'cbi-value-description' },
					_('Traffic is currently routed through the phone.')) ]);
		} else if (bestRoute) {
			exitCell = E([], [ bold(_('Standby (metric %d)').format(parseInt(metric)), 'orange'), E('br'),
				E('span', { 'class': 'cbi-value-description' },
					_('Active exit: %s (metric %d). Lower the metric in Settings to prefer the phone.')
						.format(bestRoute.dev, parseInt(bestRoute.metric || 0))) ]);
		} else {
			exitCell = bold(_('No default route present'), 'red');
		}
		addRow(_('Current Exit'), exitCell);

		return E('table', { 'class': 'table' }, rows);
	},

	pollData(container) {
		poll.add(async () => {
			const data = await this.load();
			dom.content(container, this.renderContent(data));
		});
	},

	render(data) {
		const content = E([], [
			E('h2', { 'class': 'content' }, _('USB Tethering')),
			E('div', { 'class': 'cbi-map-descr' },
				_('Live status of the internet connection shared over USB from a phone. Values refresh every few seconds. Supported drivers: RNDIS, CDC-NCM, CDC-Ether (Android), ipheth (iPhone).')),
			E('div')
		]);
		const container = content.lastElementChild;

		dom.content(container, this.renderContent(data));
		this.pollData(container);

		return content;
	},

	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

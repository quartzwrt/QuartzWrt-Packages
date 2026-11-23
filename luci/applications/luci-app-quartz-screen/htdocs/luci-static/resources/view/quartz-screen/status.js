/* SPDX-License-Identifier: Apache-2.0
 *
 * luci-app-quartz-screen — 小屏显示状态与实时预览（WP-16）
 *
 * 预览直接调用 quartz-screen 的同一套渲染器（离线渲染 → base64 PNG），
 * 保证 LuCI 看到的画面与实屏一致；只读页面，不显示保存条。
 */

'use strict';
'require view';
'require fs';
'require uci';
'require poll';
'require ui';

var PAGE_LABELS = {
	overview: _('Overview'),
	network: _('Network'),
	wireless: _('Wireless'),
	cellular: _('Cellular'),
	system: _('System'),
	clock: _('Clock'),
	note: _('Note')
};

var CSS = '\
#qz-screen-status-page .qz-screen-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:10px}\
#qz-screen-status-page .qz-screen-head .spacer{flex:1}\
#qz-screen-status-page .qz-screen-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 28px}\
#qz-screen-status-page .qz-screen-kv{display:flex;justify-content:space-between;gap:14px;padding:6px 0;border-bottom:1px solid var(--qz-border,rgba(127,127,127,.18))}\
#qz-screen-status-page .qz-screen-kv>span:first-child{color:var(--qz-text-2,#737373)}\
#qz-screen-status-page .qz-screen-kv>span:last-child{color:var(--qz-text,#222);font-family:var(--qz-font-mono,monospace);text-align:right;word-break:break-all}\
#qz-screen-status-page .qz-dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:2px}\
#qz-screen-status-page .qz-dot.ok{background:var(--qz-ok,#34c759)}\
#qz-screen-status-page .qz-dot.err{background:var(--qz-err,#ff3b30)}\
#qz-screen-status-page .qz-screen-toolbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:12px}\
#qz-screen-status-page .qz-screen-previews{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:14px}\
#qz-screen-status-page .qz-screen-frame{background:var(--qz-bg-well,#f2f2f7);border:1px solid var(--qz-border,rgba(127,127,127,.18));border-radius:var(--qz-radius-sm,8px);padding:8px}\
#qz-screen-status-page .qz-screen-frame img{display:block;width:100%;height:auto;image-rendering:pixelated;border-radius:6px;background:#000}\
#qz-screen-status-page .qz-screen-frame .cap{color:var(--qz-text-2,#737373);font-size:12px;margin-top:6px;display:flex;justify-content:space-between}\
#qz-screen-status-page .qz-screen-errors{margin:10px 0 0;padding:8px 10px;background:var(--qz-bg-well,#f2f2f7);border-radius:8px;color:var(--qz-err-text,#d70015);font-family:var(--qz-font-mono,monospace);font-size:12px;white-space:pre-wrap}\
@media (max-width:700px){#qz-screen-status-page .qz-screen-grid{grid-template-columns:1fr}}';

function parseStatus(res) {
	if (!res || res.code !== 0 || !res.stdout)
		return null;
	try {
		return JSON.parse(res.stdout);
	} catch (e) {
		return null;
	}
}

function pageList(st) {
	if (st && Array.isArray(st.pages) && st.pages.length)
		return st.pages;
	var raw = uci.get('quartz-screen', 'main', 'pages');
	var out = [];
	var list = Array.isArray(raw) ? raw : String(raw || '').split(/[\s,]+/);
	list.forEach(function(id) {
		if (id && out.indexOf(id) < 0)
			out.push(id);
	});
	return out.length ? out : [ 'overview' ];
}

function label(id) {
	return PAGE_LABELS[id] || id;
}

function kv(labelText, valueText) {
	return E('div', { 'class': 'qz-screen-kv' }, [
		E('span', {}, labelText),
		E('span', {}, valueText)
	]);
}

function formatAge(ts) {
	if (!ts)
		return '—';
	var sec = Math.max(0, Math.floor(Date.now() / 1000 - ts));
	if (sec < 60)
		return sec + ' s';
	if (sec < 3600)
		return Math.floor(sec / 60) + ' min';
	return Math.floor(sec / 3600) + ' h';
}

return view.extend({
	load: function() {
		return uci.load('quartz-screen').then(function() {
			return fs.exec('/usr/sbin/quartz-screen', [ 'status' ]).catch(function() { return null; });
		});
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null,

	render: function(data) {
		var self = this;
		var st = parseStatus(data);
		var theme = 'dark';
		var statusGrid = E('div', { 'class': 'qz-screen-grid' });
		var previewBox = E('div', { 'class': 'qz-screen-previews' });
		var errorsBox = E('div', { 'class': 'qz-screen-errors' });
		var dot = E('span', { 'class': 'qz-dot' });
		var stateText = E('strong', {});
		var refreshPreviews = function() {
			var pages = pageList(self.status);
			previewBox.innerHTML = '';
			pages.forEach(function(id) {
				var img = E('img', { 'alt': id });
				var frame = E('div', { 'class': 'qz-screen-frame' }, [
					img,
					E('div', { 'class': 'cap' }, [
						E('span', {}, label(id)),
						E('span', {}, '320×172')
					])
				]);
				previewBox.appendChild(frame);
				fs.exec('/usr/sbin/quartz-screen', [
					'preview', '--page', id, '--theme', theme,
					'--format', 'base64', '--scale', '2'
				]).then(function(res) {
					if (res.code === 0 && res.stdout)
						img.src = 'data:image/png;base64,' + res.stdout.trim();
				}).catch(function() {});
			});
		};
		var renderStatus = function() {
			var s = self.status;
			var running = !!(s && s.running);
			dot.className = 'qz-dot ' + (running ? 'ok' : 'err');
			stateText.textContent = running ? _('Running') : _('Stopped');
			statusGrid.innerHTML = '';
			statusGrid.appendChild(kv(_('Service'), running ? _('Running') : _('Stopped')));
			statusGrid.appendChild(kv(_('Framebuffer'),
				(s && s.fb ? s.fb : '—') + (s && s.fb_name ? ' · ' + s.fb_name : '')));
			statusGrid.appendChild(kv(_('Resolution'),
				(s && s.width ? s.width : 320) + '×' + (s && s.height ? s.height : 172) +
				(s && s.bpp ? ' @' + s.bpp + 'bpp' : '')));
			statusGrid.appendChild(kv(_('Current page'), s && s.page ? label(s.page) : '—'));
			statusGrid.appendChild(kv(_('Brightness'), s && s.brightness != null ? s.brightness + '%' : '—'));
			statusGrid.appendChild(kv(_('Night mode'), s && s.night ? _('Yes') : _('No')));
			statusGrid.appendChild(kv(_('Last render'), s && s.last_render_at ? formatAge(s.last_render_at) + ' ' + _('ago') : '—'));
			statusGrid.appendChild(kv(_('Data source'),
				!s || !s.snapshot_ok ? _('Unavailable') : (s.stale ? _('Stale') : _('OK'))));
			errorsBox.textContent = (s && s.errors && s.errors.length) ? s.errors.join('\n') : '';
			errorsBox.style.display = (s && s.errors && s.errors.length) ? '' : 'none';
		};
		var refreshStatus = function() {
			return fs.exec('/usr/sbin/quartz-screen', [ 'status' ]).then(function(res) {
				var parsed = parseStatus(res);
				if (parsed)
					self.status = parsed;
				renderStatus();
			}).catch(function() {});
		};
		var onTest = function() {
			return fs.exec('/usr/sbin/quartz-screen', [ 'test' ]).then(function(res) {
				ui.addNotification(null, E('p', {},
					res.code === 0 ? _('Test pattern shown for 8 seconds.') : _('Failed to start test pattern.')),
					res.code === 0 ? 'info' : 'error');
			}).catch(function() {
				ui.addNotification(null, E('p', {}, _('Failed to start test pattern.')), 'error');
			});
		};
		var onRestart = function() {
			return fs.exec('/usr/sbin/quartz-screen', [ 'apply' ]).then(function(res) {
				ui.addNotification(null, E('p', {},
					res.code === 0 ? _('Service restarted.') : _('Failed to restart service.')),
					res.code === 0 ? 'info' : 'error');
				return new Promise(function(resolve) { window.setTimeout(resolve, 800); });
			}).then(refreshStatus);
		};

		var themeSelect = E('select', { 'class': 'cbi-input-select' }, [
			E('option', { value: 'dark' }, _('Dark')),
			E('option', { value: 'light' }, _('Light'))
		]);
		themeSelect.addEventListener('change', function() {
			theme = themeSelect.value;
			refreshPreviews();
		});

		var root = E('div', { 'id': 'qz-screen-status-page' }, [
			E('style', {}, CSS),
			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Status')),
				E('div', { 'class': 'qz-screen-head' }, [
					dot,
					stateText,
					E('span', { 'class': 'spacer' }),
					E('button', { 'class': 'cbi-button cbi-button-action', 'click': ui.createHandlerFn(this, onTest) }, _('Test pattern')),
					E('button', { 'class': 'cbi-button cbi-button-apply', 'click': ui.createHandlerFn(this, onRestart) }, _('Restart service'))
				]),
				statusGrid,
				errorsBox
			]),
			E('div', { 'class': 'cbi-section' }, [
				E('h3', {}, _('Live preview')),
				E('div', { 'class': 'qz-screen-toolbar' }, [
					themeSelect,
					E('button', { 'class': 'cbi-button', 'click': function(ev) {
						refreshPreviews();
						return false;
					} }, _('Refresh'))
				]),
				previewBox
			])
		]);

		this.status = st;
		renderStatus();
		refreshPreviews();
		poll.add(function() { return refreshStatus(); }, 5);
		return root;
	}
});

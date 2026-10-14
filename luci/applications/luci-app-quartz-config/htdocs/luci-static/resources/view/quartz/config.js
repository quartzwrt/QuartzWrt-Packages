'use strict';
/**
 * luci-app-quartz-config — QuartzWrt 主题配置页
 *
 * ① 外观选项（/etc/config/quartz，section 'theme'）：由主题模板 header.ut 读出并内联为
 *    CSS 变量（--qz-panel-alpha / --qz-blur / 默认外观），保存后下次页面加载生效。
 *    主题未安装本包（或读取失败）时使用内置默认值，两者可独立工作。
 *
 * ② 壁纸（W2.9）：状态即文件——/www/quartz-wallpaper/wallpaper.jpg 存在即启用
 *    （主题 theme.js 探测该固定路径），不占用 uci 选项，因此本块不参与"保存并应用"。
 *    上传经 cgi-upload 落盘，同一请求内 chmod 0644（uhttpd 拒发非全局可读文件，见
 *    cgi-upload 默认 0600）；客户端先做收敛：源文件 ≤25MB（解码护栏）、最长边 ≤1920px、
 *    统一转 JPEG（透明通道压平）、体积 ≤2MB（DESIGN §4 的用户约束）。
 *    投放目录 /www/quartz-wallpaper/ 独立于主题包，apk 升级不覆盖用户文件；
 *    /lib/upgrade/keep.d/quartz-wallpaper 让它在固件 sysupgrade 后也保留。
 */
'require view';
'require form';
'require uci';
'require fs';
'require rpc';
'require ui';
'require request';
'require dom';

/* 固定投放路径（服务端）与访问 URL（uhttpd docroot=/www）。路径是常量字符串，
 * 用户输入不参与构造；rpcd ACL 只逐字授权这一个路径。 */
const WALLPAPER_PATH = '/www/quartz-wallpaper/wallpaper.jpg';
const WALLPAPER_URL = '/quartz-wallpaper/wallpaper.jpg';

const WALLPAPER_SRC_LIMIT = 25 * 1024 * 1024;
const WALLPAPER_MAX_SIZE = 2 * 1024 * 1024;
const WALLPAPER_STEPS = [ [ 1920, 0.9 ], [ 1600, 0.8 ], [ 1280, 0.7 ] ];
const WALLPAPER_EXT = [ '.jpg', '.jpeg', '.png', '.webp', '.gif', '.bmp', '.avif' ];

function formatBytes(bytes) {
	return '%1024.2mB'.format(bytes);
}

function wallpaperUrl(token) {
	return token ? WALLPAPER_URL + '?v=' + token : WALLPAPER_URL;
}

/* 把上传的文件解码成 <img>（objectURL 由调用方释放） */
function decodeWallpaper(file) {
	return new Promise(function (resolve, reject) {
		const url = URL.createObjectURL(file);
		const img = new Image();

		img.onload = function () { resolve({ img: img, url: url }); };
		img.onerror = function () {
			URL.revokeObjectURL(url);
			reject(new Error(_('The selected file is not a decodable image.')));
		};
		img.src = url;
	});
}

/* 画布重编码：先铺主题暗色底（带透明通道的源图在 JPEG 里会压成纯黑），再等比缩放绘制 */
function encodeWallpaper(img, maxSide, quality) {
	const scale = Math.min(1, maxSide / Math.max(img.naturalWidth, img.naturalHeight));
	const canvas = document.createElement('canvas');

	canvas.width = Math.max(1, Math.round(img.naturalWidth * scale));
	canvas.height = Math.max(1, Math.round(img.naturalHeight * scale));

	const ctx = canvas.getContext('2d');
	ctx.fillStyle = '#1c1c1e';
	ctx.fillRect(0, 0, canvas.width, canvas.height);
	ctx.drawImage(img, 0, 0, canvas.width, canvas.height);

	return new Promise(function (resolve, reject) {
		canvas.toBlob(function (blob) {
			if (blob)
				resolve({ blob: blob, width: canvas.width, height: canvas.height });
			else
				reject(new Error(_('Failed to encode the wallpaper image.')));
		}, 'image/jpeg', quality);
	});
}

/* 客户端收敛：逐档降尺寸/降质，直到落进 2MB 预算 */
function prepareWallpaper(file) {
	if (!file)
		return Promise.reject(new Error(_('No file selected.')));

	const type = (file.type || '').toLowerCase();
	const name = (file.name || '').toLowerCase();
	let isImage = (type.indexOf('image/') === 0);

	if (!isImage)
		for (let i = 0; i < WALLPAPER_EXT.length; i++)
			if (name.endsWith(WALLPAPER_EXT[i])) { isImage = true; break; }

	if (!isImage)
		return Promise.reject(new Error(_('Please choose an image file (JPEG, PNG or WebP).')));

	if (file.size > WALLPAPER_SRC_LIMIT)
		return Promise.reject(new Error(
			_('The source image exceeds %s and will not be processed.').format(formatBytes(WALLPAPER_SRC_LIMIT))));

	return decodeWallpaper(file).then(function (decoded) {
		let chain = Promise.resolve(null);

		WALLPAPER_STEPS.forEach(function (step) {
			chain = chain.then(function (best) {
				if (best && best.blob.size <= WALLPAPER_MAX_SIZE)
					return best;
				return encodeWallpaper(decoded.img, step[0], step[1]);
			});
		});

		return chain.then(function (best) {
			URL.revokeObjectURL(decoded.url);

			if (!best || best.blob.size > WALLPAPER_MAX_SIZE)
				throw new Error(_('The image is still larger than %s after re-encoding; please pick a simpler image.')
					.format(formatBytes(WALLPAPER_MAX_SIZE)));

			return best;
		}, function (err) {
			URL.revokeObjectURL(decoded.url);
			throw err;
		});
	});
}

/* multipart 直传 cgi-upload：filemode 必须排在 filedata 之前（cgi-io 在字段落入时解析、
 * 拷贝阶段 chmod），缺失即 0600 → uhttpd 403。 */
function uploadWallpaper(blob, progressNode) {
	const data = new FormData();

	data.append('sessionid', rpc.getSessionID());
	data.append('filename', WALLPAPER_PATH);
	data.append('filemode', '644');
	data.append('filedata', blob, 'wallpaper.jpg');

	return request.post(L.env.cgi_base + '/cgi-upload', data, {
		timeout: 0,
		progress: function (ev) {
			if (!progressNode || !ev.total)
				return;

			const percent = (ev.loaded / ev.total) * 100;

			progressNode.setAttribute('title', '%.2f%%'.format(percent));
			progressNode.firstElementChild.style.width = '%.2f%%'.format(percent);
		}
	}).then(function (res) {
		const reply = res.json();

		if (reply && reply.failure)
			throw new Error(reply.message || _('Upload request failed'));

		return reply || {};
	});
}

/* 删除：NotFound 视为已达成（幂等），其余失败原样抛出（只读/满盘/被占用不得报成功） */
function removeWallpaper() {
	return fs.remove(WALLPAPER_PATH).catch(function (err) {
		if (err && err.name === 'NotFoundError')
			return 0;
		throw err;
	});
}

/* 本页即时生效：直接改写 :root 上的壁纸层变量与 [data-qz-wallpaper] 开关
 * （与 cascade.css + theme.js 的探测结果等效），无需刷新即可看到新壁纸 */
function applyWallpaperLive(token) {
	const root = document.documentElement;

	if (token) {
		root.setAttribute('data-qz-wallpaper', '');
		root.style.setProperty('--qz-wallpaper-on', "url('%s')".format(wallpaperUrl(token)));
	}
	else {
		root.removeAttribute('data-qz-wallpaper');
		root.style.removeProperty('--qz-wallpaper-on');
	}
}

return view.extend({
	render() {
		const m = new form.Map('quartz', _('Quartz Theme'),
			_('Appearance options for the QuartzWrt LuCI theme. Changes take effect on the next page load.'));

		const s = m.section(form.NamedSection, 'theme', 'theme', _('Appearance'),
			_('The whole interface turns translucent when opacity is below 100%; content panels additionally get a backdrop blur.'));

		let o = s.option(form.Value, 'panel_opacity', _('Interface opacity'),
			_('Opacity of content sections, the sidebar and the top bar, in percent. Lower values let the wallpaper show through.'));
		o.datatype = 'range(50,100)';
		o.default = '100';
		o.rmempty = false;

		o = s.option(form.Value, 'blur', _('Blur strength'),
			_('Backdrop blur for the sidebar, top bar and translucent panels, in pixels.'));
		o.datatype = 'range(0,40)';
		o.default = '20';
		o.rmempty = false;

		o = s.option(form.ListValue, 'default_mode', _('Default appearance'),
			_('Applied to first-time visitors; a choice made in the browser overrides it.'));
		o.value('auto', _('Follow system'));
		o.value('dark', _('Dark'));
		o.value('light', _('Light'));
		o.default = 'auto';
		o.rmempty = false;

		o = s.option(form.DummyValue, '_wallpaper', _('Wallpaper'),
			_('A single image used as the desktop background, shown below the translucent panels. It is stored on the device and takes effect immediately, without saving this form.'));
		o.cfgvalue = function () { return null; };
		o.parse = function () { return Promise.resolve(); };
		o.render = L.bind(this.renderWallpaper, this);

		return m.render();
	},

	renderWallpaper() {
		const preview = E('div', {
			'id': 'qz-wallpaper-preview',
			'style': 'position:relative;display:flex;align-items:center;justify-content:center;' +
				'width:100%;max-width:420px;aspect-ratio:16/9;overflow:hidden;' +
				'border:1px solid var(--qz-border, rgba(128,128,128,.25));' +
				'border-radius:var(--qz-radius, 12px);background:var(--qz-bg-well, rgba(128,128,128,.08))'
		});

		const image = E('img', {
			'id': 'qz-wallpaper-image',
			'style': 'display:none;width:100%;height:100%;object-fit:cover',
			'alt': ''
		});

		const placeholder = E('span', {
			'style': 'padding:0 1em;text-align:center;font-size:90%;color:var(--qz-text-2, inherit);opacity:.75'
		}, [ _('No custom wallpaper — the default quartz noise texture is used.') ]);

		const status = E('div', { 'id': 'qz-wallpaper-status', 'style': 'font-size:90%;color:var(--qz-text-2, inherit)' });

		const uploadBtn = E('button', {
			'id': 'qz-wallpaper-upload',
			'class': 'cbi-button cbi-button-action',
			'click': ui.createHandlerFn(this, function (ev) {
				ev.preventDefault();
				input.click();
			})
		}, [ _('Choose image…') ]);

		const removeBtn = E('button', {
			'id': 'qz-wallpaper-remove',
			'class': 'cbi-button cbi-button-remove',
			'style': 'display:none',
			'click': ui.createHandlerFn(this, this.handleWallpaperRemove.bind(this))
		}, [ _('Remove') ]);

		const input = E('input', {
			'id': 'qz-wallpaper-input',
			'type': 'file',
			'accept': 'image/*',
			'style': 'display:none',
			'change': ui.createHandlerFn(this, function (ev) {
				const file = ev.target.files[0];
				ev.target.value = '';
				return this.handleWallpaperUpload(file);
			})
		});

		preview.appendChild(image);
		preview.appendChild(placeholder);

		const widget = E('div', { 'style': 'display:flex;flex-direction:column;gap:.75em' }, [
			preview,
			status,
			E('div', { 'style': 'display:flex;flex-wrap:wrap;gap:.5em' }, [ uploadBtn, removeBtn, input ]),
			E('div', {
				'style': 'font-size:85%;opacity:.7;max-width:420px'
			}, [ _('Uploads are re-encoded in the browser: longest side capped at 1920 px, converted to JPEG, at most 2 MB. Devices showing the old image may need a hard refresh.') ])
		]);

		this.wallpaper = {
			image: image,
			placeholder: placeholder,
			status: status,
			removeBtn: removeBtn,
			input: input
		};

		this.refreshWallpaperState();

		return E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, [ _('Wallpaper') ]),
			E('div', { 'class': 'cbi-value-field' }, [
				widget,
				E('div', { 'style': 'font-size:85%;opacity:.7;margin-top:.5em;max-width:420px' },
					[ _('Stored at %s on the device, independent of the theme package; it survives package updates.').format(WALLPAPER_PATH) ])
			])
		]);
	},

	/* 展示当前状态：文件即状态，缺失（NotFound）与读取失败分开呈现 */
	refreshWallpaperState() {
		const ui_ = this.wallpaper;

		return fs.stat(WALLPAPER_PATH).then(function (stat) {
			ui_.removeBtn.style.display = '';
			ui_.placeholder.style.display = 'none';
			ui_.image.style.display = '';
			ui_.status.textContent = _('Current: %s, updated %s.')
				.format(formatBytes(stat.size), new Date(stat.mtime * 1000).toLocaleString());

			ui_.image.onload = function () {
				ui_.status.textContent = _('Current: %dx%d px, %s, updated %s.').format(
					ui_.image.naturalWidth, ui_.image.naturalHeight,
					formatBytes(stat.size), new Date(stat.mtime * 1000).toLocaleString());
			};

			ui_.image.src = wallpaperUrl(stat.mtime);
		}, function (err) {
			if (err && err.name !== 'NotFoundError')
				ui_.status.textContent = _('Unable to read the wallpaper file: %s').format(err.message);
		});
	},

	handleWallpaperUpload(file) {
		const ui_ = this.wallpaper;
		const progress = E('div', { 'class': 'cbi-progressbar', 'title': '0%' }, E('div', { 'style': 'width:0' }));

		function fail(err) {
			ui.addNotification(null, E('p', [ _('Wallpaper upload failed: %s').format(err.message) ]));
		}

		if (!file)
			return Promise.resolve();

		return prepareWallpaper(file).then(function (prepared) {
			ui.showModal(_('Uploading wallpaper…'), [ progress ]);

			return uploadWallpaper(prepared.blob, progress).then(function (reply) {
				ui.hideModal();

				ui_.removeBtn.style.display = '';
				ui_.placeholder.style.display = 'none';
				ui_.image.style.display = '';
				ui_.image.onload = null;
				ui_.image.src = wallpaperUrl(reply.checksum);
				ui_.status.textContent = _('Current: %dx%d px, %s, just uploaded.').format(
					prepared.width, prepared.height, formatBytes(prepared.blob.size));

				applyWallpaperLive(reply.checksum);

				ui.addNotification(null, E('p', [ _('Wallpaper updated.') ]), 'info');
			}, function (err) {
				ui.hideModal();
				throw err;
			});
		}).catch(fail);
	},

	handleWallpaperRemove() {
		const ui_ = this.wallpaper;

		ui.showModal(_('Remove wallpaper'), [
			E('p', [ _('The uploaded wallpaper will be deleted from the device. QuartzWrt falls back to the default quartz noise texture.') ]),
			E('div', { 'class': 'right' }, [
				E('button', { 'class': 'btn', 'click': ui.hideModal }, [ _('Cancel') ]),
				' ',
				E('button', {
					'class': 'btn cbi-button cbi-button-remove',
					'click': ui.createHandlerFn(this, function () {
						ui.hideModal();

						return removeWallpaper().then(function () {
							ui_.removeBtn.style.display = 'none';
							ui_.image.onload = null;
							ui_.image.removeAttribute('src');
							ui_.image.style.display = 'none';
							ui_.placeholder.style.display = '';
							ui_.status.textContent = '';

							applyWallpaperLive(null);

							ui.addNotification(null, E('p', [ _('Wallpaper removed.') ]), 'info');
						}, function (err) {
							ui.addNotification(null, E('p', [ _('Failed to remove the wallpaper: %s').format(err.message) ]));
						});
					})
				}, [ _('Remove') ])
			])
		]);
	}
});

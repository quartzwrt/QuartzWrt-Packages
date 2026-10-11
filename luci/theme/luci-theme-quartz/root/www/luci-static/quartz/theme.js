'use strict';
/*
 * luci-theme-quartz — 主题运行时交互
 *
 * 职责:
 *   1) 顶栏三态外观切换（跟随系统 / 深色 / 浅色），手动选择存入 localStorage('qz-theme')
 *   2) 自备壁纸探测: /quartz-wallpaper/wallpaper.jpg 存在时启用壁纸层与 scrim
 *   3) 移动端侧边栏开合
 *
 * 注意: 首绘前的外观（data-darkmode / data-qz-mode）由 header 模板中的内联
 * 脚本设置，避免闪烁；本文件只接管交互，不做首绘。
 */
(function () {
	var root = document.documentElement;
	var KEY = 'qz-theme';
	var MODES = ['auto', 'dark', 'light'];

	function mq() {
		return window.matchMedia('(prefers-color-scheme: dark)');
	}

	function currentMode() {
		var m = root.getAttribute('data-qz-mode');
		return MODES.indexOf(m) >= 0 ? m : 'auto';
	}

	function applyMode(mode) {
		root.setAttribute('data-qz-mode', mode);
		root.setAttribute('data-darkmode',
			(mode === 'dark' || (mode === 'auto' && mq().matches)) ? 'true' : 'false');

		try {
			if (mode === 'auto')
				localStorage.removeItem(KEY);
			else
				localStorage.setItem(KEY, mode);
		} catch (e) { /* 隐私模式下忽略 */ }

		updateToggle(mode);
	}

	function updateToggle(mode) {
		var btn = document.getElementById('qz-theme-toggle');
		if (!btn)
			return;

		btn.setAttribute('data-mode', mode);
		btn.title = (mode === 'auto') ? 'Appearance: Auto'
			: (mode === 'dark') ? 'Appearance: Dark'
			: 'Appearance: Light';
	}

	function initToggle() {
		var btn = document.getElementById('qz-theme-toggle');
		if (!btn)
			return;

		updateToggle(currentMode());

		btn.addEventListener('click', function () {
			applyMode(MODES[(MODES.indexOf(currentMode()) + 1) % MODES.length]);
		});

		var m = mq();
		var onChange = function () {
			if (currentMode() === 'auto')
				applyMode('auto');
		};

		if (m.addEventListener)
			m.addEventListener('change', onChange);
		else if (m.addListener)
			m.addListener(onChange);
	}

	function initNavToggle() {
		var btn = document.getElementById('qz-nav-toggle');
		if (btn)
			btn.addEventListener('click', function () {
				root.classList.toggle('qz-nav-open');
			});

		var veil = document.getElementById('qz-nav-veil');
		if (veil)
			veil.addEventListener('click', function () {
				root.classList.remove('qz-nav-open');
			});

		var nav = document.getElementById('qz-nav');
		if (nav)
			nav.addEventListener('click', function (ev) {
				if (ev.target.closest('a'))
					root.classList.remove('qz-nav-open');
			});
	}

	function detectWallpaper() {
		var img = new Image();
		img.onload = function () {
			root.setAttribute('data-qz-wallpaper', '');
		};
		img.src = '/quartz-wallpaper/wallpaper.jpg';
	}

	function init() {
		initToggle();
		initNavToggle();
		detectWallpaper();
	}

	if (document.readyState === 'loading')
		document.addEventListener('DOMContentLoaded', init);
	else
		init();
})();

'use strict';
/*
 * luci-theme-quartz — 登录页视图（品牌时刻）
 * 逻辑同 luci-theme-bootstrap 的 sysauth：把 sysauth.ut 里的隐藏表单
 * 提升为模态；区别是注入品牌区（石英晶体 logo + wordmark）。
 */
'require ui';
'require view';

return view.extend({
	render() {
		const form = document.querySelector('form');
		const btn = document.querySelector('button');

		const dlg = ui.showModal(
			_('Authorization Required'),
			Array.from(document.querySelectorAll('section > *')),
			'login'
		);

		/* 品牌区：inline SVG（随主题取色）+ wordmark。
		 * 注意：LuCI 的 E() 只做 setAttribute，innerHTML 必须用属性赋值而非 attrs 键
		 * （2026-10-12 真机实测：E('span', { innerHTML: ... }) 不会渲染出图标） */
		const mark = E('span');
		mark.innerHTML =
			'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" ' +
			'stroke-linejoin="round" aria-hidden="true">' +
			'<path d="M12 2 L20 6.6 V17.4 L12 22 L4 17.4 V6.6 Z"/>' +
			'<path d="M4 6.6 L12 11.6 L20 6.6"/>' +
			'<path d="M12 11.6 V22" opacity=".45"/>' +
			'</svg>';

		dlg.insertBefore(E('div', { 'class': 'qz-login-brand' }, [
			mark,
			E('div', { 'class': 'qz-brand-word' }, [ 'QuartzWrt' ])
		]), dlg.firstChild);

		form.addEventListener('keypress', (ev) => {
			if (ev.key === 'Enter')
				btn.click();
		});

		btn.addEventListener('click', () => {
			dlg.querySelectorAll('*').forEach((node) => {
				node.style.display = 'none';
			});
			dlg.appendChild(E('div', {
				class: 'spinning'
			}, _('Logging in…')));

			form.submit();
		});

		document.querySelector('input[type="password"]').focus();

		return '';
	},

	addFooter() {},

});

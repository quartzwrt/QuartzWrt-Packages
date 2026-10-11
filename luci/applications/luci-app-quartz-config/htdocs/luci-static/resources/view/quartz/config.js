'use strict';
/**
 * luci-app-quartz-config — QuartzWrt 主题配置页
 *
 * 选项存放于 /etc/config/quartz（section 'theme'），由主题模板 header.ut
 * 读出并内联为 CSS 变量（--qz-panel-alpha / --qz-blur / 默认外观），
 * 保存后在下次页面加载时生效。主题未装本包时使用内置默认值。
 */
'require view';
'require form';
'require uci';

return view.extend({
	render() {
		const m = new form.Map('quartz', _('Quartz Theme'),
			_('Appearance options for the QuartzWrt LuCI theme. Changes take effect on the next page load.'));

		const s = m.section(form.NamedSection, 'theme', 'theme', _('Appearance'),
			_('Content panels turn translucent when opacity is below 100%, and backdrop blur engages automatically.'));

		let o = s.option(form.Value, 'panel_opacity', _('Panel opacity'),
			_('Opacity of content sections and cards, in percent. Lower values let the wallpaper show through.'));
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

		return m.render();
	}
});

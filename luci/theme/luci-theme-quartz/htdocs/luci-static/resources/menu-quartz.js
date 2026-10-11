'use strict';
/*
 * luci-theme-quartz — 侧边栏菜单渲染
 * 结构参考 luci-theme-bootstrap 的 menu-bootstrap.js，适配为 macOS 式侧边栏：
 *   - 主菜单: 当前模式（admin 等）的类别 → 分组标题 + 页面条目
 *   - 页签: 仍渲染到 #tabmenu（内容区顶部）
 *   - 模式菜单: 渲染到侧边栏底部 #modemenu
 */
'require baseclass';
'require ui';

return baseclass.extend({
	__init__() {
		ui.menu.load().then((tree) => this.render(tree));
	},

	render(tree) {
		this.renderModeMenu(tree);
		this.renderSidebar(tree);

		let node = tree;
		let url = '';

		if (L.env.dispatchpath.length >= 3) {
			for (var i = 0; i < 3 && node; i++) {
				node = node.children[L.env.dispatchpath[i]];
				url = url + (url ? '/' : '') + L.env.dispatchpath[i];
			}

			if (node)
				this.renderTabMenu(node, url);
		}
	},

	/* 当前激活的模式名（requestpath[0]，否则第一个可见模式） */
	activeModeName(tree) {
		const children = ui.menu.getChildren(tree);

		if (!children.length)
			return null;

		if (L.env.requestpath.length) {
			const name = L.env.requestpath[0];
			if (children.some((c) => c.name === name))
				return name;
		}

		return children[0].name;
	},

	renderSidebar(tree) {
		const container = document.querySelector('#qz-nav');
		if (!container)
			return;

		const modeName = this.activeModeName(tree);
		const modeNode = modeName ? tree.children[modeName] : null;
		if (!modeNode)
			return;

		const dispatch = L.env.dispatchpath;

		ui.menu.getChildren(modeNode).forEach((cat) => {
			const pages = ui.menu.getChildren(cat);
			const items = pages.length ? pages : [cat];

			const group = E('div', { 'class': 'qz-nav-group' });

			if (pages.length)
				group.appendChild(E('h6', { 'class': 'qz-nav-title' }, [ _(cat.title) ]));

			const ul = E('ul');
			items.forEach((item) => {
				const href = pages.length
					? L.url(modeName, cat.name, item.name)
					: L.url(modeName, cat.name);

				const active = pages.length
					? (dispatch[0] === modeName && dispatch[1] === cat.name && dispatch[2] === item.name)
					: (dispatch[0] === modeName && dispatch[1] === cat.name);

				ul.appendChild(E('li', { 'class': 'qz-nav-item' + (active ? ' active' : '') }, [
					E('a', { href: href }, [ _(item.title) ])
				]));
			});

			group.appendChild(ul);
			container.appendChild(group);
		});
	},

	renderTabMenu(tree, url, level) {
		const container = document.querySelector('#tabmenu');
		if (!container)
			return E([]);

		const ul = E('ul', { 'class': 'tabs' });
		const children = ui.menu.getChildren(tree);
		let activeNode = null;

		children.forEach(child => {
			const isActive = (L.env.dispatchpath[3 + (level || 0)] == child.name);
			const activeClass = isActive ? ' active' : '';

			ul.appendChild(E('li', { 'class': 'tabmenu-item-%s%s'.format(child.name, activeClass) }, [
				E('a', { 'href': L.url(url, child.name) }, [ _(child.title) ])
			]));

			if (isActive)
				activeNode = child;
		});

		if (ul.children.length == 0)
			return E([]);

		container.appendChild(ul);
		container.style.display = '';

		if (activeNode)
			this.renderTabMenu(activeNode, url + '/' + activeNode.name, (level || 0) + 1);

		return ul;
	},

	renderModeMenu(tree) {
		const ul = document.querySelector('#modemenu');
		if (!ul)
			return;

		const children = ui.menu.getChildren(tree);
		if (children.length < 2)
			return;

		children.forEach((child, index) => {
			const isActive = L.env.requestpath.length
				? child.name === L.env.requestpath[0]
				: index === 0;

			ul.appendChild(E('li', { 'class': isActive ? 'active' : '' }, [
				E('a', { 'href': L.url(child.name) }, [ _(child.title) ])
			]));
		});

		const wrap = ul.closest('.qz-sidebar-foot');
		if (wrap)
			wrap.style.display = '';
	}
});

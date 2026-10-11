#
# luci-theme-quartz — QuartzWrt 全局主题（macOS Vibrancy）
#
# 包结构:
#   root/www/luci-static/quartz/  静态资源（cascade.css/theme.js/噪点/logo）
#       —— 必须走 root/ 原样安装：htdocs/ 会被构建期 csstidy/JsMin 处理，
#          而 csstidy 1.x 不认识 CSS @layer（--remove_last_semicolon 剥掉
#          @layer 语句分号 → 整张样式表解析失败、主题全裸；2026-10-12 容器实测）
#   htdocs/luci-static/resources/ 菜单与视图 JS（menu-quartz.js、view/quartz/）
#       —— 经 JsMin（classic jsmin），本包 JS 保持 ES5 兼容写法
#   ucode/template/themes/quartz/ ucode 页面模板（LuCI 25.12 模板机制）
#   root/etc/uci-defaults/        首次启动注册主题
#
# 设计规范见 docs/DESIGN.md（仓库内）。
#

include $(TOPDIR)/rules.mk

LUCI_TITLE:=QuartzWrt theme (macOS Vibrancy)
LUCI_DEPENDS:=+luci-base

PKG_LICENSE:=Apache-2.0

define Package/luci-theme-quartz/postrm
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
	uci -q delete luci.themes.Quartz
	uci commit luci
}
endef

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

#
# luci-theme-quartz — QuartzWrt 全局主题（macOS Vibrancy）
#
# 包结构:
#   htdocs/luci-static/quartz/    静态资源（cascade.css/theme.js/噪点/logo）
#   htdocs/luci-static/resources/ 菜单与视图 JS（menu-quartz.js、view/quartz/）
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

#
# luci-app-quartz-screen — 小屏显示控制（WP-16）
#
# 依赖 quartz-screen（守护 + 渲染器）；菜单挂在 系统 → 屏幕显示。
# 只做控制面：状态/实时预览/设置（亮度、夜间、轮播、页面增减、便签）。
#

include $(TOPDIR)/rules.mk

LUCI_TITLE:=QuartzWrt small screen control
LUCI_DEPENDS:=+luci-base +quartz-screen
LUCI_CATEGORY:=QuartzWrt
LUCI_SUBMENU_FORCED:=3. Device-Specific
LUCI_BASENAME:=quartz-screen

PKG_VERSION:=0.1.0
PKG_RELEASE:=4
PKG_LICENSE:=Apache-2.0
PKG_MAINTAINER:=QuartzWrt

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

#
# luci-app-quartz-config — QuartzWrt 主题配置页
#
# 与 luci-theme-quartz 配套的独立配置包：外观选项存放于 /etc/config/quartz
# （section 'theme'），由主题模板在每次页面渲染时读出并内联生效。
# 主题在未安装本包时使用内置默认值，可独立工作。
#

include $(TOPDIR)/rules.mk

LUCI_TITLE:=QuartzWrt theme configuration
LUCI_DEPENDS:=+luci-base +luci-theme-quartz

PKG_LICENSE:=Apache-2.0

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

# QuartzWrt 元数据包：跨双 Feed 的依赖聚合入口（docs/TDD.md §3.2）
#
# 用法：随发行版默认携带的包（quartz feed 核心组件 + thirdparty feed
# 第三方包 + Bridge 适配层）统一追加到下方 DEPENDS，并保持
# "quartz 在前、thirdparty 在后" 的书写顺序便于评审。

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-defaults
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

include $(INCLUDE_DIR)/package.mk

define Package/quartz-defaults
  SECTION:=meta
  CATEGORY:=QuartzWrt
  TITLE:=QuartzWrt Default Configuration and Packages
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
  # 依赖清单随核心组件落地逐步追加（quartz 在前、thirdparty 在后）：
  #   +luci-theme-quartz +luci-app-dashboard +quartz-daemon +quartz-app-api（核心）
  #   +openwrt-bandix +luci-app-bandix（thirdparty，模式 A）
  #   +quartz-ddns-bridge +luci-app-ddns（Bridge + thirdparty，模式 B）
  DEPENDS:= \
    +luci-app-openclash \
    +luci-lib-docker \
    +luci-app-dockerman
endef

define Package/quartz-defaults/description
  QuartzWrt 元数据包：统一声明发行版对 quartz 与 thirdparty 双 Feed 中
  软件包的依赖，并承载默认配置。
endef

define Build/Compile
	true
endef

define Package/quartz-defaults/install
	$(INSTALL_DIR) $(1)/
endef

$(eval $(call BuildPackage,quartz-defaults))

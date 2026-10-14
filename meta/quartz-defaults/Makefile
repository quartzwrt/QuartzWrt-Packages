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
  #   +bandix +luci-app-bandix（thirdparty 受控 Fork 镜像；WP-7 D5 预装）
  #   +quartz-runtime-resources（thirdparty；运行时资源清单 + 探针描述符，WP-7 D2）
  #   +quartz-ddns-bridge +luci-app-ddns（Bridge + thirdparty，模式 B）
  # 当前 DEPENDS 来源：luci-app-openclash 来自基座 luci feed
  # （thirdparty 不收录同名包，会被基座遮蔽，docs/TDD.md §3.1）。
  # docker 栈（luci-app-dockerman / luci-lib-docker）已按用户拍板移出预装
  # （2026-10-12，WORKPLAN §1；docker 与 fw4 冲突的安装配方见坑位 #24）。
  DEPENDS:= \
    +luci-theme-quartz \
    +luci-app-quartz-config \
    +luci-app-dashboard \
    +quartz-daemon \
    +quartz-app-api \
    +luci-app-openclash \
    +bandix \
    +luci-app-bandix \
    +quartz-runtime-resources
endef

define Package/quartz-defaults/description
  QuartzWrt 元数据包：统一声明发行版对 quartz 与 thirdparty 双 Feed 中
  软件包的依赖，并承载默认配置。
endef

define Build/Compile
	true
endef

define Package/quartz-defaults/install
	$(INSTALL_DIR) $(1)/etc
	# 发行版标识：quartz-daemon 读取为 host.release（WP-4 DESIGN §9-D7；
	# 缺失时 daemon 回退 /etc/openwrt_release 的 DISTRIB_DESCRIPTION）
	$(INSTALL_DATA) ./files/quartz-release $(1)/etc/quartz-release
endef

$(eval $(call BuildPackage,quartz-defaults))

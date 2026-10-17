#
# quartz-bandix-bridge —— bandix 的 Bridge 适配层（WP-8，TDD §5 模式 B）
#
# 设计规范: docs/DESIGN.md v1.0
# 形态: rpcd **ucode 插件**——脚本装到 /usr/share/rpcd/ucode/（rpcd 启动时扫描该目录，
#       脚本返回 { 'quartz.bandix': methods } 即注册 ubus 对象；对象名取返回表的键，
#       与 exec 插件"文件名即对象名"不同。基座自带同款先例：luci-app-ddns 的 ddns.uc）。
# 上游: 内部委托 luci-app-bandix 的 rpcd 薄壳 luci.bandix（数据源 = bandix 核心本地
#       HTTP API 127.0.0.1:8686）——网页端与 App 端落在同一份实现上（一致性）。
# 纪律: 入参全量白名单校验、不拼接 shell、动作面收敛（不暴露 installUpdate/clearData/
#       deleteDevice/checkUpdate）、写动作落 logd；ACL 不投放（默认拒绝，仅 root 侧可调）。
# 单测: dev/unit-test.sh（宿主跑目标 ucode，桩替换 ubus/log；不进安装包）。
#

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-bandix-bridge
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

PKG_LICENSE:=GPL-2.0-only
PKG_MAINTAINER:=QuartzWrt <https://github.com/quartzwrt>

include $(INCLUDE_DIR)/package.mk

define Package/quartz-bandix-bridge
  SECTION:=utils
  CATEGORY:=QuartzWrt
  TITLE:=QuartzWrt bridge adapter for bandix (stable ubus contract)
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
  DEPENDS:=+ucode +ucode-mod-ubus +bandix +luci-app-bandix
endef

define Package/quartz-bandix-bridge/description
  bandix 的 Bridge 适配层（WP-8）：把上游 LuCI 的 rpcd 薄壳（luci.bandix）收敛为稳定的
  ubus 契约 quartz.bandix（读 10 + 写 10），供 quartz-app-api 转发给 App；同时投放
  uci-defaults 把 bandix 流量监控置为出厂默认开启。
endef

define Build/Compile
	true
endef

define Package/quartz-bandix-bridge/install
	$(INSTALL_DIR) $(1)/usr/share/rpcd/ucode
	$(INSTALL_DATA) ./files/usr/share/rpcd/ucode/quartz-bandix-bridge.uc \
		$(1)/usr/share/rpcd/ucode/

	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/etc/uci-defaults/99-quartz-bandix $(1)/etc/uci-defaults/
endef

$(eval $(call BuildPackage,quartz-bandix-bridge))

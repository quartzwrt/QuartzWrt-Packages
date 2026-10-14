# QuartzWrt 元数据包：跨双 Feed 的依赖聚合入口（docs/TDD.md §3.2）
#
# 用法：随发行版默认携带的包（quartz feed 核心组件 + thirdparty feed
# 第三方包 + Bridge 适配层）统一追加到下方 DEPENDS，并保持
# "quartz 在前、thirdparty 在后" 的书写顺序便于评审。

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-defaults
PKG_VERSION:=0.1.0
PKG_RELEASE:=3

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

# WP-9 签名与 OTA 接入（Builder/docs/SIGNING-OTA.md）：
#   QUARTZ_FEED_BASE_URL —— QuartzWrt 自有 apk feed 根 URL（D10 阶段 1 开关）。
#     留空（默认）= 不写 feed 行（安全默认：feed 未发布前不引入死链，坑位 #41 不复现）；
#     发布后填 https://<托管根>（如 R2 域名）→ 安装 /etc/apk/repositories.d/quartz-feed.list，
#     设备即从自有源取 quartz/thirdparty/目标包（索引签名由预置公钥 quartz.pem 校验）。
#     阶段 2（全量自包含）另配 CONFIG_VERSION_REPO + CONFIG_FEED_{quartz,thirdparty}=y。
define Package/quartz-defaults/config
	config QUARTZ_FEED_BASE_URL
		string "QuartzWrt apk feed base URL (empty = disabled)"
		default ""
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
	# WP-9：稳定签名公钥随固件预置（feed 索引签名信任根；apk 按内容 id 匹配，
	# 文件名任意，与 base-files 自动投放的 public-key.pem 并存）
	$(INSTALL_DIR) $(1)/etc/apk/keys
	$(INSTALL_DATA) ./files/etc/apk/keys/quartz.pem $(1)/etc/apk/keys/quartz.pem
	# WP-9：OTA 清单验签公钥（usign/Ed25519；设备端 usign 随基座自带）
	$(INSTALL_DATA) ./files/etc/quartz-ota.pub $(1)/etc/quartz-ota.pub
	# WP-9：OTA 通道原型（检查/执行固件升级；信任链 = 清单签名 → 镜像 sha256）
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) ./files/usr/sbin/quartz-ota-update $(1)/usr/sbin/quartz-ota-update
	# D10 阶段 1：自有 feed 接入（仅当配置了根 URL；留空则不落文件，防死链 #41）
	if [ -n "$(CONFIG_QUARTZ_FEED_BASE_URL)" ]; then \
		$(INSTALL_DIR) $(1)/etc/apk/repositories.d; \
		{ echo "# QuartzWrt 自有 feed（D10 阶段 1；由 quartz-defaults 生成）"; \
		  echo "$(CONFIG_QUARTZ_FEED_BASE_URL)/packages/$(ARCH_PACKAGES)/quartz/packages.adb"; \
		  echo "$(CONFIG_QUARTZ_FEED_BASE_URL)/packages/$(ARCH_PACKAGES)/thirdparty/packages.adb"; \
		  echo "$(CONFIG_QUARTZ_FEED_BASE_URL)/targets/$(BOARD)/$(SUBTARGET)/packages/packages.adb"; \
		} > $(1)/etc/apk/repositories.d/quartz-feed.list; \
		# OTA 源同根：quartz-ota-update 缺省读本文件（uci quartz.ota.url 可覆盖）
		echo "$(CONFIG_QUARTZ_FEED_BASE_URL)" > $(1)/etc/quartz-ota.url; \
	fi
endef

$(eval $(call BuildPackage,quartz-defaults))

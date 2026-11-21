#
# quartz-docker — 可选 docker 栈（WP-14，2026-11-21 用户拍板「docker 做可选小包」）
#
# 定位：docker 不再作为固件构建变体（标准版镜像不含 docker）。需要 docker 的设备
#   在设备上 `apk add quartz-docker`（app-api 软件包面/命令行均可），由本 meta 拉齐
#   基座 feed 的 docker 栈，并投放 nftables 后端配方（WORKPLAN 坑位 #24）。
#
# 依赖来源：docker/dockerd/containerd/runc/docker-compose 来自基座 packages feed；
#   luci-app-dockerman 来自基座 luci feed（25.12 已不依赖 luci-lib-docker）。
#   不预装：quartz-defaults 不引用本包；check-manifest.sh 反向断言其不在固件清单。
#
# ⚠ 配方要点（坑位 #24）：daemon.json 里 "iptables": true 必须保持——置 false 会把
#   nft 后端的 IPv4 编程一起静默关掉；只关 init 层开关（dockerd.globals.iptables=0）
#   并把后端选择交给 daemon.json 的 "firewall-backend": "nftables"。
#

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-docker
PKG_VERSION:=0.1.0
PKG_RELEASE:=1
PKG_LICENSE:=Apache-2.0
PKG_MAINTAINER:=QuartzWrt

include $(INCLUDE_DIR)/package.mk

define Package/quartz-docker
  SECTION:=meta
  CATEGORY:=QuartzWrt
  SUBMENU:=2. Advanced
  TITLE:=Docker stack (optional)
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  PKGARCH:=all
  # 运行时依赖（apk 元数据）用 EXTRA_DEPENDS：不进入 Kconfig，也不生成
  # .packagedeps 编译依赖边——单包编译只编本 meta，不会连带构建 docker 栈。
  # apk 要求 EXTRA_DEPENDS 每项带版本约束，这里用 >=0（基座 feed 版本恒满足）。
  # 代价：menuconfig 勾选本包只装 nftables 配方；docker 栈仍需同时勾选或运行时
  # `apk add quartz-docker`（apk 会按本元数据自动拉齐依赖）。
  EXTRA_DEPENDS:=docker (>=0),dockerd (>=0),containerd (>=0),runc (>=0),docker-compose (>=0),luci-app-dockerman (>=0)
endef

define Package/quartz-docker/description
  可选 docker 栈安装入口：可在 menuconfig 勾选随镜像安装；标准版固件不预装，
  发布构建以 feed-only 单包编译编入软件源——设备上 `apk add quartz-docker` 从基座
  feed 拉取 docker/dockerd/containerd/runc/docker-compose + LuCI dockerman 页，并
  投放 nftables 后端配方（/etc/docker/daemon.json + 99-docker-nftables uci-defaults，
  避免 fw4「检测到旧版规则」告警，WORKPLAN 坑位 #24）。
endef

define Package/quartz-docker/conffiles
/etc/docker/daemon.json
endef

define Build/Compile
	true
endef

define Package/quartz-docker/install
	$(INSTALL_DIR) $(1)/etc/docker $(1)/etc/uci-defaults
	$(INSTALL_DATA) ./files/etc/docker/daemon.json $(1)/etc/docker/daemon.json
	$(INSTALL_BIN) ./files/etc/uci-defaults/99-docker-nftables $(1)/etc/uci-defaults/99-docker-nftables
endef

$(eval $(call BuildPackage,quartz-docker))

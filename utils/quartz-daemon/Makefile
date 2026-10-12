#
# quartz-daemon — QuartzWrt 数据聚合守护进程（WP-4）
#
# 设计规范: docs/DESIGN.md（v0.3 讨论稿，按推荐案实施）
# 数据契约: ../luci/applications/luci-app-dashboard/docs/AGGREGATE-API.md（冻结 v1，本包为生产者）
#
# 说明:
#   - in-tree Go 源码（src/）：Build/Prepare 拷贝进 PKG_BUILD_DIR；
#     交叉编译走基座 golang/host（feeds/packages/lang/golang，Go 1.26）
#   - 零第三方 Go 依赖（stdlib-only）：无 vendor / 无 go.sum，构建零网络
#   - "静态编译"的准确语义：单一自包含二进制；feed 默认 CGO_ENABLED=1 外链，
#     实际动态链接系统 musl（与 tailscale 等 feed 内 Go 包一致）
#   - 资产仅为文本文件（config/probes.d），无 htdocs 产物 → 不涉 csstidy 坑位
#

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-daemon
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

PKG_LICENSE:=GPL-2.0-only
PKG_MAINTAINER:=QuartzWrt

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

GO_PKG:=github.com/quartzwrt/quartz-daemon
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/quartz-daemon
# -X 目标：main 包的符号名是 "main.<var>"（完整 import path 写法对 main 包
# 静默失配，实测无效）；feed 内 Go 包同款惯例，如 gotop: main.Version
GO_PKG_LDFLAGS_X:= \
	main.version=$(PKG_VERSION) \
	main.release=$(PKG_RELEASE)
GO_PKG_INSTALL_BIN_PATH:=/usr/sbin

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/quartz-daemon
  SECTION:=utils
  CATEGORY:=QuartzWrt
  TITLE:=QuartzWrt data aggregation daemon
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  DEPENDS:=$(GO_ARCH_DEPENDS) +ubus
endef

define Package/quartz-daemon/description
  QuartzWrt 数据聚合守护进程：定时采集系统负载、网络吞吐、Docker 状态与
  在线设备，输出单一 JSON 快照（/var/run/quartz/status.json 原子写 + 回环
  HTTP GET /api/v1/status），供 Dashboard（rpcd 文件读取）与 App（经 WP-5）
  消费。契约见 luci-app-dashboard/docs/AGGREGATE-API.md（冻结 v1）。
endef

define Package/quartz-daemon/conffiles
/etc/config/quartz-daemon
endef

# in-tree 源码：直接拷贝工作副本（不走自我引用 git 下载），改动即编译
define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./src/* $(PKG_BUILD_DIR)/
endef

define Package/quartz-daemon/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) $(GO_PKG_BUILD_BIN_DIR)/quartz-daemon $(1)/usr/sbin/
	$(INSTALL_DIR) $(1)/etc/init.d $(1)/etc/config
	$(INSTALL_BIN) ./files/quartz-daemon.init $(1)/etc/init.d/quartz-daemon
	$(INSTALL_CONF) ./files/quartz-daemon.conf $(1)/etc/config/quartz-daemon
	$(INSTALL_DIR) $(1)/usr/share/quartz-daemon/probes.d
	$(INSTALL_DATA) ./files/probes.d/openclash.json $(1)/usr/share/quartz-daemon/probes.d/
	$(INSTALL_DATA) ./files/probes.d/dockerd.json $(1)/usr/share/quartz-daemon/probes.d/
endef

$(eval $(call BuildPackage,quartz-daemon))

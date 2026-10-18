#
# quartz-app-api — QuartzWrt 专属 App API（rpcd exec 插件，WP-5）
#
# 设计规范: docs/DESIGN.md v1.1（定案）
# 数据契约: luci-app-dashboard/docs/AGGREGATE-API.md（冻结 v1，本包为代理透传）
#
# 说明:
#   - 形态（DESIGN §2）：rpcd exec 插件——二进制装到 /usr/libexec/rpcd/quartz-app-api，
#     **文件名即 ubus 对象名**（rpcd 按文件注册）；无常驻进程（每调用 fork 一次）
#   - 暴露面：uhttpd 专用 'app' 实例（443/TLS，由 uci-defaults 注入）；ACL 收敛 =
#     acl.d 以 unauthenticated 组仅授本对象八方法（其余 ubus 面由 rpcd 拒绝）
#   - 认证：JWT(15min)+refresh(旋转/复用检测/可吊销)，状态在 /etc/quartz-app-api/
#   - 依赖：uhttpd-mod-ubus（/ubus 端点）+ libustream-openssl/px5g-openssl（TLS 证书
#     自动生成链）+ umdns（局域网发现宣告）
#   - in-tree Go 源码（src/）走基座 golang/host 交叉编译；零第三方 Go 依赖
#

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-app-api
PKG_VERSION:=0.1.0
PKG_RELEASE:=12

PKG_LICENSE:=GPL-2.0-only
PKG_MAINTAINER:=QuartzWrt

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

GO_PKG:=github.com/quartzwrt/quartz-app-api
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/quartz-app-api
GO_PKG_LDFLAGS_X:= \
	main.version=$(PKG_VERSION) \
	main.release=$(PKG_RELEASE)
GO_PKG_INSTALL_BIN_PATH:=/usr/libexec/rpcd

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/quartz-app-api
  SECTION:=utils
  CATEGORY:=QuartzWrt
  TITLE:=QuartzWrt app API (rpcd plugin, JWT auth)
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  DEPENDS:=$(GO_ARCH_DEPENDS) +uhttpd +uhttpd-mod-ubus +rpcd \
	+libustream-openssl +px5g-openssl +umdns
endef

define Package/quartz-app-api/description
  QuartzWrt 专属 App API：rpcd exec 插件（ubus 对象 quartz-app-api），基于
  /ubus JSON-RPC（HTTPS 443）。认证 = JWT(短时) + refresh(旋转+吊销)；数据面 =
  代理 quartz-daemon 回环 HTTP 快照（status 方法，与 Dashboard 同源）；控制面 =
  白名单动作（service/system/docker/packages）；软件包管理面 = apk 查询/升级/
  卸载 + 操作记录（packages 方法，v1.1）；发现 = umdns（_quartz-app._tcp，
  pin=TLS 证书 sha256）。设计见 docs/DESIGN.md（v1.1 定案）。
endef

define Package/quartz-app-api/conffiles
/etc/config/quartz-app-api
endef

# 运行时安装：rpcd 重扫插件目录 + 立即执行 uci-defaults（不等重启）
define Package/quartz-app-api/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/rpcd reload >/dev/null 2>&1
if [ -x /etc/uci-defaults/99-quartz-app-api ]; then
	/etc/uci-defaults/99-quartz-app-api
	rm -f /etc/uci-defaults/99-quartz-app-api
fi
exit 0
endef

# in-tree 源码：直接拷贝工作副本（不走自我引用 git 下载），改动即编译
define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./src/* $(PKG_BUILD_DIR)/
endef

define Package/quartz-app-api/install
	$(INSTALL_DIR) $(1)/usr/libexec/rpcd
	$(INSTALL_BIN) $(GO_PKG_BUILD_BIN_DIR)/quartz-app-api $(1)/usr/libexec/rpcd/quartz-app-api
	$(INSTALL_DIR) $(1)/etc/init.d $(1)/etc/config $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/quartz-app-api.init $(1)/etc/init.d/quartz-app-api
	$(INSTALL_CONF) ./files/quartz-app-api.conf $(1)/etc/config/quartz-app-api
	$(INSTALL_BIN) ./files/uci-defaults/99-quartz-app-api $(1)/etc/uci-defaults/99-quartz-app-api
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./files/acl.d/quartz-app-api.json $(1)/usr/share/rpcd/acl.d/quartz-app-api.json
endef

$(eval $(call BuildPackage,quartz-app-api))

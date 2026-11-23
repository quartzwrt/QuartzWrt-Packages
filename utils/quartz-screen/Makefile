#
# quartz-screen — QuartzWrt 小屏渲染守护进程（WP-16）
#
# 设备：NLnet XiGuaPi V3 板载 GC9307 SPI 屏（172x320，横屏 320x172）
# 驱动：设备层 fbtft fb_gc9307（kmod-fb-tft-gc9307），暴露 /dev/fb0
# 用户态：Go stdlib-only，内置位图字体（Noto Sans CJK 子集 + GB2312 兜底）
# 控制面：luci-app-quartz-screen（状态/设置/预览）
#
# 说明：
#   - in-tree Go 源码（src/）：Build/Prepare 拷贝进 PKG_BUILD_DIR；
#     交叉编译走基座 golang/host（feeds/packages/lang/golang，Go 1.26）
#   - 零第三方 Go 依赖（stdlib-only）：无 vendor / 无 go.sum，构建零网络
#   - 字体 atlases 由 dev/fontgen/gen_font.py 生成并随源码提交；
#     构建期不需要 Python/Chromium
#

include $(TOPDIR)/rules.mk

PKG_NAME:=quartz-screen
PKG_VERSION:=0.1.0
PKG_RELEASE:=6

PKG_LICENSE:=GPL-2.0-only
PKG_MAINTAINER:=QuartzWrt

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

GO_PKG:=github.com/quartzwrt/quartz-screen
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/quartz-screen
GO_PKG_LDFLAGS_X:= \
	main.version=$(PKG_VERSION) \
	main.release=$(PKG_RELEASE)
GO_PKG_INSTALL_BIN_PATH:=/usr/sbin

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/quartz-screen
  SECTION:=utils
  CATEGORY:=QuartzWrt
  SUBMENU:=3. Device-Specific
  TITLE:=QuartzWrt small screen renderer
  URL:=https://github.com/quartzwrt/QuartzWrt-Packages
  DEPENDS:=$(GO_ARCH_DEPENDS)
endef

define Package/quartz-screen/description
  QuartzWrt 小屏渲染守护进程：从 quartz-daemon 的 /var/run/quartz/status.json
  读取状态，按 Quartz 视觉 token 渲染 320x172 多页轮播（A+ 信息版式）；
  支持亮度/夜间调光、中英双语、页面增减与排序、运营商图标、bandix Top 设备；
  提供预览/测试图案 CLI 供 LuCI 使用。
endef

define Package/quartz-screen/conffiles
/etc/config/quartz-screen
endef

# 运行时安装/升级：立即启用并重启，避免旧二进制继续跑
define Package/quartz-screen/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/quartz-screen enable >/dev/null 2>&1
/etc/init.d/quartz-screen restart >/dev/null 2>&1
exit 0
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./src/* $(PKG_BUILD_DIR)/
endef

define Package/quartz-screen/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) $(GO_PKG_BUILD_BIN_DIR)/quartz-screen $(1)/usr/sbin/
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/quartz-screen.init $(1)/etc/init.d/quartz-screen
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/quartz-screen.conf $(1)/etc/config/quartz-screen
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/90-quartz-screen $(1)/etc/uci-defaults/90-quartz-screen
	$(INSTALL_DIR) $(1)/usr/share/licenses/quartz-screen
	$(INSTALL_DATA) ./src/internal/render/fontdata/OFL.txt $(1)/usr/share/licenses/quartz-screen/OFL.txt
	$(INSTALL_DATA) ./src/internal/render/fontdata/NOTICE $(1)/usr/share/licenses/quartz-screen/NOTICE
	$(INSTALL_DATA) ./src/internal/render/icondata/NOTICE $(1)/usr/share/licenses/quartz-screen/OPERATORS-NOTICE
endef

$(eval $(call BuildPackage,quartz-screen))

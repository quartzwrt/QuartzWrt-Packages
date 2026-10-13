# SPDX-License-Identifier: GPL-3.0-only
#
# luci-app-tailscale — LuCI for Tailscale（QuartzWrt 适配版；WP-10）
#
# 上游基线：asvow/luci-app-tailscale v1.2.6 (@534eb3f, GPL-3.0-only)
# 适配要点（详见 README.md / docs/DESIGN.md）：
#   - daemon 层依赖基座 tailscale 包（/usr/sbin/tailscaled + 其最简 init/config），
#     本包**不携带** /etc/init.d/tailscale 与 /etc/config/tailscale（零文件重叠）；
#   - 功能层（偏好应用/防火墙 zone/子网路由/MagicDNS 胶水/enabled 语义）由
#     /usr/sbin/tailscale_helper 幂等承接；boot 钩子 /etc/init.d/tailscale-quartz。

include $(TOPDIR)/rules.mk

LUCI_TITLE:=LuCI for Tailscale (QuartzWrt)
LUCI_DEPENDS:=+tailscale
LUCI_PKGARCH:=all

PKG_VERSION:=1.3.0
PKG_RELEASE:=2
PKG_LICENSE:=GPL-3.0-only
PKG_MAINTAINER:=QuartzWrt

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

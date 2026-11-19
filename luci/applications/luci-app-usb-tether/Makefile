# SPDX-License-Identifier: Apache-2.0
#
# luci-app-usb-tether — 手机 USB 共享上网（QuartzWrt，WP-13）
#
# 场景：手机（安卓 RNDIS/CDC-NCM/CDC-Ether，iPhone ipheth）插到路由器 USB 口
# 并开启「USB 网络共享」后，在 LuCI 一键启用，路由器把 USB 网卡作为 DHCP WAN
# 接入，默认 metric=50 备援（主 WAN 断时自动走手机；调低 metric 可改为主用）。
#
# 架构（详见 README.md / docs/DESIGN.md）：
#   - 接口由 netifd 自动跟踪固定设备名（默认 usb0）：手机插入 → 网卡出现 →
#     usb_tether 接口自动 ifup；拔线自动 ifdown。无常驻服务（Mode A 原生集成）；
#   - network/firewall 胶水全部由 /usr/sbin/usb_tether_helper 幂等承接；
#   - 仅入软件源（用户 apk add 按需安装）：不动 quartz-defaults / Gate B 断言；
#   - 基座 kmod 只引用不收录（docs/TDD.md §3.1 同名遮蔽规则——rndis/cdc-ether/
#     cdc-ncm 已随 qmodem 链预装，ipheth 随本包依赖带入）。

include $(TOPDIR)/rules.mk

LUCI_TITLE:=USB Tethering (QuartzWrt)
LUCI_DEPENDS:=+luci-base +kmod-usb-net-rndis +kmod-usb-net-cdc-ether \
	+kmod-usb-net-cdc-ncm +kmod-usb-net-ipheth
LUCI_BASENAME:=usb-tether

PKG_VERSION:=1.0.0
PKG_RELEASE:=1
PKG_LICENSE:=Apache-2.0
PKG_MAINTAINER:=QuartzWrt

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

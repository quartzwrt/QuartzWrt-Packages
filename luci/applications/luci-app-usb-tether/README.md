# luci-app-usb-tether（WP-13）

手机 USB 共享上网：把手机插到路由器的 USB 口并在手机上开启「USB 网络共享」后，
在 LuCI 一键启用，路由器即把该 USB 网卡作为 DHCP WAN 接入，通过手机上网。

- 支持安卓（RNDIS / CDC-NCM / CDC-Ether）与 iPhone（ipheth）；
- 默认 **metric 50 备援**：主 WAN 在线时走原出口，主 WAN 断开自动切到手机；
  在「设置」页调低 metric 可让手机成为主出口；
- 接口由 netifd 自动跟踪设备名（默认 `usb0`）：手机插入自动 ifup，拔线自动
  ifdown，无需任何常驻服务；
- 与 qmodem 5G 模组共存：状态/设置页标注模组数据口（qmi_wwan/mbim），设备
  下拉可改选 `usb1` 等避免抢占 `usb0`。

## 安装

仅入软件源（不预装、不动 `quartz-defaults`）：

```sh
apk add luci-app-usb-tether
```

内核依赖（`kmod-usb-net-rndis/cdc-ether/cdc-ncm/ipheth`）随包依赖自动带入；
现有设备固件中 rndis/cdc-ether/cdc-ncm 已随 qmodem 链预装。

## 使用步骤

1. 手机用 USB 线连接路由器 USB 口；
2. 在手机上开启「USB 网络共享」（安卓设置 → 网络 → 热点与网络共享）；
3. LuCI → 网络 → USB 共享上网 → 设置 → 勾选「启用」→ 保存并应用；
4. 状态页应显示「已连接」与 IPv4 地址；默认出口待命说明见 metric。

## 文件结构

```
Makefile                     LUCI_TITLE/DEPENDS（基座 kmod 只引用不收录）
htdocs/.../view/usb-tether/  setting.js（表单）/ status.js（轮询状态）
root/etc/config/usb-tether   本包配置（enabled=0 安全默认）
root/etc/uci-defaults/       幂等补缺省值（升级保留用户配置时兜底）
root/etc/hotplug.d/iface/    usb_tether ifup/ifdown 日志
root/usr/sbin/usb_tether_helper   apply / disable / cleanup / detect
root/usr/share/luci/menu.d/  网络 → USB 共享上网（状态/设置两 tab）
root/usr/share/rpcd/acl.d/   读：uci + helper detect + ip；写：uci + helper
```

架构与幂等语义详见 `docs/DESIGN.md`。

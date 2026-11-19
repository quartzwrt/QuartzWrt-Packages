# DESIGN — luci-app-usb-tether（WP-13，v1.0）

## 1. 问题与目标

手机开启 USB 网络共享后，路由器侧需要：USB 网卡驱动（已随 qmodem 链预装
rndis/cdc-ether/cdc-ncm）＋ `network` 里 DHCP 接口 ＋ 防火墙 zone 归属 ＋
metric 控制主备。缺一个应用层入口让用户「一键可用」。本包补齐该入口，不做
其它（不做多拨/负载均衡，与 mwan3/qmodem-mwan 正交）。

## 2. 架构（Mode A 原生集成，无常驻服务）

```
用户勾选「启用」并保存
        │  setting.js: form 只写 uci usb-tether
        ▼
usb_tether_helper apply ── 幂等同步胶水：
        ├─ network.usb_tether（命名节；proto=dhcp, device, metric, auto=enabled）
        ├─ firewall <zone>.network += usb_tether（enabled=0 时移除）
        └─ commit → network/firewall reload
                │
                ▼  netifd 自动跟踪 device（默认 usb0）
手机插入 → usb0 出现 → usb_tether ifup（DHCP 从手机取地址/网关/DNS）
拔线     → usb0 消失 → usb_tether ifdown，路由自动回落其它 WAN（metric 保证）
```

- 接口对设备出现/消失的自动跟随是 netifd 原生行为，**本包不起任何 daemon**；
  helper 只在保存/停用/清理时被调用（flock 防并发，同 tailscale_helper 模式）。
- proto 固定 dhcp（v1 决策）：手机侧即 DHCP 服务器（安卓 RNDIS 常见
  192.168.42.0/24，iPhone ipheth 常见 172.20.10.0/28）；uci 中的 proto 选项
  不暴露 UI，留给后续版本做 static。

## 3. 备援语义（metric）

- 接口 `metric` 默认 50。proto dhcp 下 netifd 以该 metric 装默认路由；主 WAN
  metric 更低（通常 0）时手机仅备援，主 WAN 断开内核自动选手机。
- 调低手机 metric（低于主 WAN）即变主出口；两出口并存时拔线即回落，无需再点
  任何按钮。
- IPv6：dhcp 同时取 v6 时同样按 metric 装路由，无需额外处理。

## 4. 与 5G 模组（qmodem）共存

- qmodem 默认 qmi/mbim 模式占用 `wwanX`（非 `usb0`），互不冲突；
- 若用户把模组切到 ECM/RNDIS 模式，模组可能占用 `usb0`。detect 对
  qmi_wwan/cdc_mbim 标 `tetherable:false`，UI 显示「模组设备」；所有 USB
  网卡（含 `usb1`+）都出现在设置页下拉，改选即可避开；
- detect 的 `used_by` 字段标出已被某 uci 接口引用的网卡，UI 显示「占用: 节名」。

## 5. 安全与幂等

- `enabled` 默认 0：**首次开启前本包零触碰 network/firewall**；
- helper 全部写操作先比较后写（`uci_set_if`/`uci_add_list_if`），重复 apply
  无副作用；uci 命名节先显式建节再设值（坑位：对不存在的节 set 直接 Invalid
  argument，同 tailscale_helper §6 教训）；
- device/zone/metric 先字符集校验后落盘，任一非法整体不动（防半套配置）；
- disable 只置 enabled=0 再走同一同步路径（幂等、不动其它设置）；cleanup
  移除 network 节与全部 zone 成员，保留 uci 源配置（conffile）；
- rpcd ACL 最小面：读 = uci(usb-tether/network/firewall) + `helper detect` +
  两条只读 `ip` 命令；写 = uci(usb-tether) + `helper apply/disable/cleanup`。

## 6. 状态页数据面（无 ubus 依赖）

状态页只读轮询（5s），数据源全部为 `fs.exec` 只读命令＋uci：

| 数据 | 来源 |
| :--- | :--- |
| 手机在位/厂商/型号/驱动 | `usb_tether_helper detect`（sysfs，printf 拼 JSON，无 jq） |
| 接口地址/流量 | `/sbin/ip -s -j ad` 过滤 device |
| 当前默认出口 | `/sbin/ip -j route show default` 取最小 metric 与 device 比对 |

不使用 ubus `network.interface dump`（计划稿曾列，实现时简化：`ip route`
即内核事实，少一条 ACL 面；代价是不显示 netifd uptime，可接受）。

## 7. 交付形态（仅入软件源，2026-11-20 拍板）

- 不进 `quartz-defaults`、不动 Gate B 清单断言；用户 `apk add` 按需安装；
- 发布上线需另行一轮：`feeds.conf.extra` quartz 锁 SHA bump（40 位）→
  `publish-feed.sh` 上传 R2 → `check-feed-live.sh` 自检（见 WORKPLAN WP-13）。

## 8. 验证

- 单包编译：`make package/luci-app-usb-tether/compile`（产物 .apk）；
- VM e2e（x86）：`ip link add usb0 type dummy`＋dnsmasq 起 192.168.42.0/24
  DHCP 模拟手机侧，验证：启用→接口 ifup 取到地址→zone 成员→默认路由 metric
  50 备援→状态页各字段；禁用/重复 apply/卸载 cleanup 三路径幂等；
- 真机验收（安卓/iPhone）由维护者执行（见 README）。

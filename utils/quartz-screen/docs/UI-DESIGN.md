# quartz-screen UI 设计稿（方向 A/A+ · R2）

**版本** v0.3（2026-11-23，R2 已实现）
**状态** 用户确认「就先这样」；渲染器/字体/图标已落地，待重编镜像/真机验收
**依据** `luci-theme-quartz/docs/DESIGN.md`（token/硬规则）、真机面板校准（320×172，GC9307）、
本轮用户反馈与 1–2 秒可读的小屏调研
**设计资产** `dev/design/preview/index.html`（交互预览，默认 A+）；
`dev/design/A/*.png` 为首轮 A 静态帧，R2 确认后重新生成

## 0. 结论

保留方向 A **每页 3 个大数值格**，A+ 在此基础上加格内补充行与 9–11px 次要行。
R2 按反馈做了五处改动：

| 页面 | R2 改动 |
| :-- | :-- |
| 概览 | 格内补 **CPU 频率 / 内存用量 / 负载**；底行补 **在线数 + WAN 协议** |
| 网络 | **删除折线图**，换 **Top 3 流量设备**（bandix.top，含占比条/速率）；峰值移入次要行 |
| 蜂窝 | **删除折线图**，换 **模组 6 项详情**；运营商旁显示 **官方图标**（移动/联通/电信） |
| 系统 | 格内补 **启动时间 / 5+15 负载 / 根分区用量**；底行补 **CPU 架构/频率/内存/温度/进程** |
| 无线 | 补 **加密方式 / AP 模式 / radio / HT / 客户端列表 / WLAN 接口 / 隐藏 SSID** |

设计原则不变：主信号 28px 主导 → 次信息 9–11px 后退 → 底行标识/速率；无渐变/装饰
动画；1px 描边；彩色只给状态与主数值。

## 1. 画框与栅格

- 画布 **320×172**；顶栏 22px；内容从 y=28 起；无独立页脚。
- 顶栏：页面标题（左，12/600）、页点（中，4px）、时间（右，10 等宽）。
- 三格：3 列等宽，间距 6，高 **92**（概览/系统，28px 主数值）或 **58**
  （网络/蜂窝/无线，22px 主数值）。
- 短三格下方是 **40px 信息卡**（y=89）：网络是 Top 3 列表，蜂窝是 6 项详情。
- 底部信息行：两行时从 y=132 起；概览/系统从 y=124 起。实作把右对齐的
  ↓/↑ 速率放在**第二行**（IP 同行），避免 9px 第一行被截断。
- 无线：SSID 16px 在 y=90；40px 详情卡 y=113；接口/能力行 y=155。
- 圆角 10；格内标签 10/400，主数值 28/600，单位 10/400。

## 2. Token

沿用 Quartz 暗/浅 token（面板 alpha=1，小屏不透明）：

| Token | 暗色 | 浅色 |
| :-- | :-- | :-- |
| Bg / Panel / Well | `#1c1c1e` / `#2c2c2e` / `#1c1c1e` | `#ececf0` / `#ffffff` / `#f2f2f7` |
| Border | `#3a3a3c` | `#ebebeb` |
| Text / Text2 / Text3 | `#f2f2f2` / `#b3b3b3` / `#666666` | `#262626` / `#737373` / `#b3b3b3` |
| Accent / OK / Warn / Err | `#0a84ff` / `#30d158` / `#ff9f0a` / `#ff453a` | `#007aff` / `#34c759` / `#ff9500` / `#ff3b30` |

## 3. 字体与图标

| 用途 | 字号/字重 | 说明 |
| :-- | :-- | :-- |
| 顶部标题 | 12 / 600 | 现有 `uib13` |
| 时间 | 10 等宽 | `mono13` 缩放或新增 10px |
| 格标签 | 10 / 400 | `ui11` |
| 格主数值（高卡） | **28 / 600** | **新增 `uib28`** |
| 格主数值（短卡） | **22 / 600** | `uib22` |
| 9px 数据行 | 9 / 400 | **新增 `ui9`** |
| 等宽小字 | 11 / 500 | **新增 `mono11`** |
| 单位/补充行 | 10 / 9 | `ui11` / 新增 `ui9`（或 `ui11` 缩排） |
| 信息卡正文 | 9–10 | `ui11`/`mono13` |
| 系统格值 | 12 | `mono13`/`ui13` |
| 时钟 | **64 / 700 等宽** | **新增 `clock64`** |
| 便签正文 | 12 | `ui13` |
| 运营商图标 | 12–14px | 预栅格化 4bpp；移动/联通/电信 |

新增 28px（UI 粗体）与 64px（时钟）两个 atlas；GB2312 1bpp 兜底不变。
R2 需要补进 4bpp 子集的字：频段、带宽、温度、启动、进程、就绪、隐藏、开启、关闭、
在线、已运行、根分区、内存、负载、客户端、在线设备、流量（GB2312 兜底已覆盖）。

运营商图标复用 Dashboard 官方符号
（`luci-app-dashboard/frontend/src/assets/operators/*.svg`），构建/开发期栅格化为
12×12 或 16×16 的 4bpp 位图嵌入守护进程；PLMN 映射与 QModemCard 一致：
46000/02/04/07/08→移动，46001/06/09→联通，46003/05/11→电信；未知运营商回退文字。

## 4. 页面

| 页面 | 三个大格 | 信息卡 / A+ 补充 | 可用条件 |
| :-- | :-- | :-- | :-- |
| 概览 | 处理器 / 内存 / 温度 | 格内：频率 / 已用·总量 / 负载；底：运行 + 在线 + WAN 协议 + IPv4 + 速率 | 总是 |
| 网络 | 下行 / 上行 / 客户端 | Top 3 设备流量（bandix）；底：IPv6 + 窗口峰值 + IPv4 + 速率 | 总是 |
| 蜂窝 | RSRP / SINR / 质量 | 6 项详情：频段/带宽/PCI/温度/ARFCN/SIM；运营商图标 + 制式 + IP + 流量 | `cellular.available` |
| 系统 | 运行 / 负载 / 根分区 | 格内：启动 / 5·15 负载 / 已用·总量；底：型号/固件 + 内核/架构/频率/内存/温度/进程 | 总是 |
| 无线 | 客户端 / 频段 / 信道 | SSID；加密/AP/radio/ch+HT；在线客户端列表；iface/HE/WMM/隐藏 | 存在 wifi-iface |
| 时钟 | —（64px 时间居中） | 日期 + 运行时长 | 总是 |
| 便签 | — | 标题 + ≤4 行 | 标题/任一行非空 |

页点只画实际可用页；页面顺序/开关由 LuCI 配置（沿用现有 uci schema）。

### 4.1 概览

- 主格：CPU 12%（bar）、内存 38%（bar）、温度 46°C。
- 格内补充行：`1.3 GHz` / `1.6 / 4.0 GB` / `负载 0.12`。
- 底行：`已运行 28 分钟 · 在线 23 · WAN DHCP`，下一行 IPv4 + `↓/↑ bps`。
- A 极简：只保留主格 + 单行 IPv4/速率。

### 4.2 网络

- 主格：下行 12.4 Mbps、上行 3.1 Mbps、客户端 23。
- 信息卡：Top 3 设备（名称 / 占比条 / 百分比 / 设备 WAN 速率），来源 `bandix.top`。
- 底行：`IPv6 240e:…:8c00::2 · 峰值 13.0 / 3.4`，下一行 IPv4 + 实时速率。
- 无 bandix（未装/未开 traffic/空列表）：信息卡回退为 **WAN 接口详情**
  （ifname / proto / IPv6），不画空条。
- 峰值是 `net.series` 环形窗口（≤60 点）内的采样峰值，不声称累计峰值。

### 4.3 蜂窝

- 主格：RSRP -82 dBm、SINR 21、质量 78%。
- 信息卡（3×2）：频段 n78 / 带宽 100MHz / PCI 123；温度 38°C / ARFCN 630000 / SIM 就绪。
- 底行：运营商图标 + 名称 + 制式（5G SA）；下一行 IP + `↓/↑ GB`。
- 图标映射见 §3；无匹配（如广电/虚拟运营商）只显示文字，不显示错图标。
- USB 速率不在当前聚合快照中；本轮用 ARFCN。若后续在 daemon 增加 USB 采集，
  可在该位置替换（不改变版式）。

### 4.4 系统

- 主格：运行 28 分钟、负载 0.12、根分区 23%。
- 格内补充行：`启动 17:22` / `0.08 / 0.05` / `2.0 / 8.5 GB`。
- 底行：`NLnet XiGuaPi V3 · QuartzWrt 0.2.4`；
  下一行 `Linux 6.12.112 · 4×A55 1.3GHz · 内存 38% · 46°C · 211 进程`。

### 4.5 无线

- 主格：客户端 2、频段 5G、信道 36（HE80）。
- SSID 16px；详情卡：`WPA3-SAE · AP · radio0 · ch36 HE80`；
  `在线 2 · MacBook-Pro 192.168.1.100 · iPhone 192.168.1.101`。
- 底行：`wlan0 · HE80 · WMM 开启`；右侧 `隐藏 SSID 否`。
- 客户端数/列表优先取 `clients.items` 中 `iface == wifi-iface` 的条目；
  取不到时回退 `clients` 总数并标注“在线”，不伪装成无线关联数。
- `wmm` 未在 uci 显式设置时按 OpenWrt 默认显示“开启”；hidden 缺省为“否”。

### 4.6 时钟/便签

不变：64px 等宽时间 + 日期/运行时长；便签标题 + 最多 4 行。

## 5. 数据来源与降级

| 页面 | 字段 |
| :-- | :-- |
| 概览 | `system.cpu_pct`、`system.mem.{used,total}`、`system.temp_c`、`system.cpu_mhz`、`system.load[0]`、`host.uptime`、`clients.total`、`net.wan.{proto,ipv4,rx_bps,tx_bps}` |
| 网络 | `net.wan.{ipv4,ipv6,rx_bps,tx_bps}`、`net.series` 峰值、`clients.total`、`bandix.top[]`（host/ip/pct/rx_r+tx_r） |
| 蜂窝 | `cellular.modems[hero].{operator,network_mode,cell_info.band,cell_info.dl_bandwidth,cell_info.pci,cell_info.arfcn,temp_c,sim.status,dial.ipv4,traffic}` |
| 系统 | `host.{model,release,kernel,uptime}`、`system.{load,rootfs,cpu_model,cores,cpu_mhz,mem,temp_c,procs}` |
| 无线 | 本地 uci wireless（ssid/encryption/mode/device/network/hidden；radio band/channel/htmode/wmm）+ `clients.items[iface]` |

- 数据源离线/过期：概览页状态点变 warn；数值保留上次；蜂窝/无线不可用整页跳过。
- 无蜂窝模组：蜂窝页不出现（不画空卡）。
- 夜间：只调背光，不改版式。
- 无数据占位 `—`；条 0；不回退伪造数据。
- 快照子集需补 `system.cpu_model/cores/cpu_mhz/procs` 与 `bandix` 节；
  `Wireless` 需补 `Encryption/Mode/Network/Hidden/WMM`。

## 5.1 小屏设计依据（2026-11-23 调研）

- **固定观看距离**：嵌入式屏距离基本固定，字号要比手机场景更大；按实际距离实测
  （ICS《UX for Small Embedded IOT and Medical Device Screens》）。
- **1–2 秒读懂**：主信息应在 1–2 秒内识别；主信号用大小/对比/颜色主导，次要数据
  主动降低亮度/饱和度后退（Glanceable Interfaces）。
- **每屏 1–2 个任务**、减少装饰与动画、高对比、逻辑分组、状态/告警优先
  （Google Wear OS Design Principles；embedded GUI 实践）。
- **模式识别优先于阅读**：大而高对比的数字 > 长文本；颜色只用于状态/分类。

映射：3 个 28px 主数值 → 9–11px 次要行/信息卡 → 底行标识；每页不超过约 10–12 个值；
不做渐变/装饰动画；1× 实尺寸检查可读性。R2 的 9px 数据行统一用 Text2，保证对比度。

## 6. 设计稿资产

```
dev/design/preview/index.html   # R2 交互预览（默认 A+；页面/暗浅/中英/1x/2x/3x）
dev/design/preview/operators/   # 运营商官方 SVG（移动/联通/电信）
dev/design/A/*.png              # 首轮 A 静态帧（R2 确认后重生成）
dev/design/A/design-A.html      # 首轮 3x 对照页
```

静态 A 帧尚未包含 R2 改动；评审以交互预览为准。

## 7. 实现状态与偏差（2026-11-23）

已落地：
1. `pages` 渲染器按 A+ 重排：高/短卡 + 40px 信息卡 + 底部 1–2 行；页点移到顶栏。
2. **删除网络/蜂窝折线图**；`show_sparkline` 旧配置忽略，LuCI 选项移除；
   网络改 Top 3（bandix），蜂窝改 6 项详情。
3. 快照子集补 `system.cpu_model/cores/cpu_mhz/procs`、`bandix.top`；
   `Wireless` 补 encryption/mode/network/hidden/wmm。
4. 运营商图标：16×16 RGB888+4bpp 位图（Dashboard 官方符号栅格化）+ PLMN/名称映射；
   未知回退文字；许可声明 `icondata/NOTICE`。
5. 字体新增 `ui9`/`uib28`/`mono11`/`clock64`；补 R2 中文子集；GB2312 兜底不变。
6. LuCI 预览/设置同步；预览直接呈现新渲染器；移除趋势图选项。
7. 显示 bring-up 修复随镜像固化：
   - 驱动 `ROW_OFFSET 34`（172 行居中于 240 source）；
   - `MADCTL = MY|MX|MV|BGR`（0xE8，真机校准确认）；
   - `fbcon=map:1`（禁止 fbcon 抢小屏）+ init 解绑；
   - 测试图案边框顺序修复（先画边框再画色条）。
### 7.1 已实现偏差（需知悉）

- 实机只出 A+，不提供运行时 A/A+ 切换；A 仅作预览对比。
- 概览/网络/系统的速率放在第二行右侧（设计稿第一行），避免实际 WAN 速率变长时截断首行。
- 无线首格用 **在线设备**（`clients.total`），不是严格 WiFi 关联数：快照的
  `clients.items[].iface` 在桥接场景多为 br-lan，无法可靠区分有线/无线；
  文档与 UI 均标「在线」。客户端列表取前 2 条。
- 蜂窝 USB 速率不在快照中，详情用 **ARFCN**；温度越界（< -40/>150）显示 `—`；
  运营商/SIM ISP 为 `????` 时回退，并可在 LuCI「运营商图标」手动覆盖。
- 运营商图标 16px（设计稿 12–14），为小屏辨识度略放大。
- 浅色/英文全页随渲染器自动可用；设计稿只展示了概览。

## 8. 后续（非阻塞）

- 若需要严格 WiFi 关联数：在 quartz-daemon `clients` 增加 `wifi` 计数（`collectWireless` 已有数据）。
- 若需要 USB 速率：在 daemon 增加 USB sysfs 采集后替换 ARFCN。
- 真机 1× 可读性复核后微调 9px 行/图标尺寸。

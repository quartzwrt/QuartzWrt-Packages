# quartz-screen 设计规范

**版本** v1.1（2026-11-23，A+ R2 实现）
**状态** 实现完成，待重编镜像/真机验收
**依据** `QuartzWrt-Builder/docs/TDD.md` §4.6（设备适配层）、`luci-theme-quartz/docs/DESIGN.md`（视觉 token）、`luci-app-dashboard/docs/AGGREGATE-API.md`（数据契约）
**设备** NLnet XiGuaPi V3（NL68K，RK3568）

## 0. 定位与范围

把板载 GC9307 SPI 小屏点亮，并用一个与 Quartz 视觉语言一致的多页轮播 UI
显示路由器状态；提供 LuCI 控制面（亮度、夜间、主题、页面增减/排序、便签、
实时预览）。

- **做**：fbtft 驱动、320×172 横屏渲染、状态页轮播、亮度/夜间调光、LuCI
  状态/设置/预览、设备层预装。
- **不做**：触摸/按键交互（本板无触摸，ADC 键仅复位）、MIPI-DSI、HDMI 界面、
  开机 logo、自动感光、App 端屏幕控制（运行态 JSON 已预留）。

## 1. 硬件与驱动

### 1.1 面板

| 项 | 值 | 来源 |
| :-- | :-- | :-- |
| 模组 | GoldenMorning T147BG-C08-06 / T147BG-H12-07 | 厂商资料 PDF |
| 尺寸 | 1.47 英寸，172(H)RGB × 320(V) | 同上 |
| 有效区 | 17.65 × 32.83 mm | 同上 |
| 控制器 | GalaxyCore GC9307C | 同上 |
| 接口 | 4-line SPI（8-bit bus + D/C），无 MISO | 同上 + 基座 DTS |
| 背光 | 3 颗白光 LED，PWM14（1 kHz） | 同上 + 基座 DTS |
| 安装方向 | 竖屏模组横装 → 320×172 | 厂商 DTS `rotate=270` |

**重要修正**：设备文档此前写「320×240」是错的；实际是 **172×320**，横屏
**320×172**（见 `dev/mockups/`）。

### 1.2 内核驱动（fbtft）

设备层自带 `fb_gc9307`，compatible `galaxycore,gc9307`：

- `display`: width=172, height=320, bpp=16, buswidth=8, fps=10；
- `set_var`: `rotate=270` → `MADCTL = MY|MX|MV|BGR`（0xE8；真机校准）；面板为 BGR 滤色片；
- `set_addr_win`: 页地址从 **source 34** 开始（172 行居中于 240 source；`ROW_OFFSET 34`）；
- `init_display`: 厂商初始化序列（`0xFE/0xEF`、0x85–0x8F、0xE8/EC/ED、
  gamma 0xF0–0xF3、0x11 + 200ms、0x29 等），见
  `devices/nlnet_xiguapi-v3/files/target/linux/rockchip/files/drivers/staging/fbtft/fb_gc9307.c`；
- `set_addr_win`: 标准 0x2A/0x2B/0x2C。

配套：

- `devices/nlnet_xiguapi-v3/files/target/linux/rockchip/patches-6.12/999-fbtft-gc9307.patch`
  给 fbtft 加 `CONFIG_FB_TFT_GC9307` / Makefile 条目；
- `devices/nlnet_xiguapi-v3/files/package/kernel/linux/modules/zz-video-gc9307.mk`
  定义 `kmod-fb-tft-gc9307`（依赖 `kmod-fb-tft`，AutoLoad 09）；
- DTS 补丁：`spi-max-frequency=24MHz`、`width/height=172/320`、`fps=10`，
  删除 `backlight` phandle（fbtft 不消费），背光 LED 改 `default-state="off"`；
- bootargs 去掉 `console=tty1` 并加 `fbcon=map:1`，init 启动前再解绑 fbcon：避免控制台日期/日志画到小屏。

### 1.3 与厂商方案的对照（2026-11-23 资料核实）

厂商 `xgp-v3-screen_1.2-3_aarch64_generic.ipk`（GPL-3.0-only）：

- 依赖 `kmod-fb-tft-gc9307`（厂商 out-of-tree fbtft 驱动，不在 ipk 内）、
  `python3`、`libstdcpp6`；
- 用户态 `/usr/zz/zz_xgp_screen` 是 3.2MB LVGL fbdev 应用，固定 5 页
  （关于/系统/网络/模组/信号），无 UCI 配置、无 LuCI、无亮度控制；
- `modem_info.py` 通过 `/usr/libexec/rpcd/modem_ctrl call info` 取模组信息。

Quartz 方案保留「fbtft 驱动」这一已验证的核心，但：

- 驱动自研并进入设备层（不依赖厂商二进制；6.6→6.12 vermagic 不兼容）；
- 用户态改为 Go stdlib-only 守护 + 自有渲染器，零 Python/LVGL/libstdc++；
- 数据源改为 `quartz-daemon` 快照（与 Dashboard/App 同源），不直连模组；
- 增加 LuCI 控制面与 Quartz 视觉。

## 2. 架构

```
quartz-daemon ──/var/run/quartz/status.json──┐
                                             ▼
LuCI ──fs.exec──> /usr/sbin/quartz-screen ──> 渲染器（320×172 RGB565）
  │                │  run/status/preview         │
  │                │                              ▼
  └──uci──────────> /etc/config/quartz-screen   /dev/fb0（fbtft）
                     │                              │
                     └── procd init ◄───────────────┘
                          │
                    /sys/class/leds/blue:backlight/brightness
```

- **单进程**：procd 拉起 `quartz-screen run`，无额外守护；预览/状态走同一
  二进制的子命令（LuCI `fs.exec`）。
- **数据只读**：快照每秒检查 mtime，变化才重绘；写屏前做画布哈希，内容不变
  不写 SPI。
- **降级**：快照缺失 → 页面显示「数据源离线」并保留上次数据；fb 打开失败 →
  每 5s 重试，状态文件记录错误；背光缺失 → 只记一次日志。
- **运行态**：`/var/run/quartz-screen.json`（PID、fb、页面、亮度、错误），
  供 LuCI/未来 App 读取；`QUARTZ_SCREEN_RUN_DIR` 可重定向（VM e2e）。

## 3. 视觉设计（320×172）

### 3.1 Token → RGB565

直接投影 `luci-theme-quartz` 的 `--qz-*` token（面板 alpha=1，小屏不透明）：

| Token | 暗色 | 浅色 | 用途 |
| :-- | :-- | :-- | :-- |
| Bg | `#1c1c1e` | `#ececf0` | 窗口底色 |
| Panel | `#2c2c2e` | `#ffffff` | 卡片 |
| Well | `#1c1c1e` | `#f2f2f7` | 进度条槽 |
| Border | `#3a3a3c` | `#ebebeb` | 1px 描边 |
| Text / Text2 / Text3 | `#f2f2f2` / `#b3b3b3` / `#666666` | `#262626` / `#737373` / `#b3b3b3` | 正文/次要/弱化 |
| Accent | `#0a84ff` | `#007aff` | 强调/主数值 |
| OK / Warn / Err | `#30d158` / `#ff9f0a` / `#ff453a` | `#34c759` / `#ff9500` / `#ff3b30` | 状态 |

硬规则沿用主题：无渐变/发光/装饰动画；1px 描边；圆角 ≤12（小屏卡片 10）；
彩色只用于文字/状态/数值；页切换瞬时完成。

### 3.2 栅格与字号（A+ R2）

- 画布 320×172；外边距 6px；顶栏 22px（标题/页点/时间）；无页脚。
- 内容从 y=28 起：高卡 92（概览/系统）、短卡 58（网络/蜂窝/无线）；
  短卡下 40px 信息卡；底部 1–2 行（y=124 或 132）。
- 字体：Noto Sans CJK 子集（4bpp AA）+ GB2312 1bpp 兜底（动态 SSID/便签）。
  `ui9`（9px 数据行）/`ui11`/`ui13`/`uib13`/`uib16`/`uib22`/`uib28`（28px 主数值）/
  `mono11`/`clock64`（64px 时钟）。
- 卡片：radius 10，panel + 1px border；进度条 3px；**不再绘制 sparkline**。
- 数据型 9px 行用 Text2；Text3 只用于页点等装饰。

### 3.3 页面库

| ID | 标题 | 默认 | 可用条件 | 内容 |
| :-- | :-- | :-- | :-- | :-- |
| overview | 概览 | ✅ | 总是 | CPU/内存/温度三格（格内频率/用量/负载）；运行+在线+WAN 协议；IPv4+速率 |
| network | 网络 | ✅ | 总是 | 下行/上行/客户端三格；bandix Top 3 设备（无 bandix 回退 WAN 详情）；IPv6+窗口峰值+速率 |
| wireless | 无线 | — | 存在 wifi-iface | 在线设备/频段/信道；SSID；加密/AP/radio/HT；客户端列表；WMM/隐藏 |
| cellular | 蜂窝 | ✅ | `cellular.available` | RSRP/SINR/质量；频段/带宽/PCI/温度/ARFCN/SIM；运营商官方图标；IP/流量 |
| system | 系统 | ✅ | 总是 | 运行/负载/根分区（格内启动/5+15/用量）；型号/固件；内核/CPU/内存/温度/进程 |
| clock | 时钟 | — | 总是 | 64px 时间 + 日期/星期 + 运行时长 |
| note | 便签 | — | 标题或任一行非空 | 标题 + ≤4 行自定义文本 |

运行时按配置顺序过滤不可用页；页点只画实际页数。

### 3.4 状态与降级

- 顶栏：页面标题（13 bold）+ 页点（居中 4px）+ 时间（mono11；时钟页改日期）。
  概览页若设置 `main.title`，标题用自定义值，否则「概览」。
- 无页脚；底部两行留给数据。
- 快照过期 >30s：顶栏 warn 点 + 概览「数据源离线」，数值保留上次。
- 蜂窝/无线不可用：整页跳过，不显示空卡。
- 字号与对比度：数据型 9px 行用 Text2；Text3 仅装饰/页点。
- 运营商图标 16×16（RGB888+4bpp alpha，Dashboard 官方符号栅格化）；
  未知运营商只显示文字。

### 3.5 数据源

- 快照子集新增：`system.cpu_model/cores/cpu_mhz/procs`、`bandix.top[]`
  （host/ip/pct/rx_r/tx_r）、`clients` 总数与条目；`net.wan`、`net.series`。
- 无线静态信息新增：`encryption/mode/network/hidden/wmm`（uci wireless）。
- 网络 Top 3 来自 `bandix.top`；未装/未开 traffic/空列表时回退 WAN
  ifname/proto/IPv6，不画空条。峰值是 `net.series`（≤60 点）窗口内采样峰值。
- 无线「在线设备」用 `clients.total`（快照不能可靠区分有线/无线），
  客户端列表取前 2 条；标注为「在线」而非「无线关联」。
- 蜂窝 6 项详情用 `cell_info` + `temp_c` + `sim.status`；USB 速率当前不在
  快照中，用 ARFCN 替代。

设计稿：`dev/mockups/{dark,light}-*.png`（真实渲染器输出，2x 最近邻）+
`dev/mockups/preview.html` 对照页；A/A+ 交互稿 `dev/design/preview/index.html`，
规格 `docs/UI-DESIGN.md`。

## 4. 配置（`/etc/config/quartz-screen`）

```uci
config screen 'main'
    option enabled '1'        # 开关
    option brightness '80'    # 日间亮度 1-100
    option theme 'dark'       # dark|light
    option lang 'auto'        # auto|zh|en（auto 跟随 luci.main.lang）
    option rotate '0'         # 0|180（内容翻转，硬件方向由 DTS rotate 固定）
    option interval '8'       # 轮播秒数；0=停在首页
    list pages 'overview'
    list pages 'network'
    list pages 'cellular'
    list pages 'system'
    option title ''           # 概览页顶栏自定义标题（空=「概览」）
    option operator 'auto'    # auto|mobile|unicom|telecom|broadnet（qmodem 报 ???? 时手动覆盖）
    option temp_unit 'c'      # c|f
    option clock_24h '1'
    option show_ipv6 '1'
    option show_peaks '1'
    option show_clients '1'
    option fb 'auto'          # auto 或 /dev/fbN
    option backlight 'auto'   # auto 或 /sys/class/leds/<name>
    option status_file '/var/run/quartz/status.json'

config night 'night'
    option enabled '1'
    option start '23:00'
    option end '07:00'
    option brightness '10'
    option off '0'            # 1=夜间熄屏

config note 'note'
    option title ''
    option line1 ''
    option line2 ''
    option line3 ''
    option line4 ''
```

解析：守护进程启动时 `uci -q show quartz-screen`；SIGHUP 重载。值域在守护侧
收敛（亮度 1-100、夜间 0-100、interval 0-300、页面去重/白名单）。

## 5. CLI

| 命令 | 说明 |
| :-- | :-- |
| `run` | 守护循环（procd）；开发参数：`--fb-file`、`--status-file`、`--demo`、`--once` |
| `status` | 输出运行态 JSON（LuCI 状态页/Poll） |
| `preview --page ID --theme dark\|light --format png\|base64 --scale N` | 离线渲染单页，与实屏同一套代码 |
| `render --all --out DIR --scale 2 --demo` | 生成设计稿 PNG + `preview.html` |
| `test` | 向运行中的守护发 SIGUSR1，显示 8 秒测试图案（色条/灰阶/方向/屏参） |
| `apply` | `/etc/init.d/quartz-screen restart`（LuCI 保存后调用） |
| `version` | 版本 |

## 6. 打包与集成

| 包 | 内容 |
| :-- | :-- |
| `quartz-screen` | Go 守护 + 渲染器 + 内置字体；`/etc/init.d/quartz-screen`、`/etc/config/quartz-screen`、uci-defaults |
| `kmod-fb-tft-gc9307`（设备层） | fbtft 驱动 kmod（`/lib/modules/*/fb_gc9307.ko`） |
| `luci-app-quartz-screen` | 状态/设置/预览 + menu/ACL/i18n |

- init：`START=95`，`has_screen`（/dev/fb* 或背光）不满足则不启动；`reload`
  发 SIGHUP；`stop_service` 关背光。
- 设备层预装：`kmod-fb-tft-gc9307`、`quartz-screen`、
  `luci-app-quartz-screen`；`device.env` 断言同名包。
- 其他机型：包进 quartz feed 可选安装；无屏机型 init 自动跳过。

## 7. 验收

1. `go test ./...`：config/字体/页面/渲染/预览。
2. `render --all`：暗/浅 7 页 PNG + HTML 目检（mockups 已入库）。
3. VM e2e：`run --fb-file` + `--demo` 假帧缓冲；LuCI Playwright 状态/设置/
   预览/保存；截图入 `dev/shots/`。
4. 真机：dmesg 面板 probe、`/dev/fb0` 320×172、背光可调、测试图案方向/颜色、
   四页轮播、夜间调光、LuCI 预览与实屏一致。
5. 镜像：Gate A/B、manifest 断言、签名。

## 8. 风险与回退

| 风险 | 处理 |
| :-- | :-- |
| MADCTL 方向/镜像不对 | `set_var` 切 0x60/0xA0/0xC0/0xE0 或 DTS `rotate` 90/180/270；测试图案有方向/中心标记 |
| BGR/RGB 颜色反 | 面板为 BGR，驱动固定 BGR=1；真机确认 0xE8 后红蓝/黄青正常 |
| SPI 24MHz 不稳 | 降到 12MHz 或 8MHz；先保证静态画面正确 |
| fbcon 抢占小屏 | bootargs `fbcon=map:1` + init 解绑（已固化）；否则控制台日期/日志会盖在帧上 |
| 面板初始化不兼容 | 回退厂商序列逐条核对；驱动为设备层补丁，可独立替换 |
| 字体缺字 | 静态标签为固定子集；动态文本走 GB2312 1bpp；缺字画占位块 |
| 无屏设备误启动 | init `has_screen` 门控；daemon fb 打开失败只记日志 |

## 9. 决策记录

- **D1 驱动**：fbtft（厂商同路线），设备层自研 `fb_gc9307`；不用
  panel-mipi-dbi（用户拍板 2026-11-23）。
- **D2 用户态**：Go stdlib-only，替代厂商 LVGL/Python；零第三方依赖。
- **D3 数据**：只读 `quartz-daemon` 快照，不直连 qmodem/ubus。
- **D4 视觉**：直接投影 Quartz token，画布 320×172；卡片 radius 10。
- **D5 交付**：NL68K 默认预装；包同时进 quartz feed。

## 10. 未决 / 后续

- 真机校准 MADCTL/颜色/SPI 上限后回填本文与驱动注释。
- 亮度是否加入“白天/夜间自动切换”的更多曲线（当前仅夜间窗口）。
- App 端屏幕控制（读取 `/var/run/quartz-screen.json` + `apply`）。

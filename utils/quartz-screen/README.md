# quartz-screen

QuartzWrt 小屏渲染守护进程（WP-16）。为 NLnet XiGuaPi V3 板载 1.47" GC9307
SPI 屏（172×320，横屏 320×172）提供多页轮播状态显示与亮度/夜间控制。

## 组成

| 路径 | 内容 |
| :-- | :-- |
| `src/cmd/quartz-screen` | CLI：`run/status/preview/render/test/apply/version` |
| `src/internal/render` | 画布/位图字体/主题/组件/运营商图标（RGB565 + PNG） |
| `src/internal/pages` | 7 个内置页面与 A+ 排版（3 格 + 信息卡 + 底行） |
| `src/internal/config` | `uci -q show quartz-screen` 解析 |
| `src/internal/snapshot` | 读取 `/var/run/quartz/status.json` |
| `src/internal/fb` | framebuffer 发现/映射/写帧（含假 fb 文件） |
| `src/internal/daemon` | 1s 渲染循环、轮播、亮度、夜间、测试图案 |
| `files/` | procd init、默认 uci、uci-defaults |
| `docs/DESIGN.md` | 设计规范（硬件/驱动/页面/配置/验收） |
| `dev/fontgen/` | 字体/运营商图标 atlas 生成器（开发机 Playwright，非构建依赖） |
| `dev/mockups/` | 真实渲染器输出的暗/浅 7 页设计稿 |

## 构建

```sh
# 在锁定基座树内（feed 已挂载）
make package/feeds/quartz/quartz-screen/compile V=s
```

Go 源码 stdlib-only；字体 atlas 已提交，构建期不联网、不需要 Python/Chromium。

## 开发

```sh
cd src
go test ./...
go build -o /tmp/quartz-screen ./cmd/quartz-screen
/tmp/quartz-screen preview --demo --page overview --theme dark --out /tmp/ov.png --scale 2
/tmp/quartz-screen render --all --out dev/mockups --scale 2 --demo
```

假 framebuffer（VM e2e）：

```sh
QUARTZ_SCREEN_RUN_DIR=/tmp/qs-run /tmp/quartz-screen run --demo \
  --fb-file /tmp/qs-run/fb.raw --status-file /nonexistent
```

## 设备依赖

- 驱动：设备层 fbtft `fb_gc9307`（`kmod-fb-tft-gc9307`），暴露
  `/dev/fb0`，320×172 RGB565；
- 背光：`/sys/class/leds/blue:backlight`（PWM14，0-255）；
- 数据：`quartz-daemon` 的 `/var/run/quartz/status.json`（含 `bandix.top`）；
  无线静态信息读 `/etc/config/wireless`（SSID/加密/AP/radio/HT/WMM）。
- 运营商图标：Dashboard 官方符号栅格化的 16×16 位图（移动/联通/电信）；
  商标声明见 `src/internal/render/icondata/NOTICE`。

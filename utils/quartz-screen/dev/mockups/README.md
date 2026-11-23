# 屏幕设计稿（320×172）

本目录的 PNG 由 **真实渲染器**（`quartz-screen render --all --demo`）生成，
不是手工绘制的示意图；`preview.html` 为暗/浅双主题对照页。图片为 2x 最近邻
放大，实际屏为 320×172。

| 文件 | 页面 |
| :-- | :-- |
| `dark-overview.png` / `light-overview.png` | 概览 |
| `dark-network.png` / `light-network.png` | 网络 |
| `dark-wireless.png` / `light-wireless.png` | 无线 |
| `dark-cellular.png` / `light-cellular.png` | 蜂窝 |
| `dark-system.png` / `light-system.png` | 系统 |
| `dark-clock.png` / `light-clock.png` | 时钟 |
| `dark-note.png` / `light-note.png` | 便签 |

重新生成：

```sh
cd src && go build -o /tmp/quartz-screen ./cmd/quartz-screen
/tmp/quartz-screen render --all --out ../dev/mockups --scale 2 --demo
```

设计规范见 `docs/DESIGN.md` §3；LuCI 控制面见
`luci/applications/luci-app-quartz-screen/docs/DESIGN.md`。

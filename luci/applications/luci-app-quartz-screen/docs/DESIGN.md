# luci-app-quartz-screen 设计规范

**版本** v1.0（2026-11-23）
**状态** 设计定稿，实现完成待 VM/真机验收
**依赖** `quartz-screen`（守护 + 渲染器）、`luci-theme-quartz`（token）
**设备** NLnet XiGuaPi V3（1.47" GC9307，320×172）

## 0. 定位

`quartz-screen` 的控制面：查看运行状态、看真实渲染预览、改配置。不做第二套
渲染/预览逻辑——预览直接调用守护进程的同一套离线渲染器，保证「LuCI 所见 =
实屏所见」。

## 1. 信息架构

菜单：`系统 → 屏幕显示`（`admin/system/quartz-screen`，order 65），两个视图：

| 视图 | 路径 | 内容 |
| :-- | :-- | :-- |
| 状态 | `quartz-screen/status` | 服务/屏幕/页面/亮度/夜间/数据源 + 实时预览 + 测试图案/重启 |
| 设置 | `quartz-screen/settings` | 显示/轮播/页面内容/夜间/便签 |

`admin/system/quartz-screen` 用 `firstchild`，默认进入状态页；`depends` 同时
要求 ACL 与 `uci quartz-screen` 存在。

## 2. 状态页

### 2.1 状态数据

`fs.exec('/usr/sbin/quartz-screen', ['status'])` → JSON（见
`quartz-screen/docs/DESIGN.md` §5）。轮询 5s（`poll.add`）。

展示字段：

| 字段 | 说明 |
| :-- | :-- |
| 服务 | Running / Stopped（绿/红点） |
| 屏幕设备 | `/dev/fb0 · fb_gc9307` |
| 分辨率 | `320×172 @16bpp` |
| 当前页面 | 页面中文名 |
| 亮度 | `80%` |
| 夜间模式 | 是/否 |
| 最近渲染 | 相对时间（秒/分/时 + 前） |
| 数据源 | OK / 数据过期 / 不可用 |
| 错误 | `errors[]` 逐行，弱化等宽块 |

### 2.2 实时预览

- 对当前实际轮播页（`status.pages`）逐页调用
  `quartz-screen preview --page <id> --theme <dark|light> --format base64 --scale 2`；
- 返回 base64 PNG → `data:image/png;base64,…`，CSS `image-rendering: pixelated`；
- 主题下拉切换暗/浅；「刷新」重新渲染；
- 预览卡使用 `--qz-bg-well` 底 + 1px `--qz-border` + `--qz-radius-sm`，
  320×172 外框；图片按容器宽度自适应（默认 2x，细节更清楚）。
- 预览不依赖守护进程运行（离线渲染），仅依赖二进制与快照（缺失时画降级态）。

### 2.3 操作

| 按钮 | 命令 | 行为 |
| :-- | :-- | :-- |
| 测试图案 | `quartz-screen test` | 实屏显示 8s 色条/灰阶/方向/参数，用于校准 |
| 重启服务 | `quartz-screen apply` | 重启 procd 实例并刷新状态 |

只读页：`handleSave/handleSaveApply/handleReset = null`，不显示底部保存条。

## 3. 设置页

使用 `form.Map('quartz-screen')` + `main` / `night` / `note` 三个 `NamedSection`；
`main` 内按顺序排列显示/轮播/内容字段（同一 section 不重复建 NamedSection，
避免 DOM id 冲突）。标准 cbi 卡片由主题样式承接，不引入自定义 CSS。

| 分组 | 字段（uci） | 控件 | 校验/默认 |
| :-- | :-- | :-- | :-- |
| 显示 | `main.enabled` | Flag | 默认 1 |
| 显示 | `main.brightness` | Value | range(1,100)，默认 80 |
| 显示 | `main.theme` | ListValue | dark/light，默认 dark |
| 显示 | `main.lang` | ListValue | auto/zh/en |
| 显示 | `main.rotate` | ListValue | 0/180 |
| 轮播 | `main.interval` | ListValue | 0/3/5/8/10/15/30，默认 8 |
| 轮播 | `main.pages` | 页面 Flag ×7 | 勾选启用；保存时按当前顺序合成 pages 列表；默认 overview/network/cellular/system |
| 内容 | `main.operator` | ListValue | auto/mobile/unicom/telecom/broadnet；qmodem 报 ???? 时手动指定图标 |
| 内容 | `main.title` | Value | ≤32，空=「概览」；非空时覆盖概览页顶栏标题 |
| 内容 | `main.operator` | ListValue | auto/mobile/unicom/telecom/broadnet；qmodem 报 ???? 时手动指定图标 |
| 内容 | `main.temp_unit` | ListValue | c/f |
| 内容 | `main.clock_24h` | Flag | 默认 1 |
| 内容 | `main.show_ipv6` / `show_peaks` / `show_clients` | Flag | 默认 1；R2 起无趋势图（`show_sparkline` 旧字段忽略） |
| 夜间 | `night.enabled` | Flag | 默认 1 |
| 夜间 | `night.start` / `night.end` | Value | `HH:MM` 正则校验，默认 23:00/07:00 |
| 夜间 | `night.brightness` | Value | range(0,100)，默认 10 |
| 夜间 | `night.off` | Flag | 夜间熄屏 |
| 便签 | `note.title` / `line1..line4` | Value | ≤32 / ≤48 |

页面库（与守护端 `KnownPages` 一致）：overview / network / wireless /
cellular / system / clock / note。不可用页（无 WiFi、无模组、便签为空）由守护
端运行时跳过；LuCI 不隐藏选项，保存后以状态页实际页为准。

ACL：读 `uci quartz-screen` + `luci`、ubus `uci get/changes`；写 `uci quartz-screen`、
ubus `uci set/changes/apply`；exec `quartz-screen status|preview *|apply|test`。

## 4. 保存链路（基座 25.12 实测语义）

基座 `ubus uci set` **写入 changes 缓冲，必须 `uci.apply` 才落盘**（不是直接落盘；
WP-16 实测：只调 `quartz-screen apply` 会重新读到旧配置）。因此：

1. `handleSave` 内联复刻 `View` 基类的保存：遍历 `.cbi-map` 调
   `dom.callClassMethod(map, 'save')` 保存 Flag；再按 `page_*` Flag 合成
   `main.pages`（沿用旧顺序，新页追加），`uci.save()` 后显式 `uci.apply()`；
2. 保存完成后调用 `fs.exec('/usr/sbin/quartz-screen', ['apply'])`
   （init restart）；
3. `handleSaveApply` 直接复用 `handleSave`；
4. 失败时 `ui.addNotification` 显示 stderr。

（WP-13 坑位：`form.Map.prototype` 上没有 handleSave/handleSaveApply，照搬
`.call()` 会崩成按钮永久转圈。）

## 5. ACL

`root/usr/share/rpcd/acl.d/luci-app-quartz-screen.json`：

- read：`uci quartz-screen`；exec
  `/usr/sbin/quartz-screen status`、`/usr/sbin/quartz-screen preview *`；
  读 `/var/run/quartz-screen.json`；
- write：`uci quartz-screen`；exec `apply`、`test`。

rpcd 的 file exec 用 `fnmatch` 匹配命令串，`preview *` 允许携带
`--page/--theme/--format/--scale` 参数。

## 6. 视觉与 i18n

- 状态页自定义 CSS 全部走 `--qz-*`（带 fallback，兼容 Vantage/7Dollar 等
  第三方主题）；预览框 1px/radius 8；无渐变/发光。
- 设置页完全使用标准 cbi 控件（主题的 `.cbi-section` 卡片、按钮、输入框）。
- 全部文案为英文 msgid + `po/zh_Hans/quartz-screen.po` 中文；菜单标题
  「Screen Display」→「屏幕显示」也在同一域。
- htdocs JS 走 JsMin（ES5 兼容写法）；自定义 CSS 通过 `E('style')` 注入，
  不经过 csstidy。

## 7. 验证

1. `node --check` 两视图语法（本地）。
2. VM：`quartz-screen run --fb-file /tmp/fb.raw --demo` + Playwright：
   - 菜单出现「屏幕显示」；
   - 状态页 8 个字段 + 预览图（data URL 非空）；
   - 切换暗/浅主题后预览重渲染；
   - 测试图案/重启按钮请求 200（fs.exec code 0）；
   - 设置页保存并应用后 uci 值落盘、服务重启、状态页亮度变化。
3. 真机：与 `quartz-screen/docs/DESIGN.md` §7 合并验收。

## 8. 边界

- 不做屏幕实时视频/高帧率预览（预览为按需离线渲染）；
- 不做屏幕端交互配置（无触摸）；
- 不改 `luci-theme-quartz`；其他主题下预览卡仍可读（token fallback）。

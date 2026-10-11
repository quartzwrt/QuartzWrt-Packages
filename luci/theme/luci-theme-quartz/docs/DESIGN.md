# luci-theme-quartz 设计规范

**版本**: v1.0（评审稿）
**日期**: 2026-10-12
**状态**: 设计定案，待用户评审 → 通过后按 §10 进入实施（W2.1 起）
**依据**: TDD §3.3（`QuartzWrt-Builder/docs/TDD.md`）；视觉参照 [macOS Vibrancy · StyleKit](https://www.stylekit.top/zh/styles/macos-vibrancy)（2026-10-12 抓取提取）

---

## 0. 定位

`luci-theme-quartz` 是 QuartzWrt 的全局 LuCI 主题：负责全部页面的换皮（含第三方 LuCI 应用），并为 WP-3 Dashboard 提供 token 与容器约定。

视觉方向（用户定案）：参照 **macOS Vibrancy** —— 以"克制"换质感。层级靠三级灰阶与 1px 半透明描边区分，不靠渐变、发光或重阴影；模糊只服务于固定 chrome（侧边栏 / 顶栏 / 模态）。

参考稿为**暗色原生**（不含浅色规范）；本项目采用**双模**：浅色按 macOS 亮色语义色板派生，遵循同一材质体系与克制规则，不做机械反相。

## 1. 设计语言（硬规则）

| # | 规则 |
| :--- | :--- |
| 1 | 禁止渐变、发光、装饰性动画（pulse/bounce/spin） |
| 2 | 阴影 ≤2px 扩散、仅模态使用；深度一律用灰阶层级 + 1px 描边表达 |
| 3 | 边框一律 1px，透明度 .08 / .12 两档 |
| 4 | 圆角上限 12px（卡片/模态），按钮/输入框 8px；禁止胶囊化（细小状态圆点除外） |
| 5 | 过渡仅挂 color / background-color / border-color，200ms ease-out；禁止位移、缩放类动效 |
| 6 | 按压反馈用 opacity .8，禁止上浮/缩放 |
| 7 | 彩色只用于文字高亮、状态色与主按钮（accent），禁止彩色背景块 |
| 8 | `prefers-reduced-motion` 时移除全部过渡 |

## 2. Token 层（暗/浅双模）

命名空间 `--qz-*`，与 WP-3 Dashboard 的 Tailwind 前缀约定共用；Dashboard 构建配置直接消费这些变量，保证"主题定义 token、Dashboard 只引用"的单一来源。

### 2.1 背景与材质

| Token | 暗色 | 浅色 | 用途 |
| :--- | :--- | :--- | :--- |
| `--qz-bg-base` | `#1c1c1e` | `#ececf0` | 窗口底色（壁纸层之上、面板之下） |
| `--qz-bg-panel` | `#2c2c2e` | `#ffffff` | 内容面板、卡片、字段集 |
| `--qz-bg-raised` | `#3a3a3c` | `#ffffff` + 描边 | 按钮等抬升元素 |
| `--qz-bg-well` | `#1c1c1e` | `#f2f2f7` | 输入框、代码块等"下沉"面 |
| `--qz-chrome` | `rgba(28,28,30,.80)` | `rgba(242,242,247,.72)` | 侧边栏 / 顶栏 / 模态遮罩，叠加 `blur(20px)` |
| `--qz-scrim` | `rgba(28,28,30,.45)` | `rgba(242,242,247,.55)` | 壁纸与内容之间的压暗/提亮层（保证对比度） |

暗色三级灰直接取自参考稿。浅色不反相：macOS 亮色下"抬升"是白、面板也是白（靠描边区分），"下沉"面用灰（systemGray6 系）。

### 2.2 描边与文字

| Token | 暗色 | 浅色 | 用途 |
| :--- | :--- | :--- | :--- |
| `--qz-border` | `rgba(255,255,255,.08)` | `rgba(0,0,0,.08)` | 常态描边、分隔线 |
| `--qz-border-strong` | `rgba(255,255,255,.12)` | `rgba(0,0,0,.12)` | 重点描边、按钮 |
| `--qz-text` | `rgba(255,255,255,.95)` | `rgba(0,0,0,.85)` | 正文、标题 |
| `--qz-text-2` | `rgba(255,255,255,.70)` | `rgba(0,0,0,.55)` | 次要信息 |
| `--qz-text-3` | `rgba(255,255,255,.40)` | `rgba(0,0,0,.30)` | 弱化信息 |

对比度约束：`--qz-text-3` 暗色下约 3.8:1，**只准用于禁用态与装饰性标签**；任何可读内容最低 `--qz-text-2`（暗色 ≈8:1、浅色 ≈4.7:1）。

### 2.3 强调、状态与交互

| Token | 暗色 | 浅色 | 备注 |
| :--- | :--- | :--- | :--- |
| `--qz-accent` | `#0a84ff` | `#007aff` | macOS 系统蓝 亮/暗配对 |
| `--qz-ok` | `#30d158` | `#34c759` | macOS 系统绿配对 |
| `--qz-warn` | `#ff9f0a` | `#ff9500` | macOS 系统橙配对 |
| `--qz-err` | `#ff453a` | `#ff3b30` | macOS 系统红配对 |
| `--qz-focus` | `rgba(10,132,255,.30)` | `rgba(0,122,255,.30)` | 焦点环：`0 0 0 2px`，用于输入框/按钮/链接 |
| hover 叠加 | `rgba(255,255,255,.05)` | `rgba(0,0,0,.04)` | 列表项/侧边栏项/普通按钮 |
| active 叠加 | `rgba(255,255,255,.10)` | `rgba(0,0,0,.08)` | 选中态 |

### 2.4 形状与字体

| Token | 值 |
| :--- | :--- |
| `--qz-radius` / `--qz-radius-sm` | `12px` / `8px` |
| `--qz-blur` | `20px` |
| `--qz-font-sans` | `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif` |
| `--qz-font-serif` | `"Iowan Old Style", "Palatino Linotype", "Songti SC", "Noto Serif CJK SC", SimSun, Georgia, serif` |
| `--qz-font-mono` | `ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, "Noto Sans Mono CJK SC", monospace` |

字体策略（用户定案）：**一律系统字体，零 webfont**。衬线仅用于登录页 / Dashboard hero 等"品牌时刻"，接受随平台漂移（中文衬线在各平台可用性不一，属已知接受项；wordmark 优先用 SVG 资产锁定观感）。功能 UI 用无衬线栈；IP / MAC / 吞吐率 / 日志 / 代码块等技术数值一律等宽栈。

## 3. LuCI 组件映射

```
┌──────────────┬───────────────────────────────┐
│ 侧边栏 chrome │ 顶栏 chrome（固定，blur）        │
│ 固定 + blur   ├───────────────────────────────┤
│              │ 内容区 bg-base                  │
│              │  └ 字段集/卡片 bg-panel + 描边   │
│              │    输入 bg-well  按钮 bg-raised  │
└──────────────┴───────────────────────────────┘
  最底层：壁纸层（噪点或用户壁纸）+ scrim
```

| LuCI 元素 | 样式落点 |
| :--- | :--- |
| 侧边栏 mainmenu / 顶栏 | `--qz-chrome` + blur，固定定位；激活项 active 叠加 + `--qz-text`；hover 用 hover 叠加 |
| 内容区背景 | `--qz-bg-base` |
| 字段集 fieldset / cbi-section / 卡片 | `--qz-bg-panel` + 1px `--qz-border` + `--qz-radius` |
| 输入类（cbi-input-*、下拉、textarea） | `--qz-bg-well` + `--qz-border`；focus 时边框提亮 + `--qz-focus` 环 |
| 按钮（cbi-button） | 普通 = `--qz-bg-raised`（浅色配描边）；主按钮（-apply/-save/-action）= accent 实心白字；危险操作 = `--qz-err` |
| 表格（cbi-table / table） | 容器 panel；表头 `--qz-text-2`；行分隔 1px border；行 hover 叠加 |
| Tabs（cbi-tabmenu） | 选中项用 active 叠加 + 文字提亮，不用彩色底 |
| 模态（cbi-modal） | 遮罩 = scrim + blur；面板 = `--qz-bg-panel` + `--qz-radius` + 2px 阴影（阴影唯一使用点） |
| 通知（LuCI ui.js） | 深/浅面板 + 左侧 2px 状态色条（彩色仅作提示条，不作底） |
| 徽标 / Badge | 8px 圆角矩形 + 状态色文字 |
| 登录页 | 品牌时刻：壁纸直出 + chrome 材质卡片浮层 + wordmark（SVG） |

**版式密度**：参考稿是 hero 大标题、大间距的展示型页面；LuCI 是高密度管理界面。风格 DNA 全盘保留，版式按管理界面重设——正文基准 14px（text-sm），大标题体系仅登录页与 Dashboard hero 使用。

## 4. 背景与壁纸

Vibrancy 需要"可模糊之物"：内容从 chrome 下滚过是其一，背景层是其二。

- **默认**：极低对比度石英噪点纹理（可平铺，暗/浅两变体，各 <2KB）。既是"石英质感"的字面落地，又为模糊提供材质采样；不违反禁渐变规则（噪点非渐变）。
- **用户自备壁纸**（用户定案，机制先行、上传 UI 后置至 Dashboard / App 二期）：
  - 实现：背景层用 CSS 多层背景 `url('/quartz-wallpaper/wallpaper.jpg'), url(噪点)`——第一层 404 时浏览器自动跳过、噪点兜底，无需 JS 探测。
  - 投放目录 `/www/quartz-wallpaper/` **独立于主题包**，apk 包升级不会覆盖用户文件。
  - scrim 层压在壁纸之上保证文字对比度；`prefers-reduced-transparency` 时壁纸层降为纯色。
  - 建议约束（写入用户文档）：≤2MB、宽 ≥1920px、JPEG；上传 UI 落地时做客户端压缩与尺寸校验。

## 5. 双模机制

- `:root` 定义浅色；`@media (prefers-color-scheme: dark)` 覆写为暗色；`html[data-qz-theme="dark"|"light"]` 手动覆盖优先级最高。
- 顶栏切换钮三态：跟随系统 / 暗 / 浅；手动态存 `localStorage`（MVP）。跨设备持久化（uci）列为后续增强。
- **检出基座后验证**：若 ImmortalWrt 25.12 的 luci-base 提供内建暗色机制，优先复用，否则按上述自管。

## 6. 性能护栏

| 项 | 约束 |
| :--- | :--- |
| blur 范围 | 仅固定 chrome 与模态遮罩；滚动容器禁挂；同屏模糊层 ≤3 |
| 回退 | `@supports not (backdrop-filter: blur(1px))` → chrome 用不透明 `--qz-bg-panel` |
| 体积预算 | cascade.css 压缩后 ≤60KB；主题 JS <10KB；噪点图 <2KB；零 webfont |
| 动效 | 仅颜色过渡，无 transform 动画 |
| 无障碍 | `prefers-reduced-motion` 去过渡；`prefers-reduced-transparency` 去模糊与壁纸 |

## 7. dev/preview.html（评审与回归）

开发目录 `dev/`（**不进安装包**；luci.mk 只收集 htdocs / luasrc / root / po）。单页静态渲染：token 色板、双模切换、cbi 控件样张（输入/按钮/表格/tabs/模态/通知）、侧边栏 + 顶栏布局、登录页、壁纸三态（噪点 / 自备壁纸 / 减少透明度）。

用途：设计评审不用等固件构建；后续换皮以此为视觉回归基线。

## 8. 包结构

```text
luci/theme/luci-theme-quartz/
├── Makefile                     # include $(TOPDIR)/feeds/luci/luci.mk（同 argon 方式）
├── docs/DESIGN.md               # 本文件
├── dev/                         # preview.html + 噪点纹理生成脚本（不进包）
├── htdocs/luci-static/quartz/
│   ├── cascade.css              # 全部样式（手写 CSS，非 Tailwind）
│   ├── theme.js                 # 模式切换（<10KB）
│   ├── noise-dark.png / noise-light.png
│   └── logo.svg / favicon.png
├── luasrc/view/themes/quartz/
│   └── header.htm / footer.htm / login.htm
├── po/                          # zh_Hans / en
└── root/etc/uci-defaults/40-luci-theme-quartz   # 首启设默认主题
```

主题模板骨架以 luci-theme-bootstrap 为底（兼容性最稳、模板体量最小），CSS 100% 自研。

## 9. 与 WP-3 的接口约定

- **token 单一来源**：主题 `cascade.css` 的 `:root` 变量；Dashboard 的 Tailwind 配置把颜色映射到 `var(--qz-*)`，不另起色板。
- **Tailwind 隔离**（TDD §8 待决项）：方案 A「不开 preflight（不引 `@tailwind base`）+ utilities `qz-` 前缀」先行；引入第三方组件库时升级为 PostCSS prefixwrap 作用域方案。spike 验证后结论写回 TDD §8。
- **容器约定**：Dashboard 渲染在主题内容区内，只消费 token，不改写主题样式（不引入全局 reset 依赖）。

## 10. 交付物分解（W2.x）

| 序号 | 内容 | 依赖 |
| :--- | :--- | :--- |
| W2.0 | 本设计文档 + preview 骨架 | — |
| W2.1 | 包骨架（Makefile + 模板 + 最小 CSS），单包编译验证 | WP-1 基础设施（build-local.sh 或 CI） |
| W2.2 | header / footer / login 模板 + 侧边栏 | W2.1 |
| W2.3 | token 层（双模 + 噪点/壁纸 + 字体栈） | W2.2 |
| W2.4 | cbi 全面换皮（表单/表格/按钮/tabs/模态/通知） | W2.3 |
| W2.5 | Tailwind 隔离 spike + 结论写回 TDD §8 | W2.3 |
| W2.6 | Logo 定稿入库（SVG 候选 → 用户挑选） | W2.3 |
| W2.7 | 收尾：quartz-defaults 追加依赖（共享文件，追加式）、默认主题、验收 | W2.4–W2.6 |

**验收标准**：① 主题可编译进固件（单包编译 + CI 全量双保险）；② 原生 cbi 页面未被污染（preview 注入对比 + 真机/虚拟机过一遍 firewall 等复杂页）；③ 双模在 preview 页评审通过；④ 性能护栏达标（§6）。

## 11. TDD 同步清单（待写入两处副本，需用户/架构确认）

1. §8 待决事项「UI 隔离」→ 已决策：方案 A 先行（spike 通过后回填结论）。
2. §3.3 / §8：新增「主题双模」决策条目与 DESIGN.md 指针。
3. §4.4 运行时资源：壁纸投放目录（`/www/quartz-wallpaper/`，用户文件、不随包升级）纳入治理清单。

## 12. 未决与验证项

- **Logo**：待手绘 SVG 候选（石英晶体图形 + wordmark）供用户挑选；在此之前用占位。
- **验证项（检出基座后执行）**：luci.mk 引用路径、主题注册机制（mediaurlbase）、luci-base 双模机制、uci-defaults 在 apk 体系下的首启行为、cbi 类名现状。
- **可选增强（后置）**：强调色可换（macOS 本身允许）、跨设备主题持久化（uci）、壁纸上传 UI。

---

*变更记录：2026-10-12 v1.0 —— 依据用户定案（参照 macOS Vibrancy / 系统字体 / 双模 / 噪点 + 自备壁纸 / preview 评审）成文。*

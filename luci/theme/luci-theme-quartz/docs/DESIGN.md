# luci-theme-quartz 设计规范

**版本**: v1.4
**日期**: 2026-10-12（v1.3：2026-10-14）
**状态**: 设计定案 + 实现完成（W2.0–W2.10 全量关闭 + W2.11 页底 chrome）；W2.7 锁定固件复核、W2.9 壁纸管理面 VM 验收 15/15、W2.10 R1 反馈轮 VM 复验、W2.11 页脚材质 VM 复验；进度与剩余项见 §10、§12
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
| `--qz-bg-panel` | `rgba(44,44,46,α)` | `rgba(255,255,255,α)` | 内容面板、卡片、字段集（α = `--qz-panel-alpha`，默认 1） |
| `--qz-bg-raised` | `#3a3a3c` | `#ffffff` + 描边 | 按钮等抬升元素（保持不透明） |
| `--qz-bg-well` | `rgba(28,28,30,α)` | `rgba(242,242,247,α)` | 输入框、代码块等"下沉"面 |
| `--qz-chrome` | `rgba(28,28,30,α×.80)` | `rgba(242,242,247,α×.72)` | 侧边栏 / 顶栏 / 页面操作条 / 页脚（版本行） / 模态遮罩，叠加 `blur(20px)`；**透明度与内容面板同源**（`.72`/`.80` 是 `α=1` 时的基准值，配置调低时整界面同步变透，chrome 恒比面板更透） |
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
| 侧边栏开合（顶栏汉堡） | 窄屏(≤900px)：抽屉式（`html.qz-nav-open` + 遮罩，默认收起）；桌面(>900px)：收起切换（`html.qz-nav-collapsed`，侧边栏左移 + 顶栏/内容列让位），选择存 localStorage、header 首绘前恢复 |
| 内容区背景 | `--qz-bg-base` |
| 字段集 fieldset / cbi-section / 卡片 | `--qz-bg-panel` + 1px `--qz-border` + `--qz-radius` |
| 输入类（cbi-input-*、下拉、textarea） | `--qz-bg-well` + `--qz-border`；focus 时边框提亮 + `--qz-focus` 环 |
| 按钮（cbi-button） | 普通 = `--qz-bg-raised`（浅色配描边）；主按钮（-apply/-save/-action）= accent 实心白字；危险操作 = `--qz-err` |
| 表格（cbi-table / table） | 容器 panel；表头 `--qz-text-2`；行分隔 1px border；行 hover 叠加 |
| Tabs（cbi-tabmenu） | 选中项用 active 叠加 + 文字提亮，不用彩色底 |
| 模态（cbi-modal） | 遮罩 = scrim + blur；面板 = `--qz-bg-panel` + `--qz-radius` + 2px 阴影（阴影唯一使用点） |
| 通知（LuCI ui.js） | 深/浅面板 + 左侧 2px 状态色条（彩色仅作提示条，不作底） |
| 徽标 / Badge | 8px 圆角矩形 + 状态色文字 |
| 页脚（版本行） | **页底 chrome**：`--qz-chrome` + blur；与贴底操作条相邻时由操作条的上边框承担分隔、两者合并为一条 chrome 面（`:has(.cbi-page-actions)` 抑制自身描边），独处页底时自己画 1px 描边；`--qz-text-3` + 12px 保持"页脚注脚"层级 |
| 登录页 | 品牌时刻：壁纸直出 + chrome 材质卡片浮层 + wordmark（SVG） |

**版式密度**：参考稿是 hero 大标题、大间距的展示型页面；LuCI 是高密度管理界面。风格 DNA 全盘保留，版式按管理界面重设——正文基准 14px（text-sm），大标题体系仅登录页与 Dashboard hero 使用。

## 4. 背景与壁纸

Vibrancy 需要"可模糊之物"：内容从 chrome 下滚过是其一，背景层是其二。

- **默认**：极低对比度石英噪点纹理（可平铺，暗/浅两变体，各 <2KB）。既是"石英质感"的字面落地，又为模糊提供材质采样；不违反禁渐变规则（噪点非渐变）。
- **用户自备壁纸**（用户定案；**上传 UI 见 §5.1，W2.9 已落地**）：
  - 实现：`theme.js` 用一次 `Image()` 探测 `/quartz-wallpaper/wallpaper.jpg`，命中则置 `html[data-qz-wallpaper]`；CSS 据此打开壁纸层（`body::before`）与 scrim 层（`body::after`）。无壁纸时探测即一次 404，可接受。
  - 状态即文件：**存在即启用**，不占 uci 选项；上传/替换/移除都在配置页完成（§5.1）。
  - 踩坑记录：自定义属性中的相对 URL 按声明位置解析，壁纸与预览中的图片 URL 一律使用**绝对路径**。
  - 投放目录 `/www/quartz-wallpaper/` **独立于主题包**，apk 包升级不会覆盖用户文件；配置包投放 `/lib/upgrade/keep.d/quartz-wallpaper`，固件 sysupgrade 后同样保留。
  - scrim 层压在壁纸之上保证文字对比度；`prefers-reduced-transparency` 时壁纸与模糊整体退化为纯色。
  - 约束（上传 UI 落实，DESIGN 原建议项）：源文件 ≤25MB（解码前拒绝，解码炸弹护栏）、最长边收敛到 ≤1920px、统一转 JPEG（透明通道压平到 `#1c1c1e`）、逐档降质至 ≤2MB。落盘文件固定为 `wallpaper.jpg` 名字与 `.jpg` URL，格式由客户端转码保证。
  - **缓存语义（已知边界）**：URL 固定，无版本号。上传方在本页即时生效（带 `?v=<md5>`）；其他已打开的设备在“旧图仍在启发式新鲜期内”可能短暂显示旧壁纸，强制刷新即恢复（配置页已提示）。如需彻底消除，改进方向是服务端把文件 mtime 注入模板（`header.ut` 用 ucode `stat()` 输出 `data-qz-wallpaper-ver`，见 §12）。

## 5. 双模机制（已对齐基座原生机制，源码核实）

基座 LuCI 25.12 原生提供 `html[data-darkmode="true|false"]` 外观开关（luci-theme-bootstrap 同款），且生态应用（OpenClash、luci-mod-dashboard 等）均以此属性响应暗色。本主题直接对齐该机制，不自造一套：

- `ucode/template/themes/quartz/header.ut` 内联脚本在首绘前设定 `data-darkmode` 与 `data-qz-mode`：`localStorage('qz-theme')` 手动选择优先，否则跟随 `prefers-color-scheme`（避免闪烁）。
- 顶栏三态切换钮（跟随系统 / 深 / 浅）由 `theme.js` 维护；`color-scheme` 随模式同步，原生控件与滚动条自动适配。
- 主题注册：`luci.themes.Quartz=/luci-static/quartz`（uci-defaults 注册；全新安装时设为默认，升级不覆盖用户选择）。模板按 `themes/$(basename mediaurlbase)/` 解析为 `ucode/template/themes/quartz/*.ut`。
- **兼容层**：cascade.css 同时提供 bootstrap 旧变量名（`--background-color-*` / `--text-color-*` / `--border-color-*` / `--primary-color-*` / `--error/success/warn-*` 等）到 `--qz-*` 的别名映射，引用旧变量的第三方应用自动适配双模与 Quartz 色板。
- **布局工具类语义（对齐 bootstrap，`0c6f665`）**：`.pull-left/.pull-right` 才是浮动；`.left/.right` 是 `text-align`（`!important`）——LuCI 标记大量以 `td.left`/`td.right` 表示文本对齐，若按浮动实现会把真实表格单元格 block 化（行宽/分隔线/底色半途而止，见 WORKPLAN 坑位 #22）。
- 跨设备持久化（uci）列为后续增强。

### 5.1 外观配置包（luci-app-quartz-config）

主题的可配置项由**独立配置包**提供（用户定案）：主题本体不含设置界面，未安装配置包或读取失败时一律回退内置默认值，两者可独立工作。

| 选项（`/etc/config/quartz`，section `theme`） | 范围 | 默认 | 作用 |
| :--- | :--- | :--- | :--- |
| `panel_opacity` | 50–100 (%) | 100 | **全界面不透明度**：内容面板/输入井 + chrome（侧边栏/顶栏/页面操作条/登录卡，见 §2.1 chrome 行）；<100% 时自动启用面板毛玻璃（`html[data-qz-translucent]`） |
| `blur` | 0–40 (px) | 20 | `--qz-blur`：侧边栏、顶栏与半透明面板的背景模糊量 |
| `default_mode` | auto / dark / light | auto | 首次访问者的默认外观（浏览器内手动选择优先） |

- **生效路径**：`header.ut` 每次渲染读出 uci，输出 `<style>:root{--qz-panel-alpha;--qz-blur}</style>` 与 `data-qz-translucent` / `data-qz-default` 属性；保存后下次页面加载生效，无需重启服务。**ucode 整除陷阱**：`panel_opacity / 100` 在 ucode 里是**整数除法**（两侧都是 int 时），`50/100 → 0`——`--qz-panel-alpha: 0` 会让面板全透明，必须写成 `/ 100.0`（2026-10-14 用户实测反馈修复，见 §12）。
- **chrome 与面板同源（2026-10-14）**：`--qz-chrome = rgba(var(--qz-chrome-rgb), calc(var(--qz-panel-alpha) * var(--qz-chrome-base-alpha)))`——设计基准 `.72`/`.80` 即 `α=1` 时的取值，配置调低时侧边栏/顶栏/操作条/登录卡同步变透（chrome 恒比面板更透，材质层级不变）；`α=1` 时与旧值逐字节等价，默认观感零变化。
- **性能**：面板毛玻璃仅在半透明时挂 `backdrop-filter`（见 §6）；不透明状态下零额外合成开销。
- **预览**：`dev/preview.html` 右下角提供等价滑杆（面板透明度 / 模糊强度），可实时体验。
- 配置页位于 LuCI「系统 → Quartz Theme」（menu.d `admin/system/quartz-config`）。
- `quartz-defaults` 元包同时依赖主题与配置包（随发行版默认携带，可在 LuCI 主题选择器切换）。

**壁纸管理（W2.9）**——配置页第二块，**不占 uci 选项**（状态即文件，见 §4）：

| 动作 | 机制 | 落地文件 |
| :--- | :--- | :--- |
| 上传/替换 | 浏览器内 canvas 收敛（≤1920px / JPEG / ≤2MB，源图 ≤25MB 护栏）→ `cgi-upload` multipart → 同请求 `filemode=644`（cgi-upload 默认 0600，uhttpd 拒发非全局可读文件） | `/www/quartz-wallpaper/wallpaper.jpg` |
| 移除 | rpcd `file remove`（NotFound 视为已达成，幂等） | 同上（`.placeholder` 保证目录仍在） |
| 状态/预览 | rpcd `file stat`（体积/更新时间）+ 图片自带尺寸，预览 URL 带 `?v=<mtime>` 缓存失效；上传后本页即时生效（改写 `:root` 的 `--qz-wallpaper-on` 与 `[data-qz-wallpaper]`），无需保存表单 | — |

- **ACL 最小化**（`acl.d`）：只逐字授权 `/www/quartz-wallpaper/wallpaper.jpg` 这一个路径（read/list/write/remove）+ 所需 ubus 方法（`file` 的 `stat`/`remove`）与 `cgi-io upload`；目标路径是常量，用户输入不参与路径构造（同 footstrap 的安全姿势）。
- **不随升级覆盖**：`root/www/quartz-wallpaper/.placeholder` 只为随包创建目录（用户文件与包文件同名与否都不会被 apk 删除）；`root/lib/upgrade/keep.d/quartz-wallpaper` → 固件 sysupgrade 后保留 `/www/quartz-wallpaper`（与 bandix/qmodem 同机制）。
- **验收**：`dev/wallpaper-e2e.py`（Playwright + VM，15/15：初始态 / 上传收敛 1920x1080 / 落盘 JPEG 200+魔数 / 本页即时生效 / 跨页探测命中 / 替换 md5 变化 / 移除 404 与回落 / 无 JS 异常）；证据 `dev/shots/vm-wallpaper/`。

## 6. 性能护栏

| 项 | 约束 |
| :--- | :--- |
| blur 范围 | 默认仅固定 chrome 与模态遮罩；面板毛玻璃为用户显式开启（默认关闭）；开启后滚动面板参与模糊，弱机型可通过配置包调低 blur 或保持 100% 不透明 |
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
├── Makefile                     # include $(TOPDIR)/feeds/luci/luci.mk
├── docs/DESIGN.md               # 本文件
├── dev/                         # 开发资源（不进安装包）
│   ├── preview.html             # 主界面设计预览（加载真实 cascade.css）
│   ├── preview-login.html       # 登录页预览（壁纸品牌时刻）
│   ├── gen-assets.py            # 噪点纹理生成（纯标准库，可复现）
│   ├── wallpaper-sample.svg     # 示例壁纸（仅预览用）
│   └── shots/                   # 渲染基准截图（评审与视觉回归）
├── root/
│   ├── www/luci-static/quartz/  # 静态资源（走 root/ 原样安装，见下方说明）
│   │   ├── cascade.css          # 全部样式（手写 CSS，双模 token + cbi 全组件）
│   │   ├── theme.js             # 三态外观切换 / 壁纸探测 / 侧边栏开合（窄屏抽屉 + 桌面收起）
│   │   ├── noise-dark.png / noise-light.png   # 石英噪点（各 <2KB，96px 可平铺）
│   │   └── logo.svg             # 切面宝石标记（方案 B，2026-10-12 暂定）
│   └── etc/uci-defaults/40-luci-theme-quartz   # 注册主题 + 首装设默认
├── htdocs/luci-static/resources/
│   ├── menu-quartz.js           # 侧边栏菜单（改自 menu-bootstrap.js）
│   └── view/quartz/sysauth.js   # 登录视图（注入品牌区）
└── ucode/template/themes/quartz/
    └── header.ut / footer.ut / sysauth.ut  # LuCI 25.12 的 ucode 模板（非旧式 .htm）
```

**静态资源的安装路径（构建期处理链，2026-10-12 容器实测）**：LuCI 构建在 `htdocs/` 安装路径上会跑两个遗留压缩器——csstidy（CSS）与 classic jsmin（JS）。**csstidy 1.x 不认识 CSS `@layer`**：它带 `--remove_last_semicolon` 运行，会剥掉 `@layer theme, base, …;` 语句的分号，导致整张样式表解析失败、主题完全失效（正是"真机全裸"事故的根因）。因此本包的静态资源（含需要入层的 cascade.css）**走 `root/www/` 原样安装**，绕开压缩器；JS 保留在 `htdocs/`（JsMin 实测无损，但**约束：本包 JS 必须保持 ES5 兼容写法**）。其他后续要发现代 CSS（尤其 Tailwind 产物）的包请遵循同一规则，或由 WP-1 统一评估关闭 `CONFIG_LUCI_CSSTIDY`。

主题骨架结构以 luci-theme-bootstrap 为底（Apache-2.0，兼容性最稳）；CSS 100% 自研。注意 25.12 的 LuCI 模板体系已是 **ucode 模板（`.ut`，位于 `ucode/template/themes/<主题名>/`）**，旧式 `luasrc/view/themes/*.htm` 不再适用；`luci.mk` 会把 `ucode/` 安装到 `/usr/share/ucode/luci/`。

配套配置包（独立安装，主题可脱离它工作）：

```text
luci/applications/luci-app-quartz-config/
├── Makefile                     # LUCI_DEPENDS:=+luci-base +luci-theme-quartz
├── htdocs/luci-static/resources/view/quartz/config.js   # 配置页（JS CBI：外观 + 壁纸）
├── root/etc/config/quartz       # 默认配置（section 'theme'）
├── root/usr/share/luci/menu.d/luci-app-quartz-config.json
├── root/usr/share/rpcd/acl.d/luci-app-quartz-config.json
├── root/www/quartz-wallpaper/.placeholder       # 随包创建壁纸投放目录（用户文件不随升级覆盖）
├── root/lib/upgrade/keep.d/quartz-wallpaper     # sysupgrade 保留 /www/quartz-wallpaper
├── po/zh_Hans/quartz-config.po  # 中文翻译
└── dev/                         # 开发资源（不进安装包）
    ├── wallpaper-e2e.py         # 壁纸端到端验收（Playwright + VM 预览台）
    └── shots/vm-wallpaper/      # 验收证据（截图 + run-log.jsonl）
```

## 9. 与 WP-3 的接口约定

- **token 单一来源**：主题 `cascade.css` 的 `:root` 变量；Dashboard 的 Tailwind 配置把颜色映射到 `var(--qz-*)`，不另起色板。
- **Tailwind 隔离**（已实测定案，TDD §8 已写回）：dashboard 侧构建入口为 `@import "tailwindcss" prefix(qz) source(none);` + `@source` 精确圈定源码，类名写 `qz:` 形式（v4 前缀是变体语法且只能配在完整导入上）；**主题 cascade.css 入层**（层序 `theme < base < qz-theme < components < utilities`），使 preflight（base 层）天然弱于主题、utilities 天然强于主题；preflight 的"单侧泄漏"（主题未声明过的属性，如 `img{display}`）由主题内显式中和规则处理（已内置）。实测矩阵与依据见 `dev/spike/tailwind/RESULTS.md`。
- **token 命名重叠提示**：Tailwind 生成的 CSS 变量同样带 `qz` 前缀（`--qz-font-sans`、`--qz-radius-sm` 等）与主题 token 同命名空间；层序上主题值胜出（正是 token 单一来源所期望的方向），`--radius-sm` 等语义差异由 dashboard 侧 `@theme` 显式对齐。
- **容器约定**：Dashboard 渲染在主题内容区内，只消费 token，不改写主题样式（不引入全局 reset 依赖）。
- **分层边界与第三方样式（2026-10-12 复测新增）**：**未分层的第三方 CSS 优先于任何 `@layer` 内声明（级联规则，与特异性无关）**——第三方应用自带的全局 reset（如 OpenClash `oc.css` 的 `* { margin: 0; padding: 0 }`）会整片压掉主题层内样式（实测页面塌向侧边栏）。主题文件末尾（层外）提供增量兜底：`html, body, html *, html *::before, html *::after { margin: revert-layer; padding: revert-layer; }`，把 margin/padding 回退到主题分层级联值；第三方自己组件的类级样式不受影响。新增第三方样式依赖（更多全局 reset、其他属性）时先评估该边界（WORKPLAN 坑位 #20）。

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
| W2.8 | 外观配置包 `luci-app-quartz-config`（面板透明度/模糊强度/默认外观） | W2.3 |
| W2.9 | 壁纸管理面（配置包内上传/替换/移除；客户端收敛 + ACL 最小化 + sysupgrade 保留） | W2.3 / W2.8 |
| W2.10 | ✅ R1 用户实测反馈（2026-10-14）：`panel_opacity` 语义扩为全界面（chrome 跟随面板，§2.1/§5.1）+ 修 ucode 整除致 `--qz-panel-alpha: 0`（`/100.0`）+ 配置页文案同步 | W2.8 / W2.9 |
| W2.11 | ✅ R1 反馈补充（2026-10-14）：页脚改为页底 chrome（与贴底操作条同源材质、相邻时合并为一条 chrome 面，§3） | W2.4 |

**验收标准**：① 主题可编译进固件（单包编译 + CI 全量双保险）；② 原生 cbi 页面未被污染（preview 注入对比 + 真机/虚拟机过一遍 firewall 等复杂页）；③ 双模在 preview 页评审通过；④ 性能护栏达标（§6）。

**实现状态（2026-10-12）**：

- ✅ W2.0 设计文档 + 预览页（`dev/preview.html`、`dev/preview-login.html`，含渲染截图 `dev/shots/`）
- ✅ W2.1 骨架包 / W2.2 模板 / W2.3 token 层 / W2.4 cbi 换皮——**初版完成**，已通过 Edge 无头渲染校验（暗/浅/壁纸/登录/半透明面板六态）
- ✅ W2.8 外观配置包 `luci-app-quartz-config` 初版；`quartz-defaults` 已追加 `+luci-theme-quartz +luci-app-quartz-config`
- ✅ W2.5 Tailwind 隔离 spike 完成：结论已写回 TDD §8（两副本同步）；实测矩阵见 `dev/spike/tailwind/RESULTS.md`
- 🚧 W2.6 Logo：四方向候选已出（`dev/logo-candidates.html` + `dev/shots/logo-candidates.png`），待用户选定后细化
- ✅ **W2.6.5 全页面逐页真机测试（2026-10-12，VM 预览台）**：Playwright + QEMU VM（`dev/vm-test/`，59 页：菜单树 50 view + OpenClash 6 + 登录/404/退出），暗/浅双轮全量截图（`dev/shots/vm-test-dark/`、`vm-test-light/`）。**发现并修复 4 个 cbi 机制缺口**（均为 bootstrap 有而主题缺失的机制，属 W2.4 换皮遗漏）：
  ① `.cbi-tooltip` 无隐藏/悬停机制（工具提示常显遮挡，防火墙状态页可见）；
  ② `.cbi-progressbar` 缺 `::before{content:attr(title)}`（概览页内存/存储读数不显示）；
  ③ `[data-tab-title]`/`[data-tab-active]` 分区页签收起规则缺失（系统/路由/DHCP 等页所有页签内容同时铺开）；
  ④ `.cbi-value-field{display:flex}` 泛化误伤 `td.cbi-value-field`（可编辑分区表整行塌成竖排——通信规则/区域/DHCP 静态租约等）。
  另确认 3 项非主题问题：实时图表白底（上游内联样式）、Dockerman 概览横向溢出（插件 JSON 长值表，bootstrap 同）、sticky 应用条在全页截图中的中段悬停（截图假象，实际贴视口底）。
- ✅ **W2.6.6 复测轮修复（2026-10-12，用户 VM 实测反馈，`e0d681c`/`bc78dd3`）**：①第三方无分层全局 reset 压制主题 `@layer`（OpenClash 页面整页塌）→ 层外 `revert-layer` 兜底（§9 已记）；②「保存并应用/强制应用」被 `.cbi-dropdown{width:100%}` 撑成大蓝条 → 宽度改 `:not(.btn):not(.cbi-button)` 作用域（bootstrap 同款），应用条恢复右下紧凑排布；③密码控件组 `.control-group` 缺规则 → input 独占整行、显示/隐藏按钮掉行，补 inline-flex 同行合并边框 + 组宽 400px；④**下拉部件收起/展开显隐机制整组补齐**（`li{display:none}`+`li[display]{display:flex}` 收起只显当前项、`[empty]` 显占位、`[open] > ul.dropdown{display:block}` 竖排平铺、`.more` 仅 `[multiple][more/empty]`）——缺失时所有下拉收起态把全部选项铺开。双模 59 页证据全量重拍。
- ✅ **W2.6.7 复测轮补充 2（2026-10-12 用户实测反馈，`5fc60ec`）**：①**桌面侧边栏收起**能力补齐（此前从未实现：汉堡仅 ≤900px 显示）——汉堡全宽度可见，桌面点击切换 `html.qz-nav-collapsed`（侧边栏左移 + 顶栏/内容列让位，.24s 与窄屏一致），localStorage 持久化 + 首绘前恢复 + 跨断点清理互斥态；②横幅 `.alert-message` 浮动按钮**逸出卡片**并挤窄 Dashboard 首卡（非 BFC 根 → 浮动进入共享 BFC，flex 卡避让收缩 1204→1063）→ 卡片 `display: flow-root` 自包含（WORKPLAN 坑位 #21）。证据双模 60 页重拍 + `dev/shots/sidebar-{collapsed,expanded}.png`。
- ✅ **W2.6.8 工具类语义修复（2026-10-12，用户对照 argon 实测，`0c6f665`）**：`.left/.right` 由浮动改回 bootstrap 语义（text-align），修复概览页/实时图表页因 `td.left/td.right` 被 float 块化导致的行宽、分隔线、悬停底色与右对齐统计失效（§5 兼容层已记；坑位 #22）。
- ✅ **W2.7 收尾（2026-10-14，本对话，用户指令「清小尾巴」）**：**默认主题 + 机制逐项在锁定固件上复核通过**（x86_64 镜像 + QEMU VM 首启实测）：
  ① 镜像工厂层：`/etc/config/luci` 为基座预置 `mediaurlbase=/luci-static/bootstrap`，镜像内 `/etc/uci-defaults/40-luci-theme-quartz`（755）待首启执行（debugfs 直读 rootfs 分区核实）；
  ② 首启后（VM 在线态）：`luci.main.mediaurlbase=/luci-static/quartz`、`luci.themes.Quartz=/luci-static/quartz`、`/etc/uci-defaults` 已清空（脚本已消费）——**全新安装默认 quartz 主题成立**（升级保留用户选择的逻辑按 `PKG_UPGRADE` 判定，未动）；
  ③ 模板机制：`/usr/share/ucode/luci/template/themes/quartz/{header,footer,sysauth}.ut` 就位（`themes/$(basename mediaurlbase)` 解析）；
  ④ 静态资源：`/www/luci-static/quartz/` 五件（cascade.css/theme.js/logo.svg/noise-{dark,light}.png）与源码仓库 **sha256 逐字节一致**（未过 csstidy/JsMin，坑位 #15 规避生效）；
  ⑤ 端到端：登录页 HTML 引用 `luci-static/quartz/{cascade.css,theme.js,logo.svg}` 且含 `data-qz-default`/`qz-*` 标记——主题确实渲染。
  证据：本节命令行记录 + `dev/shots/vm-test-{dark,light}/`（此前 59 页×双模全量截图）。
- ✅ **W2.11 页脚页底 chrome（2026-10-14，R1 反馈补充，用户实测「页脚版本行透明、与上方半透明操作条不协调」）**：`footer` 由裸文本改为 `--qz-chrome` + `blur(var(--qz-blur))` 的页底 chrome 面，padding 调整为 `8px 24px 10px`；有页面操作条时 `body:has(.cbi-page-actions) footer{border-top:none}` 抑制描边、两者合并为一条 chrome 面（分隔线由操作条上边框承担），无操作条时保留 1px 描边；`@supports not (backdrop-filter)` 与 `prefers-reduced-transparency` 兜底列表同步纳入 `footer`（此时 chrome 退化为实心、去模糊）。**VM 复验**（用户壁纸 + 不透明度 50% 的实机态）：computed `background-color: rgba(28,28,30,.4)`/`rgba(242,242,247,.36)` + `blur(20px)`；有操作条页 `border-top-width: 0px`、无操作条页 `1px rgba(255,255,255,.08)`；页脚盒 [864,900] 紧接操作条 [806,864]（视口 900）连成一条带。证据 `dev/shots/footer-r1/`（长页×双模×壁纸/无壁纸 + 无操作条 + 短页）。
- ✅ **单包编译验证（随每次全量构建持续复核）**：`luci-theme-quartz` apk 每次固件构建产出并在 manifest 在列（x86_64 与 rockchip/armv8 双目标；镜像级三层验证见 WORKPLAN §7 日志 2026-10-12 条目）。
- ✅ **W2.9 壁纸管理面（2026-10-14，本对话，用户指令「WP-2 的壁纸功能没加上，在 config 软件包里加进去」）**：把 DESIGN §4「机制先行、上传 UI 后置」的欠账补进 `luci-app-quartz-config`——配置页新增壁纸区块（预览 + 体积/尺寸/更新时间 + 上传/替换/移除），客户端收敛（源图 ≤25MB 护栏、最长边 ≤1920px、转 JPEG 压平透明、逐档降质至 ≤2MB），上传走 `cgi-upload`（同请求 `filemode=644`）、移除走 rpcd `file remove`；ACL 只逐字授权单一路径 + 所需方法；投放目录随包创建、`/lib/upgrade/keep.d` 保 sysupgrade 后保留。**验收 15/15**（VM 预览台 Playwright：初始/上传/跨页探测/替换/移除 + 服务端 200/JPEG 魔数 + mode=644 + 无 JS 异常），证据 `dev/shots/vm-wallpaper/`，工具 `dev/wallpaper-e2e.py`。全程**零主题改动**（机制与探测路径原样复用）。

## 11. TDD 同步清单（待写入两处副本，需用户/架构确认）

1. §8 待决事项「UI 隔离」✅ 已写回（2026-10-12）：移入已决策项并附实测依据指针。
2. §3.3 / §8：新增「主题双模」决策条目与 DESIGN.md 指针。
3. §4.4 运行时资源：壁纸投放目录（`/www/quartz-wallpaper/`，用户文件、不随包升级；W2.9 起由配置包创建目录、`/lib/upgrade/keep.d/quartz-wallpaper` 保 sysupgrade 后保留、ACL 单路径授权）——**是否登记进 §4.4 清单待用户/架构确认**（当前定位为「用户数据」而非「运行时下载资源」）。
4. 外观配置包（§5.1）：主题配置的 uci 归属（`/etc/config/quartz`，section `theme`）与 `luci-app-quartz-config` 包建议在 TDD 中登记。

## 12. 未决与验证项

- **Logo**：方向 B「切面宝石」暂定（2026-10-12，用户选定）并已入库——结构为六边形外廓 + 顶部切面折线 + 中轴（顶点网格 2/4/12/20/22，stroke 1.6），四处同源：`logo.svg`（favicon）、`header.ut`（侧边栏内联）、`view/quartz/sysauth.js`（登录页内联）、预览页。候选页（A–D）保留在 `dev/logo-candidates.html` 供日后回看；如需再迭代在此方向上细化。
- **机制核实结果（原"检出后验证"项，已通过基座源码核实并落入实现）**：`luci.mk` 收集并安装 `ucode/` 至 `/usr/share/ucode/luci`；模板解析 = `themes/$(basename mediaurlbase)/*`；双模 = `html[data-darkmode]`；主题注册 = `luci.themes.*` uci 项。**已在锁定固件上逐项复核通过（W2.7，2026-10-14）**：模板落点 `/usr/share/ucode/luci/template/themes/quartz/*.ut`、`luci.themes.Quartz` 注册、`data-qz-*` 首绘前生效均实测。
- **可选增强（后置）**：**短页粘底布局**（内容 < 视口时页底 chrome 目前落在内容末尾、其下留页面背景；要挪到视口底需 `body` flex 粘底布局，会牵动登录页与 Dashboard，故列为候选而非本轮实施）、强调色可换（macOS 本身允许）、跨设备主题持久化（uci）、**壁纸层缓存失效的彻底方案**（把文件 mtime 经 `header.ut` 的 ucode `stat()` 注入为 `data-qz-wallpaper-ver` / `--qz-wallpaper-url`，取代固定 URL；现状见 §4 缓存语义）、**暗色下实时图表白底**（LuCI 上游 `view/status/*` 内联 `background:#fff` + 为白底设计的轴颜色；如需主题内覆盖需连同轴/网格线着色一起处理，评估后再定）。
- **已完成（原后置项）**：~~壁纸上传 UI~~ → W2.9（见 §5.1；壁纸上传/替换/移除已在配置包落地）。
- **测试工具（dev/vm-test/）**：VM 逐页截图套件（Playwright + 页面清单 + run-log）；续测/回归直接 `python3 shoot.py --mode dark|light`，说明见该目录 README。

---

*变更记录：2026-10-12 v1.0 —— 依据用户定案（参照 macOS Vibrancy / 系统字体 / 双模 / 噪点 + 自备壁纸 / preview 评审）成文。*
*2026-10-14 v1.1 —— W2.7 收尾：默认主题与模板/注册/资源机制在锁定固件上逐项复核通过，WP-2 全量关闭（见 §10 实现状态）。*
*2026-10-14 v1.2 —— W2.9 壁纸管理面：上传/替换/移除落地配置包（§5.1），§4 补约束与缓存语义，§12 勾销"壁纸上传 UI"后置项。*
*2026-10-14 v1.3 —— W2.10 R1 用户实测反馈轮（三条现象中的两条属主题）：①**`panel_opacity` 语义扩为全界面**——`--qz-chrome` 改为 `rgba(var(--qz-chrome-rgb), calc(var(--qz-panel-alpha) * var(--qz-chrome-base-alpha)))`（§2.1 token 表 / §5.1），侧边栏/顶栏/页面操作条/登录卡随面板同步变透，`α=1` 时与旧值逐字节等价；②**修 `header.ut` ucode 整除**——`panel_opacity / 100` 两侧皆 int 时为整除，`50/100 → 0` 使 `--qz-panel-alpha: 0`（面板全透明），改 `/ 100.0`（坑位 #48）；③配置页选项文案随语义更新（"内容面板不透明度"→"界面不透明度"，zh 同步）。VM 热更复验：50% → 面板 .5 / chrome .36，100% → 面板 1 / chrome .72（默认观感零变化）。*
*2026-10-14 v1.4 —— W2.11（R1 反馈轮补充，用户实测「页脚版本行透明、与上方半透明操作条不协调」）：**页脚改为页底 chrome**——`--qz-chrome` + `blur()`，版本行不再直接压在壁纸/噪点上（`--qz-text-3` 在杂背景上可读性不足），且与贴底操作条连成一条 chrome 面；有操作条时用 `body:has(.cbi-page-actions) footer { border-top: none }` 抑制页脚描边（分隔线由操作条上边框承担），无操作条（只读视图/仪表盘）时保留 1px 描边；`@supports`/`prefers-reduced-transparency` 兜底列表同步纳入 `footer`。padding `0 24px 24px` → `8px 24px 10px`（成条后文字垂直居中）。§2.1/§3 同步。*

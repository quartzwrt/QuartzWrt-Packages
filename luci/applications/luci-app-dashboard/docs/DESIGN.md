# luci-app-dashboard 设计规范（讨论稿）

**版本**: v1.3（**定案，W3.1–W3.8 已落地并在固件内验收**）
**日期**: 2026-10-12（v1.3：2026-10-14）
**状态**: 六项决策用户拍板（§10）；契约冻结 [`AGGREGATE-API.md`](AGGREGATE-API.md)；**固件内验收通过**（QEMU 真启动，证据 `dev/shots/vm/`）；**W3.8 R1 用户实测反馈轮**（只读页操作条抑制 + 中文命名去撞名，证据 `dev/shots/vm-r1/`）。
历史：v0.2 扩展模型与联动 → v0.3 设计预览页 → v0.4 布局配对 → v1.0 定案 → v1.1 W3.2–W3.4 → v1.2 固件集成验收（W3.5/W3.6）+ 打包坑位 → v1.3 W3.8 R1 反馈轮。
**依据**: TDD §3.3 / §8（`QuartzWrt-Builder/docs/TDD.md`，两副本同步）；WP-2 `luci-theme-quartz/docs/DESIGN.md`（token / 层序 / 容器约定）；
**跨 WP**: §3 聚合契约冻结后为 **WP-4（`quartz-daemon`）的实现依据**；WP-3 开发不阻塞于 WP-4（mock 先行，见 §8）。

---

## 0. 定位与范围

`luci-app-dashboard` 是 QuartzWrt 的 Web 概览页：**登录后默认首页**，客户端一次取数渲染整页（TDD §3.3），数据全部来自 WP-4 聚合 API。

- **依赖**：WP-2（token 单一来源、CSS 层序、容器约定——已就绪）；WP-4（数据源——未开工，本稿冻结契约 + mock 并行）。
- **边界（v1 只读）**：只展示、不写系统。重启服务、触发插件核心更新、壁纸上传等写操作后置（走 WP-5 的 rpcd API 体系，权限边界最小化；接口 seam 见 §6.3-5）。
- **不做**：不做通用图表库、不做前端路由库、不做第三方页面嵌入（iframe）、不做运行时插件机制（后置评估，见 §6.4）。
- **扩展方针**：**数据契约开放、卡片层注册表化、入口层深链化**——新增内容不重写页面（§6）。

## 1. 落地机制（基座源码已核实）

### 1.1 登录后默认首页

LuCI 25.12 的落地解析在 `dispatcher.uc`：路径解析到无 action 的节点（如 `admin`）时，`resolve_firstchild` 取 **order 最小**的子节点（`node_weight`，认证节点 +10000 排后）；menu.d 里的 `preferred` 字段在本版本**未被实现/使用**。

基座 `admin` 一级项 order 现状（feeds 全量扫描）：

| order | 节点 | 来源 | 本固件状态 |
| :--- | :--- | :--- | :--- |
| 1 | `admin/dashboard` | 基座 luci feed `luci-mod-dashboard` | **未选装**（seed.config 未选，.config = not set） |
| 9 | `admin/olsr` | luci-app-olsr | 未装 |
| 10 | `admin/status` | luci-base（当前实际落地页 = status/overview） | 在装 |

**方案**：本包注册 `admin/dashboard`（order 1），成为登录落地页与侧边栏首项；`luci-mod-dashboard` 由基座 feed 提供但未选装，为防用户后续 `apk add` 造成同一路由双定义，**声明 `CONFLICTS:=luci-mod-dashboard`**（apk 体系支持 Conflicts 字段，`include/package-pack.mk` 已核实）。备选：不抢默认首页、仅作侧边栏项——与需求"登录后默认首页"不符，不推荐。

### 1.2 视图形态与加载约束（重要，影响 §4 选型）

- 形态：**LuCI JS view 壳**（`htdocs/luci-static/resources/view/quartz/dashboard.js`，路径与主题既有 `view/quartz/` 命名空间一致）+ 构建产物。页面渲染在主题 chrome（侧边栏/顶栏）内，自动获得双模与材质。
- **加载器约束（luci.js 源码核实）**：LuCI 的资源加载 = XHR 拉源码 → 正则扫描 `'require x'` 伪语句 → `eval` 成工厂函数执行。因此：
  1. **构建产物（ESM/IIFE bundle）不能走 LuCI 的 `require` 体系**；
  2. 由壳层以 `<script src=…>`（IIFE 产物）或原生 `import()`（ESM 产物、绝对 URL）注入加载；
  3. 产物内**不得引用 LuCI 内部模块**（`fs`/`poll`/`ui` 等），依赖一律通过壳层注入的**宿主适配器**（§2.3）。
- 静态资源 URL 用 `L.resource()` 拼接，追加 `?v=<包版本>` 破缓存（升级后浏览器立即拿到新产物）。
- 主题内容区 `#maincontent` 无宽度上限（`.container` 类在主题中已无规则），Dashboard 直接使用全宽内容区；移动端边距由主题处理。

### 1.3 菜单与权限

```json
// root/usr/share/luci/menu.d/luci-app-dashboard.json
{ "admin/dashboard": { "title": "Dashboard", "order": 1,
    "action": { "type": "view", "path": "quartz/dashboard" },
    "depends": { "acl": [ "luci-app-dashboard-index" ] } } }
```

- **中文名 = 仪表盘**（`po/zh_Hans/qdashboard.po`，2026-10-14 用户实测反馈定案）：**不得译作"概览"**——基座 `luci-mod-status` 的 `admin/status/overview` 已占用"概览"（base.po `msgid "Overview"`），撞名会让侧边栏同时出现两个"概览"（本页是落地页、基座页在 状态 分组下），用户无法从菜单区分；英文标题保持 `Dashboard`。
- **只读页无操作条（2026-10-14）**：壳层视图置空 `handleSave` / `handleSaveApply` / `handleReset`——LuCI 对每个 JS 视图无条件调用 `addFooter()`，三个 `handle*` 非空（基类默认实现）即渲染"保存并应用 / 保存 / 复位"，对只读页无意义（§0 边界）。基座只读视图同款写法（`view/status/index.js` 等）。

- ACL 组 `luci-app-dashboard-index`：只读 `ubus.file.read` 限定 `/var/run/quartz/status.json`（写法沿用基座 luci-mod-status 的路径级授权模式）＋所需只读 ubus（`system.board` 等）。
- root 会话不受限；非 root 用户需被授予该组（标准 LuCI 行为）。

## 2. 数据通路（建议案）

### 2.1 主通路：daemon 快照 + rpcd 文件读取

```
quartz-daemon (WP-4)                    浏览器（登录会话）
  采集周期 2s  ──原子写──▶  /var/run/quartz/status.json
                                          │  fs.read（ubus file.read，会话 cookie 鉴权 + 路径级 ACL）
                                          ▼
                                  壳层适配器 parse → 渲染
```

- **一次请求渲染整页**：整页数据 = 单个快照文件的一次读取，无逐卡片请求。
- **零新增暴露面**：不新开监听端口、不需要 CORS/token 下发；复用 LuCI 会话与 rpcd ACL（`file` 对象为 rpcd 内置）。
- **体积安全**：契约（§3）把 payload 限制在 ≤32KB，远低于 ubus 消息限制。
- **与 App 侧解耦**：WP-5/App 走 daemon 自己的 HTTP/JSON（TDD §3.4），两条消费路径互不影响。
- 备选（不推荐 v1）：rpcd ucode 插件代理 daemon HTTP——多一跳、需在 ucode 侧实现 HTTP 获取，无增益；若将来需要"读时实时采集"再评估。

### 2.2 降级与容错（验收必测路径）

| 场景 | 表现 |
| :--- | :--- |
| 文件不存在（daemon 未装/未启动） | 整页骨架 + "采集器未运行"封面卡（含重启提示），5s 自动重试 |
| 文件存在但某节为 null（如未装 Docker） | 仅该卡片显示"不可用/未安装"，其余正常 |
| 文件损坏（parse 失败） | 同"未运行"态 + 错误提示（不白屏） |
| daemon 侧采集部分失败 | 由 `errors[]` 携带，卡片角落显示弱化提示 |

### 2.3 宿主适配器（壳层 → 应用）

壳层向应用 `mount(el, host)` 注入固定接口，产物的数据/翻译/生命周期/入口全部经此，保证"产物不依赖 LuCI 内部模块"：

| 接口 | 实现（生产） | 实现（dev/预览） |
| :--- | :--- | :--- |
| `host.readStatus()` | `fs.read('/var/run/quartz/status.json')` → JSON.parse | 读 `dev/mock/status.json`（可选时变数据） |
| `host.poll(fn, sec)` | LuCI `poll.add`（**页面隐藏时不取数由壳层包裹实现**——源码实测 LuCI `Poll` 不做可见性判断；恢复可见时立即补取一次） | `setInterval` + Page Visibility 模拟 |
| `host.t(str)` | LuCI `_()`（页面已加载 translations） | 原样返回（或内置 zh 表） |
| `host.hasPage(path)` | `ui.menu.load()` 菜单树遍历（基座既有 API，主题 `menu-quartz.js` 同源用法） | mock 菜单表 |
| `host.url(path)` | `L.build_url` / 相对拼接 | '#dev-' + path |
| `host.version` | 包版本（缓存破坏/诊断） | 'dev' |

适配器为**版本化接口**：v1 只读三件套（readStatus/poll/t）；将来写操作（上传/动作）以 `host.v2` 增量扩展，不破坏 v1 契约（§6.3-5）。

轮询策略：默认 **5s**（v1 常量；将来如需可入 `/etc/config/quartz-dashboard`，归 WP-3 所有，不占用 WP-2 的 `/etc/config/quartz`）；`document.hidden` 时不取数——**由壳层包装回调实现**（2026-10-12 源码核实：LuCI `Poll` 只做 `setInterval`，不做可见性判断），恢复可见时立即补取一次避免陈旧数据。

## 3. 聚合 API 契约（**已冻结** → [`AGGREGATE-API.md`](AGGREGATE-API.md)）

**契约已独立成文并冻结**（2026-10-12，用户拍板）。完整字段表、单位、可空性、上限（整文件 ≤32KB / `clients.items` ≤32 / `net.series` ≤60 点）、完整示例、生产者（WP-4）与消费者（WP-3/WP-5）义务、变更流程（加法优先，破坏性升 `v`），一律以 `docs/AGGREGATE-API.md` 为准——本稿不再重复正文，避免两处漂移。

**通道摘要**：文件 `/var/run/quartz/status.json`（daemon 原子写 tmp+rename；Dashboard 经 rpcd `file.read` 读取）+ HTTP `GET /api/v1/status`（同一 payload，供 WP-5/App）。基准样例：`dev/mock/status.json`（正常）与 `dev/mock/status.partial.json`（部分不可用）。**增长方式见 §6.2。**


## 4. 前端栈与构建策略（**已定案：案 A**）

**定案（2026-10-12，用户拍板）**：**案 A = Vue 3 + Vite**，产物策略 = **提交产物 + CI 重建校验**（固件构建与 CI 构建保持零 node 依赖）。下表 B/C 保留为记录——若后续出现硬约束变化（体积/工具链），按本表回退评估。

**共同前提**：Tailwind v4 已由 WP-2 spike 定案（`qz:` 前缀 + `source(none)` + 主题入层 + preflight 中和，实测零污染），且 Tailwind 构建需要 CLI 步骤（standalone 二进制即可，无需 npm）——因此"零构建"最多是"零 JS 构建"。

| 案 | 栈 | 产物体积（量级，W3.2 实测冻结） | 优点 | 代价 |
| :--- | :--- | :--- | :--- | :--- |
| **A** | **Vue 3 + Vite**（IIFE 单文件产物） | 框架 ~25–35KB gzip，总量 ≤50KB gzip | TDD 原文方向（"Vue/React SPA"、Phase 1 "Vite + Tailwind 脚手架"）；组件化 + 注册表（§5.3），后续加卡成本低 | 开发侧引入 node 工具链；**产物与源码漂移**需校验门禁（见下） |
| B | Preact + htm 或 Svelte（同类工艺） | ~10–15KB gzip | 体积优 | 生态/熟悉度次之，收益有限 |
| C | 零构建 vanilla ESM（LuCI 原生惯用法：`E()`/`poll`/`ui`） | <10KB gzip | 无 JS 工具链、无漂移风险、最贴近 LuCI 生态 | 交互/卡片数量增长后组织成本高（注册表可缓解但仍需手写渲染）；需同步 TDD 措辞 |

**（定案）案 A 的理由**：TDD 已表述 Vue/Vite 方向；Dashboard 是"会继续长"的交互页面（§6 扩展模型即为其设计），组件 + 注册表是长期可维护性最省的路径。构建链成本可被纪律消解：

- **产物策略（定案）**：`src/` 源码 + 构建产物（`app.js`/`app.css`）**提交入库**；包 Makefile 只装文件——**固件构建与 CI 构建保持零 node 依赖**（符合 TDD §7 可重现原则）。`dev/build.sh` 一键重建并断言体积预算；CI 增加廉价 job：`npm ci && vite build && git diff --exit-code`（防漂移门禁）。
- 集成：Vite 以 `formats:['iife']` 输出单文件，暴露 `window.QuartzDashboard.mount(el, host)`；壳层 `<script>` 注入（§1.2）。
- **依赖预算（可维护性护栏）**：运行时依赖只允许"框架 + Tailwind"两类；新增运行时依赖须在提交说明中给出理由与体积账（§6.5-1）。
- **构建坑位（W3.2/固件集成已踩）**：
  1. Vite **lib 模式不替换 `process.env.NODE_ENV`**，必须在 `define` 显式定义为 `'production'`——否则浏览器端 `process` 未定义，产物加载即抛错、页面空白（实测）；
  2. luci.mk 会把 `htdocs/` 下的 **CSS 交给 CSSTidy 1.3**（`CONFIG_LUCI_CSSTIDY`）：不识别 `@layer`/现代属性，实测解析告警——故 CSS/JS 资产一律走 **`root/www/` 原样安装**（主题包同款先例）；
  3. luci.mk 视包内 **`src/` 目录为 C 源码目录**（要求 `src/Makefile` 且 install 阶段调用 `make install`）——前端源码目录命名 `frontend/` 规避（实测 `No rule to make target 'install'`）；
  4. luci.mk 的 i18n 包名 = `luci-i18n-$(LUCI_BASENAME)-<lang>`，basename 由包名剥前缀而来——`luci-app-dashboard` 与基座 `luci-mod-dashboard` 都剥成 `dashboard` → **kconfig 符号重名**（连带 lmo 文件名 `dashboard.zh_Hans.lmo` 冲突），实测导致 `quartz-defaults` 依赖解析失败被 defconfig 关闭。规避：`LUCI_BASENAME:=qdashboard` + po 文件名 `qdashboard.po`（用户拍板命名）。
- **工具链**：pnpm 经 corepack 固定版本（`packageManager: pnpm@10.15.0`），无需全局安装；`dev/build.sh` 首次运行自动 `corepack pnpm install`。
- **体积增长路径（预留）**：卡片增多致总量超预算时，切换为 ESM 产物 + 壳层原生 `import()` 分包（壳层加载方式无需重构，卡片模块边界即分包边界，§7）。
- 本机现状：node v20.19.2 可用、npm 缺失（`corepack enable` 或 apt 补装即可）；仅开发侧需要。
- Tailwind 入口（沿用 spike 定案）：`@import "tailwindcss" prefix(qz) source(none);` + `@source "./src/**/*.{vue,js}";`，`@theme` 把色板/圆角/字体映射到 `var(--qz-*)`（token 单一来源，命名重叠按 spike 结论由层序处理）。

## 5. 页面构成与视觉

### 5.1 布局（内容区全宽；12 列栅格，占位按卡声明）

```
┌─────────────────────────────────────────────────────────────┐
│ Hero（通栏）：衬线 hostname + 型号/版本/内核 · 右：uptime/WAN/采集时刻 │
├──────────────────────────┬──────────────────────────────────┤
│ 系统资源 (4)              │ 网络吞吐 (8)                      │
│ 负载/CPU/内存/交换/存储/温度│ WAN + 实时上下行 + 60 点 sparkline  │
├──────────────────────────┴──────────────────────────────────┤
│ 蜂窝 (12，cellular.available && modems 有值；否则整卡隐藏)：      │
│ 信号（质量条+四指标+RSRP 曲线）/ 服务小区 / 拨号 / 流量与 SIM      │
├──────────────────────────┬──────────────────────────────────┤
│ Tailscale (4)            │ 服务与核心 (8)                    │
│ 状态/地址/MagicDNS/对端   │ OpenClash / Docker / Tailscale /  │
│                          │ Bandix / QModem / daemon          │
├──────────────────────────┴──────────────────────────────────┤
│ 流量明细 (12，bandix；available=false 时整卡隐藏)：在线设备 + WAN 合计 │
│ + 连接/DNS 摘要 + Top≤8 设备表（设备/下行/上行/占比）              │
├─────────────────────────────────────────────────────────────┤
│ 在线设备 (12)：数量 + 表格（主机名/IP/MAC/接口/最近活跃，前 8 台） │
└─────────────────────────────────────────────────────────────┘
```

- **排布原则（预览实测确定）**：同一行内卡片的内容高度应相近——**矮卡配矮卡**（Tailscale 与"服务与核心"同行，两条大卡各占一行），行内按最高卡对齐、行尾不留大片空白；**不采用 masonry**（CSS 原生 masonry 未标准化，JS 重排对其余场景是负债）。行序 = 注册表顺序，占位由每卡声明（§5.3 `span`）——新增卡时按此原则选行配对。**Docker 信息并入服务卡**（运行/总数/镜像/不健康），不再单卡（与 IA v2 dashboard v2 对齐）。
- 断点：宽屏（≥1120px）如上图；中屏（<1120px）2 卡/行；手机（<700px）单列，顺序为 `#cellular 3 → #clients 4 → #bandix 5 → #tailscale 6 → #services 7`（蜂窝是 5G CPE 主 WAN，提前；设备列表与流量明细仍先于服务）。
- **移动端细则**：设备表窄屏隐藏 MAC 与接口列；卡片 `min-width: 0`（表格等最小内容宽度不得撑破页面）；Hero 摘要换行左对齐；悬浮控制条类元素左右贴边。
- 宽屏设备表用固定布局 + 列宽百分比（24/18/22/14/20），避免自动布局在超宽下列距失衡；超长内容省略号截断。
- 卡片 = 主题 `--qz-bg-panel` + 1px 描边 + 12px 圆角（复用 token，不另起色板）；半透明状态下的毛玻璃需主题提供通用面板类（见附 A）。

### 5.2 视觉与交互硬规则（全部沿用 WP-2 DESIGN.md §1）

- 禁渐变/发光/装饰性动画；过渡仅颜色 200ms；按压 = opacity .8。
- 彩色只用于文字高亮/状态色：状态圆点（ok/warn/err）+ 等宽数值；**不做彩色背景块**。
- 技术数值（IP/MAC/速率/负载）一律 `--qz-font-mono`；Hero hostname 用衬线（品牌时刻，与登录页一致）。
- sparkline：手写 inline SVG（`<path>`，≤60 点，stroke 1.6/1.8 与图标一致），**零图表库**、无动画。平滑口径（IA v2 R3）：**EMA(α=0.3) → 等距降采样（~13 点）→ Catmull-Rom 转三次贝塞尔 → round linecap/linejoin**；负值序列（蜂窝 RSRP）按 min/max + 15% 边距归一，不能按 0..max（否则曲线贴顶、面积铺满）。单序列（RSRP 历史）用 `values` 入口 + 可选淡色面积。
- 数值刷新不做跳动/滚动动画；异常状态（daemon 未运行、节不可用）用主题的告警样式，不用红色铺底。
- 空态/降级态是设计的一部分（§2.2 四态在 preview 页可切换评审）。

### 5.3 卡片注册表（内部架构，可扩展性的落点）

卡片**不是**写死在页面里，而是"一个卡一个模块 + 一份清单"：

```js
// src/cards/index.js —— 唯一的卡片清单（新增卡 = 加一个模块 + 加一行）
import SystemCard from '../components/cards/SystemCard.vue';
import NetworkCard from '../components/cards/NetworkCard.vue';
import QModemCard from '../components/cards/QModemCard.vue';
import TailscaleCard from '../components/cards/TailscaleCard.vue';
import ServicesCard from '../components/cards/ServicesCard.vue';
import BandixCard from '../components/cards/BandixCard.vue';
import ClientsCard from '../components/cards/ClientsCard.vue';

export default [
	{ id: 'system', title: 'System', span: 4, component: SystemCard },
	{ id: 'network', title: 'Network', span: 8, component: NetworkCard, link: 'admin/status/realtime' },
	{ id: 'cellular', title: 'Cellular', span: 12, component: QModemCard,
	  link: 'admin/modem/qmodem', visible: (p) => !!(p.cellular?.available && p.cellular.modems?.length) },
	{ id: 'tailscale', title: 'Tailscale', span: 4, component: TailscaleCard,
	  link: 'admin/vpn/tailscale', visible: (p) => !!p.services?.tailscale },
	{ id: 'services', title: 'Services & cores', span: 8, component: ServicesCard }, // 含 Docker 行
	{ id: 'bandix', title: 'Traffic detail', span: 12, component: BandixCard,
	  visible: (p) => p.bandix?.available === true },
	{ id: 'clients', title: 'Clients', span: 12, component: ClientsCard }
];
```
> 条件卡（Docker 已并入服务卡；蜂窝/Tailscale/Bandix 仍是整卡隐藏语义）：`visible(payload)` 为 false 时不渲染，避免"0 值/空卡"误导。

```js
// src/cards/network.js —— 一张卡的完整声明（声明式优先，纯展示，不取数）
export default {
  id: 'network',                       // 稳定 id：锚点深链 (#network)、截图基线、i18n 命名
  section: 'net',                      // 取自快照的哪一节；null/缺节 → 走 empty 态
  order: 20,                           // 排序：单列（手机）顺序与基线排序；行配对按 §5.1 原则
  span: { sm: 12, md: 6, lg: 8 },      // 响应式栅格占位（宽屏 12 列；md 2 卡/行；sm 单列）
  title: () => _('Network'),
  link: 'admin/status/realtime',       // 可选深链（壳层做存在性判定，不存在则隐藏入口，§6.3-1）
  render: (net, ctx) => { /* primitives 组合；自定义组件为逃生舱 */ },
  empty: (ctx) => { /* 该卡降级态（不可用/未安装） */ }
};
```

- **primitives（集中维护，强制主题硬规则）**：`Stat`（数值+单位+状态点）、`KVList`、`Table`、`Sparkline`、`Badge`、`EmptyState`。多数新卡用 primitives 组合即可，无需碰框架细节。
- **硬规则：卡片不得自行取数**——不读 uci、不调 ubus、不访问插件自己的接口。需要新数据 → **先扩契约**（§6.2），再加卡；由此"一次请求渲染整页"（§2.1）与降级模型（§2.2）不被逐卡破坏。
- **错误隔离**：每卡渲染包裹错误边界（Vue `onErrorCaptured`），单卡异常只显示卡内错误 chip，整页存活——与 §2.2 降级哲学同源，也是"新卡不会拖垮首页"的保证。

## 6. 扩展模型与联动

### 6.1 三层扩展点（新增内容落在哪）

| 层 | 位置（所有者） | 新增动作 | 典型未来内容 |
| :--- | :--- | :--- | :--- |
| 数据层 | WP-4 daemon 采集器（一节一采集器） | 加采集器 + 契约加可选字段/新节（§6.2） | 温度/风扇、多网口、WiFi 客户端、UPS、月度流量、OTA 新版本、插件核心版本 |
| 卡片层 | WP-3 `src/cards/` 注册表（§5.3） | 加卡模块 + 注册表一行 + mock 变体 + 截图基线（§6.6） | 上述数据的新卡片、公告/引导卡 |
| 入口层 | 壳层深链 + 锚点 id | 声明 `link` / 锚点 | 卡片 → 既有 LuCI 页；外部 → `…/admin/dashboard#<card-id>` |

**典型场景走查**（验证"扩展不重写页面"）：

1. **同层小改**：OpenClash 核心版本与更新状态——WP-4 在 `services.openclash` 加 `core`/`core_update`（可选字段，不升版）；WP-3 服务卡展示 + "更新可用" badge + 深链 OpenClash 页。两侧各改一处。
2. **新增数据节**：WiFi 客户端（带无线机型）——WP-4 新增 `wifi` 节；**旧前端对未知节自动忽略、不崩**；WP-3 加 `wifi.js` 卡（未装 iwinfo 时该节为 null → 卡片自动隐藏）。
3. **跨 WP 链路**：DDNS 状态（WP-8 bridge）——bridge 暴露 ubus → WP-4 采集进 `services.ddns` → 卡片展示；Dashboard 全程不接触 ddns 插件内部（TDD §5 模式 B 在展示层的体现）。
4. **写操作（二期）**：壁纸上传（WP-2 DESIGN §12）——host 适配器 `v2` 加 action/upload 能力，权限收敛到 WP-5 rpcd 体系；卡片 `actions` 槽为预留。v1 不做。
5. **固件链路**：OTA 新版本提示（WP-9）——`system.upgrade` 字段 + Hero 上一枚 chip + 深链系统刷机页（存在性判定）。

### 6.2 契约增长规则（扩展的纪律）

- **加法优先**：新数据以**可选字段或新节**进入契约，不升 `v`；`services` 为开放映射，插件状态优先以"新增键"接入（§3）。
- **破坏性变更才升 `v`**（语义/类型变更、删除、必备性变化）。
- **双向安全网**：老前端**忽略未知字段/节**；新前端**对缺失节降级**。由此前端/后端可独立升级——这是多对话并行开发与长期维护的关键。
- **唯一入口**：契约变更只经冻结版 `AGGREGATE-API.md`（§3 末），双端（WP-3/WP-4）改版号并在 WORKPLAN 记录。

### 6.3 联动矩阵

| # | 联动 | 机制 | v1 |
| :--- | :--- | :--- | :--- |
| 1 | 卡片 → 既有 LuCI 详情页 | 卡片声明 `link`；壳层经 `host.hasPage()` 做**存在性判定**（`ui.menu.load()` 菜单树遍历，基座既有 API；ucode 模板侧同款思路是 `dispatcher.lookup`），不存在则不显示入口 → **永不产生死链/404** | ✅ |
| 2 | 外部 → 具体卡片（锚点深链） | 每卡稳定 `id`，支持 `…/admin/dashboard#docker` 定位；文档、App、跨对话记录可直达某卡 | ✅ |
| 3 | 跨 WP 数据联动（展示） | 链路 = 插件/bridge（WP-7/8）→ ubus → daemon 采集器（WP-4）→ 契约节 → 卡片（WP-3）；运行时资源清单（TDD §4.4）状态同路 | ✅（按 §6.1 场景走查） |
| 4 | 多端复用 | Dashboard 与 App 消费**同一契约**（TDD §3.1 能力下沉）；两侧互不阻塞 | ✅（契约即接口） |
| 5 | 写操作联动（二期） | `host.v2` 增量扩展（action/upload）+ 权限统一收敛 WP-5 rpcd API（JWT/ACL）；卡片 `actions` 槽预留 | ⬜ 预留 |
| 6 | 第三方运行时 widget（免改本仓库） | 见 §6.4 | ⬜ 后置 |

**深链落点示例**（全部运行时判定存在性）：网络卡 → 基座实时流量页 `admin/status/realtime`；Docker 卡 → `admin/services/dockerman/containers`；设备卡"查看全部" → bandix 页（在装时）否则 `admin/status/routes`；服务卡 → `admin/services/openclash`（旧式 luasrc 注册，路径运行时判定）；Hero 版本 → 系统刷机页 `admin/system/flash`。

### 6.4 第三方运行时 widget 机制（**已定案：v1 不做**，仅预留 seam）

**v1 不做**，理由：需要稳定且版本化的 widget ABI、样式与错误隔离、独立的体积与安全预算；当前无外部需求方（WP-7/8 的展示需求经 daemon 契约即可满足）；对单人维护是负债。

**预留 seam（只冻结形状，不写代码）**：widget = `mount(el, host, cfg)` 的异步模块；宿主强制错误边界、`host` 版本门、独立体积上限、只读默认、样式 scoped 约定。

**再评估触发条件**（满足其一）：① ≥2 个外部 WP 需要"纯展示"卡且不愿改 daemon；② 出现社区插件想投卡片；③ 契约已稳定 ≥1 个发布周期。

### 6.5 可维护性护栏

1. **依赖预算**：运行时依赖仅"框架 + Tailwind"（§4）；新增需理由与体积账。
2. **单一集成面**：Dashboard 永不直连插件（不读第三方配置/接口）——集成数量增长不增加前端耦合（§6.3-3 链路）。
3. **单卡隔离**：错误边界 + 每卡空态；一卡坏不坏整页（§5.3）。
4. **每卡 DoD**：注册表一行、mock 变体、双模截图基线、i18n 字符串、`link` 存在性检查、体积预算内（§6.6）。
5. **契约纪律**：加法优先 / 破坏性升 `v` / 双端记录 + 双向安全网（§6.2）。
6. **构建纪律**：产物提交 + `dev/build.sh` 体积断言 + CI 重建 diff 门禁（§4）；杜绝"改了源码没更产物"的隐性腐化。
7. **文档纪律**：新增卡片/新增数据节的流程文档化为 `docs/EXTENDING.md`（W3.7 交付）——后续对话（跨 WP 协作）照文档即可贡献，无需读全部源码。

### 6.6 新增一张卡片（流程，供后续 WP 与未来自己照抄）

1. 判定数据是否已在契约：无 → 先按 §6.2 扩契约（WP-4 侧加采集器/字段）；
2. `dev/mock/status.json` 补该卡变体（含不可用态）；
3. `src/cards/<id>.js` 实现（primitives 组合优先；自定义组件须复核 §5.2 硬规则）；
4. `src/cards/index.js` 注册一行；
5. `dev/build.sh`（体积断言）→ `dev/serve.sh` 截图（双模 × 正常/降级）；
6. po 补字符串；`link` 若声明，确认存在性判定生效；
7. 记录：动了契约则记契约版号，WORKPLAN 记一笔。

## 7. 性能预算

| 项 | 约束 |
| :--- | :--- |
| JS 产物 | ≤50KB gzip（含框架）、单文件、无 code-split（增长超预算时按 §4 切 ESM 分包） |
| CSS 产物 | ≤12KB gzip（Tailwind `source(none)` 精确扫描后） |
| 首屏静态请求 | +2（app.css / app.js，带 `?v=` 缓存破坏；其余复用主题已加载资源） |
| 数据请求 | 一次 `file.read`/5s，payload ≤32KB；页面隐藏时停轮询 |
| 渲染 | 首屏骨架立即出现（不等数据）；数据到达后单次渲染；无布局抖动 |
| 卡片增长 | 预算是**总量约束**：新增卡必须在预算内收纳（primitives 复用、避免引入图表/富文本类重组件） |
| 外部依赖 | 零 CDN / 零 webfont / 零外部请求（与主题同） |
| 验收基准 | x86 虚拟机 + R4S 级真机（加载/滚动/轮询 CPU 观察） |

## 8. dev/ 工作流与验证（不进安装包）

```
luci-app-dashboard/
├── Makefile                        # LUCI_DEPENDS:=+luci-base +quartz-daemon；CONFLICTS:=luci-mod-dashboard
├── docs/DESIGN.md                  # 本文件（v1.0 定案）；AGGREGATE-API.md（✅ 冻结 v1）；EXTENDING.md（W3.7）
├── frontend/                       # ✅ 前端源码（Vue 3 + Vite + Tailwind v4；node 工具链仅开发侧）
│   │                               #    目录名不可叫 src/：luci.mk 视 src/ 为 C 源码目录，
│   │                               #    要求 src/Makefile 并提供 install 目标（实测报 install 失败）
│   ├── pnpm-lock.yaml              # ✅ 提交（构建可复现；node_modules 由 .gitignore 排除）
│   └── src/{main.js,App.vue,components/,cards/,lib/,styles/}
├── dev/
│   ├── preview.html                # ✅ 设计预览（已落地）：静态渲染 mock + 四态 + 双模 + 锚点/深链示意
│   │                               #    ?theme=auto|dark|light  ?state=normal|down|partial|broken
│   │                               #    ?wallpaper=1  ?alpha=50..100  ?live=0（截图冻结）  ?debug=1（溢出诊断）
│   │                               #    ?app=1（加载真实构建产物 + mock 宿主适配器，产物级回归）
│   ├── build.sh                    # ✅ 构建：corepack pnpm 安装 + vite build + 体积断言 + 壳层 BUILD 回写
│   ├── serve.sh                    # ✅ 预览服务（根 = QuartzWrt-Packages/luci，端口 8898）
│   ├── probe.sh                    # ✅ 无头截图（Edge 经 WSL 互操作，沿用 WP-2 做法）
│   ├── vm-shot.py                  # ✅ 固件内验收（Playwright 登录 VM 内的 LuCI 并截图 + 采集 DOM/网络证据）
│   ├── check-freshness.sh          # ✅ 产物新鲜度门禁（重建 + git diff；CI 手动化后的本地版门禁）
│   ├── shots/                      # ✅ 视觉回归基线：dashboard-*（静态基准）/ app-*（真实产物）/ vm/（固件内）
│   └── mock/status.json            # ✅ 契约样例（联调基准）；status.partial.json（部分不可用变体）——与冻结契约同源
├── root/www/luci-static/resources/ # 资产走 root/ 原样安装：htdocs/ 下 CSS 会被构建期
│   │                               #    csstidy 1.x 处理（不识别 @layer，实测毁样式表；主题同款先例）
│   ├── view/quartz/dashboard.js    # LuCI 壳（手写，不构建）：加载产物 + 宿主适配器 + poll
│   └── quartz-dashboard/           # 构建产物（提交入库：app.js / app.css）
├── root/usr/share/luci/menu.d/…    # §1.3
├── root/usr/share/rpcd/acl.d/…     # §1.3
└── po/zh_Hans/dashboard.po         # 手工维护（沿用 quartz-config 模式）
```

**固件内验证流程（W3.6 实操，可复跑）**：

```sh
# ① 构建树 quartz feed 临时指向本地工作副本（src-link；下次 build-local.sh 会按锁定 SHA 重置）
cd $BUILD/immortalwrt
cp feeds.conf /tmp/feeds.conf.bak
sed -i 's|^src-git quartz .*|src-link quartz '"$REPO"'/QuartzWrt-Packages|' feeds.conf
rm -rf feeds/quartz package/feeds/quartz && ./scripts/feeds update quartz
./scripts/feeds install luci-app-dashboard luci-theme-quartz luci-app-quartz-config quartz-defaults
cp $REPO/QuartzWrt-Builder/config/seed.config .config && make defconfig   # 确认 CONFIG_PACKAGE_luci-app-dashboard=y
# ② 全量构建（WSL 必须净化 PATH，坑位 #13）
export PATH=$(echo $PATH | tr ':' '
' | grep -v '^/mnt/' | paste -sd:)
make -j16 V=w
# ③ 起 VM（宿主 8080）并验收
$REPO/QuartzWrt-Builder/scripts/test-in-vm.sh --detach
python3 dev/vm-shot.py --tag nodata                      # 降级态（无快照）
ssh -p 2222 root@127.0.0.1 "mkdir -p /var/run/quartz && cat > /var/run/quartz/status.json" < dev/mock/status.json
python3 dev/vm-shot.py --tag data --locale zh-CN         # 正常态 + 中文
# 验收后可 ssh 热更 /www/luci-static/... 快速复验（提交前以源码为准）
```

> **⚠ src-link 验证的两条纪律（2026-10-14 实测，坑位 #49）**：
> ①**先清旧包**——`luci.mk` 的版本戳来自 `git log -1 -- <包目录>`：改动未提交时戳不前进，src-link 全历史下新包的戳会**低于**上一轮（浅克隆 feed tip）的旧包，包解析取最高版本 → `bin/packages/<arch>/quartz/` 里的旧 apk **静默胜出**，镜像装旧文件而构建零报错。验证前 `rm -f bin/packages/<arch>/quartz/<包名>-*.apk`（同族全清）或先提交。
> ②**镜像是最终判据**——`debugfs -R "dump <路径> /tmp/x"`（对 `ext4-rootfs.img.gz` 解出的 rootfs 镜像）逐字节比对源码；注意 `?v=` 缓存破坏是安装期 sed（`luci.mk`）改写的模板引用，模板与源码不逐字节一致属正常。

**验证路径**：

1. ✅ 设计预览（`dev/preview.html`；`dev/serve.sh` 起 8898）：双模 × 数据四态 × 锚点/深链示意已可评审，截图基线在 `dev/shots/`（窄屏截图受 Windows Edge 无头最小窗口宽度限制，CSS 视口 ≈477px 起；真机手机宽度留待固件内验收）；
2. 本地全量构建（WP-1 `build-local.sh`）→ QEMU 起 x86 镜像 → 登录见默认首页（daemon 未装时同时验证"采集器未运行"降级态）；
3. WP-4 出首版后：真数据联调（契约对齐核对）、R4S 级设备实测加载流畅性。

## 9. W3.x 交付分解

| 序号 | 内容 | 依赖 |
| :--- | :--- | :--- |
| W3.0 | ✅ 本设计稿定案（v1.0）+ 聚合契约冻结（`AGGREGATE-API.md`） | ~~用户拍板~~ 已完成 2026-10-12 |
| W3.1 | 🚧 包骨架（Makefile/menu.d/acl.d/po）+ LuCI 壳（含 `host` 适配器）+ mock 数据 + 降级态（以 `dev/preview.html` 为视觉基准） | W3.0 |
| W3.2 | ✅ 构建链落地（2026-10-12）：pnpm 经 corepack 固定 `10.15.0`；Vite lib/IIFE 单文件 + Tailwind v4 入口；`dev/build.sh` 含体积断言与壳层 BUILD 回写——实测 app.js **28.6KB gzip** / app.css **2.4KB gzip** | W3.1 |
| W3.3 | ✅ 数据层（2026-10-12）：宿主适配器两实现（生产壳层 / 预览 harness）；轮询可见性包装、四态降级、单卡错误边界 | W3.1 |
| W3.4 | ✅ 卡片注册表 + primitives + 五卡 + Hero（2026-10-12）：真实产物在预览 harness（`?app=1`）渲染通过，与静态基准一致 | W3.2、W3.3 |
| W3.5 | ✅ 视觉打磨（2026-10-12）：预览/产物/固件三档截图基线齐全；固件内修正 KV 数值换行（主题 `.cbi-progressbar{min-width:170px}` 与卡片竞争宽度，卡片内覆盖为可收缩） | W3.4 |
| W3.6 | ✅ 固件集成验收（2026-10-12）：本地全量构建 → `scripts/test-in-vm.sh` QEMU 真启动 → Playwright 登录验收（`dev/vm-shot.py`）——**登录落地页即 Dashboard**；快照经 rpcd `file.read`+ACL 读取渲染（hero+5 卡+8 行设备表）；三条深链存在性判定通过；i18n 中文生效；无数据时降级横幅正常。证据 `dev/shots/vm/`（run-{nodata,data}.json + 截图）。真数据联调待 WP-4 | W3.5 |
| W3.7 | ✅ 扩展机制沉淀（2026-10-12）：`docs/EXTENDING.md` 成文（新增数据节/卡片/深链流程 + 卡片 DoD）；深链存在性检查由壳层 `host.hasPage()` 实现并在固件内验证 | W3.4 |
| W3.8 | ✅ R1 用户实测反馈轮（2026-10-14）：①**只读页操作条抑制**——壳层置空 `handleSave`/`handleSaveApply`/`handleReset`（LuCI `addFooter()` 对每个视图无条件调用，基类默认实现非空→只读页长出"保存并应用/保存/复位"）；②**中文命名去撞名**——`Dashboard` 译名"概览"→"**仪表盘**"（"概览"归基座 `admin/status/overview`，同站点同名两入口不可区分）。证据 `dev/shots/vm-r1/`（`before/after50/after100` 双模 + `run-*.json`：`pageActionBtns` 由 `["保存","重置"]` → `null`、文档标题 `| 仪表盘`、侧栏"概览"计数 2→1） | W3.6 |

W3.1–W3.5、W3.7 全程可在 WP-4 缺位下推进（mock 先行）。

**验收标准**（对齐 WORKPLAN §3 WP-3）：① 固件中实际渲染通过；② 弱设备（R4S 级）加载流畅（§7 预算达标）；③ 四态降级路径可复现（§2.2）；④ 主题原生 cbi 页面不受 Dashboard 产物污染（沿用 WP-2 的对比方法复测一次）；⑤ 扩展流程可复跑——按 §6.6 新增一张示例卡不改动既有卡片代码。

## 10. 决策记录（已拍板，2026-10-12）

| # | 决策 | 定案 | 备注 |
| :--- | :--- | :--- | :--- |
| 1 | 前端栈 | **案 A：Vue 3 + Vite**（IIFE 单文件产物） | 见 §4；B/C 保留为回退评估记录 |
| 2 | 产物策略 | **提交产物 + CI 重建校验门禁** | 固件构建零 node 依赖（§4） |
| 3 | 默认首页与冲突 | **注册 `admin/dashboard` order 1 + `CONFLICTS:=luci-mod-dashboard`** | 基座同名包未选装，CONFLICTS 兜底（§1.1） |
| 4 | 数据通路 | **daemon 快照 + rpcd `file.read`**（非 rpcd 插件代理 HTTP） | 零新增暴露面、复用会话 ACL（§2.1） |
| 5 | 契约冻结 | **即冻结** `AGGREGATE-API.md`，WP-4 按此实现 | 已冻结（§3），解耦两个 WP 并行开发 |
| 6 | 第三方运行时 widget | **v1 不做，仅冻结 seam 形状** | 再评估触发条件见 §6.4 |

## 附 A. 对既有文件的改动清单（所有权）

| 文件 | 改动 | 归属/约定 |
| :--- | :--- | :--- |
| `meta/quartz-defaults/Makefile` | 追加 `+luci-app-dashboard` | 共享文件，追加式（WORKPLAN §4） |
| WP-2 `cascade.css` | 增补**通用面板类 `.qz-panel`**（bg-panel + 1px 描边 + 圆角，并纳入 `html[data-qz-translucent]` 毛玻璃选择器），供 Dashboard 与第三方视图复用 | 跨 WP（对话 B 内），需在 WORKPLAN §4 登记；未落地前 Dashboard 自持一条材质规则（仅引用 token，色板仍单一来源，预览页即此做法） |
| TDD §3.3 / §8 | 补 Dashboard 落地机制与契约指针（拍板后） | 两副本同步，需用户确认（WP-2 §11 同类待办一并处理） |
| WORKPLAN | WP-3 状态与快照回写 | 本对话已执行 |

## 附 B. 与 WP-2 / 基座的接口（已就绪，无待办）

- token 单一来源：`--qz-*`（Tailwind `@theme` 映射，不另起色板）；
- 层序：`theme < base < qz-theme < components < utilities`（主题已入层）；
- 容器：渲染在 `#maincontent` 内，只消费 token，不改写主题样式；preflight 中和规则主题已内置；
- 联动接口：`ui.menu.load()` 菜单树（存在性判定，JS 侧）与 ucode 模板侧 `dispatcher.lookup`（同款思路）、锚点约定 `#<card-id>`。

---

*变更记录：*
*v0.1 2026-10-12 —— 首稿：落地机制经基座源码核实（默认首页解析 / 加载器约束 / apk CONFLICTS）、数据通路建议案、聚合契约草案、栈选型三案对比、W3.x 分解。*
*v0.2 2026-10-12 —— 增补扩展模型与联动：三层扩展点与场景走查（§6.1）、契约增长规则（§6.2）、联动矩阵与深链落点（§6.3）、第三方运行时 widget 后置与 seam（§6.4）、可维护性护栏（§6.5）、新增卡片流程（§6.6）、卡片注册表（§5.3）；W3.7 交付项；待拍板增至 6 项。*
*v0.3 2026-10-12 —— 设计预览页落地（`dev/preview.html` + `serve.sh`/`probe.sh` + 六张截图基线）：静态 mock 渲染、双模 × 数据四态、锚点/深链示意、溢出诊断；§5.1 布局与移动端细则按预览实测修订；登记 WP-2 通用面板类 `.qz-panel` 接口项（附 A）。*
*v0.4 2026-10-12 —— 布局优化（用户评审反馈"卡片大小不同留白"）：确立**矮卡配矮卡**排布原则（Docker + 服务与核心同行；设备表改全宽 12 列承载，增"接口"列、前 8 台）；系统卡补"交换"行；宽屏设备表固定布局 + 列宽百分比；移动端单列顺序按 `order`（设备列表提前）；注册表示例同步（§5.3）。*
*v1.0 2026-10-12 —— **定案**：用户拍板六项（记录见 §10）；聚合契约冻结为 [`AGGREGATE-API.md`](AGGREGATE-API.md)（§3 改为指针，避免两处漂移；`collect_ms` 提升为顶层字段）；`dev/mock/status.json` 与 `status.partial.json` 落地并与预览页同源（预览改为读取 mock 文件）；勘误 LuCI `Poll` 不做可见性判断（改由壳层实现，§2.3）；W3.0 ✅、W3.1 开工。*
*v1.1 2026-10-12 —— **W3.2–W3.4 落地**：构建链（corepack/pnpm 固定、Vite lib IIFE、Tailwind v4 入口、`dev/build.sh` 体积门禁与 BUILD 回写；产物 app.js 28.6KB gzip / app.css 2.4KB gzip）、数据层（宿主适配器生产/预览两实现、四态降级、单卡错误边界）、卡片实现（注册表 + primitives + 五卡 + Hero）；预览页新增 `?app=1` 真实产物模式并出 `shots/app-*.png` 基线；踩坑记录：Vite lib 模式不替换 `process.env.NODE_ENV`（§4）；W3.5 🚧。*
*v1.2 2026-10-12 —— W3.5/W3.6 固件集成验收（登录落地页即 Dashboard、真数据联调、四态降级、深链存在性、i18n）+ 打包三坑（i18n 撞名 `LUCI_BASENAME:=qdashboard` / 源码目录不得为 `src/` / CSS 走 `root/www/` 绕开 csstidy），W3.7 扩展手册成文。*
*v1.3 2026-10-14 —— **W3.8 R1 用户实测反馈轮**（用户实测三条现象，两条归本包）：①只读页"保存并应用"操作条抑制（`handle*` 置空，§1.3）；②中文名"概览"→"仪表盘"（与基座 `status/overview` 撞名，§1.3）。第三条（主题 chrome 不跟随 `panel_opacity`）归 WP-2，见其 DESIGN v1.3 / 坑位 #48。VM 热更复验 + 包级核验（新 lmo 词条、壳层字节）证据 `dev/shots/vm-r1/`。*
*v1.4 2026-10-17 —— **WP-8 P2（bandix 流量明细卡，契约 §3.7 加法）**：新增 `BandixCard.vue`（在线设备/WAN 合计/连接与 DNS 摘要/Top≤8 表）+ 注册表一行（span 12，`available!==true` 整卡隐藏）；`app.css` 增列宽类与移动端 order（clients 4 → bandix 5 → services 6）；mock/po/产物/基线同步（`dev/shots/*` 重出）；preview.html 同步（补 Bandix 服务行 + 流量明细卡；预览页样式无层，规则成对改，见 §服务卡 注）。数据面 = WP-4 daemon `bandix` 节（慢刷 10s 经 `quartz.bandix`）。*
*v1.5 2026-10-19 —— **IA v2 dashboard 卡补齐（WP-3）**：①新增 `QModemCard.vue`（span 12 四栏：信号〔质量条+四指标+RSRP 曲线〕/ 服务小区〔PCC/NSA 的 ARFCN·PCI·带宽·SCS〕/ 拨号〔状态·IPv4 打码·速率·USB 模式〕/ 流量与 SIM〔本月·重置计划·SIM·温度〕；`cellular.available && modems` 才显示；数据面 = daemon `cellular` 10s 慢车道节 §3.9）；②新增 `TailscaleCard.vue`（span 4：运行态/版本、tailnet 地址打码、MagicDNS、对端在线、后端状态；`services.tailscale` 存在才显示；探针 extras 由 WP-10 的 `tailscale_probe` 提供）；③**Docker 卡并入服务卡**（运行/总数/镜像/不健康），服务卡改为 Tailscale 同行 span 8；④`primitives.js` Sparkline 改 EMA(α=0.3)+等距降采样(~13)+Catmull-Rom `<path>`+round 端点，新增 `values` 单序列与负值域；⑤mock/po/产物/`app-*` 基线同步（Bundle 32.0KB gzip / CSS 2.8KB gzip）。字段口径：蜂窝只用快照已有字段（APN/DNS/MTU/短信存储属页面级读，v1 不上卡）。*

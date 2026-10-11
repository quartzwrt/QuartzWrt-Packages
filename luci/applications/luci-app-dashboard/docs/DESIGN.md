# luci-app-dashboard 设计规范（讨论稿）

**版本**: v1.0（**定案**）
**日期**: 2026-10-12
**状态**: 六项决策已由用户拍板（记录见 §10）；聚合契约已冻结为 [`AGGREGATE-API.md`](AGGREGATE-API.md)；W3.1 开工。
历史：v0.2 扩展模型与联动（§5.3、§6）→ v0.3 设计预览页（`dev/preview.html`）→ v0.4 布局按内容高度配对（§5.1）→ v1.0 定案。
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
├──────────────────────────┼──────────────────────────────────┤
│ Docker (4)               │ 服务与核心 (8)                    │
│ 运行/总数/镜像/不健康      │ OpenClash / dockerd / quartz-daemon │
├──────────────────────────┴──────────────────────────────────┤
│ 在线设备 (12)：数量 + 表格（主机名/IP/MAC/接口/最近活跃，前 8 台） │
└─────────────────────────────────────────────────────────────┘
```

- **排布原则（预览实测确定）**：同一行内卡片的内容高度应相近——**矮卡配矮卡**（Docker 与"服务与核心"同行，两条大卡各占一行），行内按最高卡对齐、行尾不留大片空白；**不采用 masonry**（CSS 原生 masonry 未标准化，JS 重排对其余场景是负债）。行序 = 注册表顺序，占位由每卡声明（§5.3 `span`）——新增卡时按此原则选行配对。
- 断点：宽屏（≥1120px）如上图；中屏（<1120px）2 卡/行；手机（<700px）单列，顺序按注册表 `order`（设备列表提前到"服务与核心"之前）。
- **移动端细则**：设备表窄屏隐藏 MAC 与接口列；卡片 `min-width: 0`（表格等最小内容宽度不得撑破页面）；Hero 摘要换行左对齐；悬浮控制条类元素左右贴边。
- 宽屏设备表用固定布局 + 列宽百分比（24/18/22/14/20），避免自动布局在超宽下列距失衡；超长内容省略号截断。
- 卡片 = 主题 `--qz-bg-panel` + 1px 描边 + 12px 圆角（复用 token，不另起色板）；半透明状态下的毛玻璃需主题提供通用面板类（见附 A）。

### 5.2 视觉与交互硬规则（全部沿用 WP-2 DESIGN.md §1）

- 禁渐变/发光/装饰性动画；过渡仅颜色 200ms；按压 = opacity .8。
- 彩色只用于文字高亮/状态色：状态圆点（ok/warn/err）+ 等宽数值；**不做彩色背景块**。
- 技术数值（IP/MAC/速率/负载）一律 `--qz-font-mono`；Hero hostname 用衬线（品牌时刻，与登录页一致）。
- sparkline：手写 inline SVG（`polyline`，≤60 点，stroke 1.6 与图标一致，accent 色），**零图表库**、无动画。
- 数值刷新不做跳动/滚动动画；异常状态（daemon 未运行、节不可用）用主题的告警样式，不用红色铺底。
- 空态/降级态是设计的一部分（§2.2 四态在 preview 页可切换评审）。

### 5.3 卡片注册表（内部架构，可扩展性的落点）

卡片**不是**写死在页面里，而是"一个卡一个模块 + 一份清单"：

```js
// src/cards/index.js —— 唯一的卡片清单（新增卡 = 加一个模块 + 加一行）
import system  from './system.js';
import network from './network.js';
import docker  from './docker.js';
import clients from './clients.js';
import services from './services.js';
export default [ system, network, docker, clients, services ];
```

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
├── src/                            # 源码（案例 A 时）；node 工具链仅开发侧
├── dev/
│   ├── preview.html                # ✅ 设计预览（已落地）：静态渲染 mock + 四态 + 双模 + 锚点/深链示意
│   │                               #    ?theme=auto|dark|light  ?state=normal|down|partial|broken
│   │                               #    ?wallpaper=1  ?alpha=50..100  ?live=0（截图冻结）  ?debug=1（溢出诊断）
│   ├── serve.sh                    # ✅ 预览服务（根 = QuartzWrt-Packages/luci，端口 8898）
│   ├── probe.sh                    # ✅ 无头截图（Edge 经 WSL 互操作，沿用 WP-2 做法）
│   ├── shots/                      # ✅ 视觉回归基线：dashboard-{dark,light,partial,down,wallpaper,mobile}.png
│   ├── mock/status.json            # ✅ 契约样例（联调基准）；status.partial.json（部分不可用变体）——与冻结契约同源
│   └── build.sh                    # ⬜ 重建产物 → 同步 htdocs + 断言体积预算
├── htdocs/luci-static/resources/
│   ├── view/quartz/dashboard.js    # LuCI 壳（手写，不构建）：加载产物 + 宿主适配器 + poll
│   └── quartz-dashboard/           # 构建产物（提交入库）
├── root/usr/share/luci/menu.d/…    # §1.3
├── root/usr/share/rpcd/acl.d/…     # §1.3
└── po/zh_Hans/dashboard.po         # 手工维护（沿用 quartz-config 模式）
```

**验证路径**：

1. ✅ 设计预览（`dev/preview.html`；`dev/serve.sh` 起 8898）：双模 × 数据四态 × 锚点/深链示意已可评审，截图基线在 `dev/shots/`（窄屏截图受 Windows Edge 无头最小窗口宽度限制，CSS 视口 ≈477px 起；真机手机宽度留待固件内验收）；
2. 本地全量构建（WP-1 `build-local.sh`）→ QEMU 起 x86 镜像 → 登录见默认首页（daemon 未装时同时验证"采集器未运行"降级态）；
3. WP-4 出首版后：真数据联调（契约对齐核对）、R4S 级设备实测加载流畅性。

## 9. W3.x 交付分解

| 序号 | 内容 | 依赖 |
| :--- | :--- | :--- |
| W3.0 | ✅ 本设计稿定案（v1.0）+ 聚合契约冻结（`AGGREGATE-API.md`） | ~~用户拍板~~ 已完成 2026-10-12 |
| W3.1 | 🚧 包骨架（Makefile/menu.d/acl.d/po）+ LuCI 壳（含 `host` 适配器）+ mock 数据 + 降级态（以 `dev/preview.html` 为视觉基准） | W3.0 |
| W3.2 | 构建链（Vite+Tailwind 入口、`dev/build.sh`、体积断言、产物提交） | W3.1 |
| W3.3 | 数据层（适配器两实现 + 轮询/降级/容错） | W3.1 |
| W3.4 | 卡片注册表 + primitives + 首卡（Hero/系统/网络/Docker/设备/服务） | W3.2、W3.3 |
| W3.5 | 视觉打磨（双模 × 正常/降级 × 移动端；截图回归基线） | W3.4 |
| W3.6 | 固件集成验收（本地构建 + QEMU；WP-4 首版后真数据联调） | W3.5、（WP-4） |
| W3.7 | 扩展机制沉淀：`docs/EXTENDING.md`（§6.6 流程成文）+ 深链存在性检查 | W3.4 |

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

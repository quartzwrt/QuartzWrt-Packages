# 扩展手册：新增数据节 / 新增卡片 / 新增深链

**适用**: 后续 WP（WP-4 采集器、WP-7/8 插件桥接）与本仓库未来的维护者
**依据**: `docs/DESIGN.md` §5.3（卡片注册表）、§6（扩展模型）、§6.2（契约增长）、§6.6（流程）；`docs/AGGREGATE-API.md`（冻结契约 v1）
**一句话原则**: **先扩数据契约、再加卡片**——卡片永不自行取数（不读 uci、不调 ubus、不直连插件）。

---

## 0. 决策：这一步要改哪一层

| 想做的事 | 落点 | 改动量 |
| :--- | :--- | :--- |
| 展示**已有契约**里但还没展示的数据 | 只加卡片（§1） | 1 个卡组件 + 注册表 1 行 |
| 展示**契约里没有**的数据 | 先扩契约（§2）再加卡片 | WP-4 加采集器/字段 + 卡片 |
| 卡片标题右侧加"跳转入口" | 只加深链声明（§3） | 注册表 1 行加 `link`/`linkLabel` |
| 现有卡片内容微调（增加一行/换种展示） | 直接改该卡组件 | 改 1 个文件 |

## 1. 新增一张卡片（WP-3 侧）

以新增「WI-FI 客户端」卡为例：

1. **确认数据在契约中**：查 `docs/AGGREGATE-API.md` §3；若没有 → 先做 §2。
2. **建卡组件** `frontend/src/components/cards/WifiCard.vue`：
   - 只读 props：`payload`（整份快照）与 `host`（翻译/深链）；用 `computed` 取自己那一节（**不要**在 setup 里一次性解构 `props.payload.x`，轮询刷新不会更新）；
   - 一律用 primitives 组合：`Stat / KV / Badge / Dot / Sparkline / EmptyState / ErrorChip`（`frontend/src/components/primitives.js`）；自定义组件是逃生舱，须复核 DESIGN §5.2 视觉硬规则（禁渐变/发光/装饰动画、彩色仅文字与状态、圆角 ≤12px…）；
   - 文案一律走 `t('English msgid')`（`t = (s) => host.t(s)`）；
   - 该节为 `null` 或空 → 给 `EmptyState`（不可用/未安装语义）；不要把"不可用"画成错误色块。
3. **注册**：在 `frontend/src/cards/index.js` 加一行：
   ```js
   { id: 'wifi', title: 'Wi-Fi', span: 4, component: WifiCard,
     link: 'admin/network/wireless', linkLabel: 'Wireless' }
   ```
   - `span`（12 列栅格宽屏占位）按 **"矮卡配矮卡"** 原则选：同行的卡片内容高度应相近（DESIGN §5.1）；新增卡若与现有卡高度差大，考虑调整行配对而不是拉高等高；
   - `visible: (p) => …` 可选：返回 false 则整卡不渲染（例：Docker 卡在 `available === false` 时隐藏）；
   - `link` 只写 LuCI 页路径，**存在性判定与 URL 由壳层负责**（不存在则自动不显示入口，不会死链）。
4. **补样例数据**：`dev/mock/status.json`（正常态）与 `dev/mock/status.partial.json`（不可用态）各补该节——它们是 WP-4 的联调基准，也是预览页与截图的数据源。
5. **补翻译**：`po/zh_Hans/qdashboard.po` 增加新 msgid（文件名为 i18n 域，**不要**改回 `dashboard.po`，会与基座 luci-mod-dashboard 冲突，见 Makefile 注释）。
6. **构建与回归**：
   ```sh
   dev/build.sh            # 构建 + 体积断言（超预算即失败）+ 壳层 BUILD 回写
   dev/check-freshness.sh  # 提交前：确认产物与源码同源
   dev/serve.sh            # 另开：8898 预览
   # 浏览器打开 …/dev/preview.html?app=1  用右下角切四态核对
   dev/probe.sh            # 需要时更新 shots/ 基线截图
   ```
   卡片若涉及**固件内**行为（新深链目标、新数据节），再跑一次固件级验收：
   ```sh
   QuartzWrt-Builder/scripts/test-in-vm.sh --detach   # QEMU 真启动（宿主 8080）
   python3 dev/vm-shot.py --tag data --locale zh-CN   # 截图 + DOM/网络证据 → shots/vm/
   ```
7. **记录**：动了契约 → 在 `AGGREGATE-API.md` 记版号；WORKPLAN §1/§7 记一笔。

### 卡片 DoD（Definition of Done）

- [ ] 数据来自契约节，组件内**无**任何取数/写操作
- [ ] 节为 null/空 → EmptyState；叶片 null → `—`（等宽）
- [ ] 文案全部 `t()`；po 已补
- [ ] mock 正常/降级两态各有一份数据
- [ ] `dev/build.sh` 体积断言通过（总量预算：JS ≤50KB gzip / CSS ≤12KB gzip）
- [ ] 双模（深/浅）× 正常/降级 × 窄屏（≤699px）截图核对
- [ ] 若声明 `link`：目标页存在时显示、不存在时不显示（假路径试一次）

## 2. 新增/扩展数据节（WP-4 侧，契约协同）

1. 读 `docs/AGGREGATE-API.md` §6（生产者义务）与 §0 变更流程：**加法优先**——加可选字段或整节不升 `v`；改语义/类型/删除/必备性变化才升 `v`。
2. WP-4 在采集器侧新增一节（一节一采集器）；**单节失败不中断**：该节置 `null` + `errors[]` 记一条。
3. 遵守上限：整文件 ≤32KB、`clients.items` ≤32、`series` ≤60 点；新节自守其字段级上限。
4. 更新 `dev/mock/status.json` + `status.partial.json`（两侧仓库都可见的基准样例），并在契约文档追加字段表行 + 示例片段。
5. 双端改版号记录（WP-3/WP-4），WORKPLAN 记一笔。**老前端对未知字段/节必须忽略**（现有实现已满足），因此新节可以先上 daemon、后上卡片。

## 3. 新增深链（卡片 → 既有 LuCI 页）

1. 注册表对应卡加 `link: '<luci 路径>'`（如 `admin/status/realtime`）+ `linkLabel: '<msgid>'`。
2. 确认目标页在固件里可能不存在的情况：壳层 `host.hasPage()` 会遍历菜单树判定，不存在 → 入口自动隐藏（**不要**自己写条件判断）。
3. po 补 `linkLabel` 文案。

## 4. 明确不做（避免走弯路）

- 卡片内直接 `fetch`/`ubus`/读文件——破坏"一次请求渲染整页"与降级模型；
- 引入图表库/富文本/图标字体——体积预算与视觉硬规则不允许（sparkline 手写 SVG 即范式）；
- 为单张卡改主题全局样式——主题样式归 WP-2，卡片只用 token 与 primitives；
- 运行时第三方 widget——已定案 v1 不做（DESIGN §6.4，触发条件见该节）。

---

*建立：2026-10-12（W3.7）；伴随实现：`frontend/src/cards/index.js`、`frontend/src/components/primitives.js`、`dev/build.sh`。*

# quartz-bandix-bridge 设计（WP-8 · Bridge 适配层首个落地）

**版本**: v1.0（**定案并开工**：2026-10-17 用户「按推荐来吧」→ D1–D6 全按推荐；W8.2–W8.5 已落地，见 §12）
**日期**: 2026-10-17
**归属**: WP-8（`QuartzWrt-Packages` → `bridge/quartz-bandix-bridge/`，本 WP 自有路径）
**上游**: `timsaya/bandix`（核心，Apache-2.0）+ `quartzwrt/openwrt-bandix`、`quartzwrt/luci-app-bandix`（受控 Fork，WP-7 收录，锁 tag）

**关联文档**
- TDD `newproject.md` §5 模式 B（Bridge 适配层）/ §3.2 quartz-defaults / §4.4 运行时资源
- WORKPLAN §2（WP-8 状态）/ §4（`bridge/` 所有权）
- `QuartzWrt-ThirdParty/docs/DESIGN.md` §3.1（bandix 收录与 TDD §6 清单核对）
- `luci-app-dashboard/docs/AGGREGATE-API.md` v1（快照契约）/ `DESIGN.md` §5.3、§6.1、§6.3
- `quartz-app-api/docs/DESIGN.md` v1.1 §5（动作白名单）/ `quartz-daemon/docs/DESIGN.md` §3.6、§13.1（探针）

---

## 0. 定位与范围

**一句话**：把 bandix 以「**契约层**」接入——网页端继续用上游 LuCI 四页（不动），App/Dashboard 只与 `quartz.bandix` 这一稳定 ubus 契约交互，读同一份数据、走同一套动作。

**bandix 的模式判定（WP-7 已给结论，本设计承接）**：bandix 是**模式 A 近亲**——核心 `bandix` 常驻 procd，暴露本地 HTTP API（`127.0.0.1:8686`）；上游 LuCI 包已自带一层 rpcd 薄壳 `luci.bandix`（§1.2 实测）。

因此本 Bridge **不做"从零封装"**，只做三件事：

| # | 职责 | 具体 |
| :--- | :--- | :--- |
| ① | **统一契约** | 稳定方法名/入参/返回形态/单位；抹平上游"部分方法 unwrap `$.data`、部分原样透传"的不一致（§1.2） |
| ② | **收敛动作面** | 白名单子集，**不暴露** `installUpdate`（GitHub 下载 + `apk add --allow-untrusted`，供应链敏感）、`clearData`（破坏性）等（§3.3） |
| ③ | **入参校验** | TDD §5 硬要求：全部入参结构校验 + 字符集/范围白名单，**不拼接 shell**（§4） |

**本次范围（v1）**：bandix 一个 bridge 包 + 默认开启 + Dashboard 服务行/深链 + 跨 WP 登记（App 富化页与流量卡为 P2/P3，见 §6/§7）。
**明确不做**：重新实现 bandix 统计逻辑；修改上游核心；App 内嵌 LuCI 网页（D1）；App 内编辑 bandix 全部配置（`port`/`iface`/`data_dir` 等留网页，§3.3）。

---

## 1. 事实核实（2026-10-17，本地锁定版本实测）

### 1.1 三件套与版本（现状，勿重复劳动）

| 组件 | 仓库 | 版本 | 形态 |
| :--- | :--- | :--- | :--- |
| 核心 | `timsaya/bandix`（**不 fork、不引入**） | **0.12.11** | Release 预编译 musl 二进制，逐 arch SHA256 固定在 `openwrt-bandix` Makefile |
| 打包层 | `quartzwrt/openwrt-bandix`（Fork 镜像 `net/bandix/`） | 0.12.11-r1 | pkg `bandix`：`/usr/bin/bandix` + `/etc/init.d/bandix` + `/etc/config/bandix` + `keep.d` |
| LuCI 前端 | `quartzwrt/luci-app-bandix`（Fork 镜像 `net/luci-app-bandix/`） | **0.12.12** | pkg `luci-app-bandix`：4 页 JS + rpcd 薄壳 `luci.bandix` + menu/acl/po/status |

本地实测（x86_64 构建产物）：`bandix --version` → `bandix 0.12.11`；`-V/--version` 均支持（探针可用，WP-7 已用）。

### 1.2 上游 rpcd 薄壳 `luci.bandix`（本 Bridge 的上游，§2.2）

- 位置 `/usr/libexec/rpcd/luci.bandix`（约 1900 行 shell），`list` 分支声明 **32 个方法**；
- 数据全部来自核心 HTTP API：`http://127.0.0.1:$(uci get bandix.general.port || echo 8686)/api/...`（16 个端点常量）；
- **入参两形态**：stdin JSON（对象 `$.mac` 或数组 `$[0]`）与位置参数 `$3..$9` 都接受——所以用 `ubus call` 传对象参数即可，兼容；
- **返回形态不一致**（这是我们抹平的对象）：`getStatus`/`getMetrics`/`getTrafficUsageRanking`/`getTrafficUsageIncrements` 返回 **unwrap 后的 `$.data`**；`getConnection`/`getConnectionFlows`/`getDnsQueries`/`getDnsStats`/`getScheduleLimits`/`getRateLimitWhitelist`/`getTrafficQuotas` 返回**原样包封 `{status,data}`**；
- **上游 ACL 与实现已漂移**：`acl.d/luci-app-bandix.json` 列了 `getMetricsDay/Week/Month`、`getRateLimitWhitelistEnabled`，而脚本 `list` 未实现前三者 → **上游 luci 面本身是不稳定面**，这正是 Bridge 存在的技术理由（§2.2 逃生舱）；
- 上游 ACL 把读写**都绑在 `luci-app-bandix` 组**（LuCI 会话），写面含 `installUpdate`（`wget` GitHub release → `apk add --allow-untrusted`，无签名校验）与 `clearData`。

### 1.3 核心 HTTP API（不可在宿主复现，原因已实测）

- 端点（16，来自上游脚本常量 + 上游 README 核对）：`/api/traffic/devices|metrics|metrics/{day,week,month}|limits|limits/schedule|quotas|bindings|usage/ranking|usage/increments|rate_limit/whitelist|rate_limit/whitelist/enabled|rate_limit/default`、`/api/connection/devices|flows`、`/api/dns/queries|stats`、`/api/version`；
- 绑定 **127.0.0.1**（release 模式；Debug 模式才 0.0.0.0）+ uci 端口可改；**无鉴权**（回环信任模型）→ 本机谁都能读，我们的收敛点因此放在 rpcd 契约层（§4）；
- **本机复现失败（如实记录）**：宿主 uid 1000 无 CAP_BPF → 启动即 `Failed to load eBPF program: map error: failed to create map IPV4_SUBNET_INFO` 退出（web 服务在模块初始化之后才 listen）。故**字段形状以「上游脚本注释 + 前端消费」为准，VM 实测定稿**（§9 验收 2/3）。

### 1.4 "默认开启"缺口（本 WP 要补的唯一缺口）

- `files/bandix.config` 三个模块**全部默认 0**（`traffic`/`connections`/`dns`），且 `bandix.init` 在三者全关时 `return 1` → **出厂状态服务根本不启动**；
- 已核 `base-files` `/lib/functions.sh` 的 `default_postinst`：装包时自动对包内 init 脚本 `enable`（镜像构建期只建 rc.d 符号链接）+ 运行时 `enable` 并 `start` → **init 符号链接不需要我们管**；
- ⇒ 缺口只有一个：**uci 开关**。一个 `uci-defaults` 即可补（§5.2）。

### 1.5 探针与快照（**WP-7 已交付，本 WP 不重复**）

`quartz-runtime-resources`（thirdparty）已投放 `probes.d/bandix.json`：

```json
{ "id": "bandix", "title": "Bandix",
  "running": { "type": "procd", "name": "bandix" },
  "extras": { "core": { "type": "exec", "path": "/usr/bin/bandix",
                        "args": ["--version"], "regex": "bandix ([0-9.]+)", "cache": 300 } } }
```

⇒ 快照 `services.bandix = {running, core}` **现成可用**（daemon 零改动，§13.1 drop-in 机制）。Bridge 只需在其上加**契约面**。

### 1.6 前端消费面（4 页 → 上游方法，Bridge 的对齐基准）

| 页面 | 上游方法 |
| :--- | :--- |
| 状态（设备表 + 速率 + 用量排行 + 图表） | `getStatus`、`getMetrics`、`getTrafficUsageRanking`、`getTrafficUsageIncrements`、`setHostname`、`deleteDevice`、`getScheduleLimits`、`setScheduleLimit`、`updateScheduleLimit`、`deleteScheduleLimit`、`getVersion`、`checkUpdate`、`getRateLimitWhitelist`、`setRateLimitWhitelistEnabled`、`addRateLimitWhitelist`、`deleteRateLimitWhitelist`、`setDefaultRateLimit`、`getTrafficQuotas`、`setTrafficQuota`、`deleteTrafficQuota` |
| 连接 | `getConnection`、`getConnectionFlows` |
| DNS | `getDnsQueries`、`getDnsStats` |
| 设置 | uci 表单（`/etc/config/bandix` 全字段）+ `clearData`、`restartService`、`getVersion`、`checkUpdate`、`installUpdate` |

字段/单位（上游语义，Bridge 契约沿用）：`t_rx_b/t_tx_b` 字节数、`t_rx_r/t_tx_r` **Byte/s**、`w_*`=WAN、`l_*`=LAN、`last`=epoch **ms**；限速/配额入参单位 **Byte/s**（前端按 KB/s=1024 换算显示，§3 契约同）。

### 1.7 前置与代价（与 WP-7 D4 同一验收项）

- eBPF/BTF 支持：x86_64 与 aarch64（RK3399）**双目标实测列入验收**；
- **flow offloading 干扰**：上游 README 要求关硬件卸载；基座默认 `flow_offloading=1`（软件）+ `fullcone=1`，对 TC/eBPF 统计的实际影响 → VM A/B 实测（沿用 WP-7 D4 结论项）；
- 常驻开销：traffic 默认**内存态**（`traffic_enable_storage=0`，无磁盘增长）；`data_dir=/usr/share/bandix` 已随 `keep.d` 保留（sysupgrade 安全）；实测占用见 §9。

**真机结论（2026-10-17，FNet3399/RK3399，6.12.112，10-16 构建；工具 `dev/rk3399-device-check.sh`，证据 `dev/accept-rk3399-20261017-194225.log`）**：
- **eBPF 在 RK3399 内核可用**：`/sys/kernel/btf/vmlinux` **缺失**（无 BTF）但 eBPF 程序照常加载并经真流量计数（设备表 3 台、计数器随流量变动）——bandix 用**预编译 BPF 而非 CO-RE**，BTF 不是前置条件（TDD §6「BTF 支持」顾虑据此解除）；
- **软件 flow offloading 不遮蔽统计（A/B 实测）**：受控 LAN 客户端（quartzboat 18:c0:4d:af:a2:23）下载 10 MB——`flow_offloading=1`（现状）设备表 `w_rx_b` += 9,549,585 B（≈91%）；`=0` += 9,761,430 B（≈93%）；两轮均在 2% 内吻合（差额为计数器读时抖动），**无需为用户关闭软件卸载**；**硬件卸载 `flow_offloading_hw` 未设（关闭）**——上游 README 的"关硬件卸载"要求在本部署默认满足；
- **开销（RK3399，traffic-only）**：RSS 4.6 MB、CPU 0.1%（单核 30s 均值）——与 x86_64 同量级，D2「只开 traffic」的代价成立。

---

## 2. 架构与选型

### 2.1 链路（三条，收敛在 `luci.bandix`）

```
① 网页：  浏览器 ── LuCI 会话 ──▶ rpcd luci.bandix ──▶ http://127.0.0.1:8686（core 内存态）
② App：   App(JWT) ─443─▶ quartz-app-api ──(root)──▶ ubus quartz.bandix（Bridge）──▶ luci.bandix
③ 看板：   Dashboard ◀─ 快照 services.bandix（已有）/ P2 新节 bandix ── quartz-daemon
```

**一致性由"同一上游实现"保证**：①②最终都走 `luci.bandix` → 同一份 core 数据、同一套动作语义；③P2 经 Bridge 取数（唯一集成面），数值与①②抽样对照（§9 验收 2/3）。

### 2.2 关键选型：Bridge 的上游 = `luci.bandix`（**D1**）

| 方案 | 结论 | 理由 |
| :--- | :--- | :--- |
| **① 委托 `luci.bandix`（推荐）** | ✅ | 同一实现 ⇒ 一致性最强（用户核心诉求）；**零重复**（不复制 1900 行调用逻辑）；上游修复/新字段自动跟进（与"基座 feed 分支追踪、上游修复自动跟进"同一哲学）；对外契约仍由 Bridge 承担 ⇒ 隔离不丢 |
| ② 直连 core HTTP API | ✗ | 要自己复刻端点/字段映射并追 core API 演进（上游 README main 与 0.12.11 已现差异）；与网页端出现"两份实现"，反而**降低**一致性 |
| ③ App webview 内嵌 LuCI 页 | ✗ | 需把 LuCI 会话/凭据下沉给 App，与 WP-5 §2.3「认证分层」决议正面冲突；主题/壳层/i18n/降级/深链全部失控；与 App UI-DESIGN §4.4「webview 化插件页」在明确不做清单里的既定边界冲突 |

⇒ **"体验一致"的锚点在「数据源 + 动作语义 + 字段单位」三层，不在 HTML 层**（§7 展开）。
**逃生舱（Bridge 的隔离价值）**：Bridge 内部适配集中在单个 ucode 文件的一个转发函数；若上游 `luci.bandix` 重构到不可适配，替换为直连 core HTTP API 即可，**对外契约不变、App 无感**。

### 2.3 实现形态：rpcd **ucode 插件**（对象名 `quartz.bandix`）

- 机制已核实（锁定基座 rpcd 源码）：rpcd 扫描 **`/usr/share/rpcd/ucode/*.uc`**（`ucode.c`: `RPC_UCSCRIPT_DIRECTORY`），脚本返回 `{ '<对象名>': methods }`——**对象名取返回表的键**（与 exec 插件"文件名=对象名"不同）；基座自带同款实例：`/usr/share/rpcd/ucode/ddns.uc`（luci-app-ddns）；
- 为什么 ucode 而不是 shell / Go：JSON 原生（免 `jsonfilter` 拼串）、**零编译**（不引入 Go 工具链/CI 构建时长）、依赖极轻且基座可得（`ucode` + `ucode-mod-ubus` + `ucode-mod-uci`，后两者在基座多个包 DEPENDS 中出现，如 `wifi-scripts`）；shell 方案（照抄上游风格）在出参规范化与嵌套 JSON 上易错；Go（app-api 风格）对本场景过重；
- ubus 调用用 `ubus.call('luci.bandix', <method>, {...})`（ucode 内建，无 shell 介入）。

### 2.4 包结构

```
bridge/quartz-bandix-bridge/
├── Makefile                              # PKGARCH:=all（纯脚本包，无编译）
├── README.md                             # 用法/诊断（ubus call 示例）
├── docs/DESIGN.md                        # 本文档
├── files/
│   ├── usr/share/rpcd/ucode/quartz-bandix-bridge.uc   # 契约实现（唯一逻辑文件）
│   └── etc/uci-defaults/99-quartz-bandix               # 默认开启（§5.2）
└── dev/                                  # VM 验收脚本 + 证据（W8.6）
```

依赖：`DEPENDS:=+ucode +ucode-mod-ubus +ucode-mod-uci +bandix +luci-app-bandix`（后两者=上游实现与引擎，显式声明使"Bridge 半残"结构上不可能）。

---

## 3. 契约（`quartz.bandix` v1）

### 3.1 通则

- 入参：对象（缺失键=该参数空）；出参统一信封 **`{"ok":true,"api":1,"data":…}`** / **`{"ok":false,"api":1,"error":{"code","message"}}`**（`code` 机器可读，供 app-api 映射到 `action_failed.detail`；`message` 在 `data_unavailable` 时带上游原因，如"Connection monitoring is not enabled"，App 可直接展示）；
- 字段名**沿用上游**（`t_rx_b`/`w_rx_r`…）：不重命名，换取"网页与 App 逐字段对照可核"（一致性优先于美观）；单位随字段一起冻结（§1.6）；
- 方法名统一**下划线风格**（`limit_schedule_set`），与上游 rpcd 方法风格一致、避免点号在 ubus 工具链里的歧义；
- 只对外暴露 **读 10 + 写 10**，不追平上游 32 方法（单人维护成本纪律，§10 风险）。

### 3.2 读方法（v1）

| 契约方法 | 上游 | 入参（校验见 §4） | data 要点 |
| :--- | :--- | :--- | :--- |
| `status` | `getStatus` | `start_ms?`,`end_ms?` | `{d:[{mac,ip4,ip6[],host,conn,uplink,w_ch,last,t_rx_b…l_tx_r}]}`（设备表+速率，`last`=epoch ms） |
| `device_metrics` | `getMetrics` | `mac?` | `{retention_seconds,mac,metrics:[…]}`（1s 采样，前端画图同源） |
| `ranking` | `getTrafficUsageRanking` | `start_ms?`,`end_ms?`,`network_type?` | `{start,end,net,cnt,t_b,t_rx_b,t_tx_b,r:[{mac,ip4,host,r,pct,t_b,rx_b,tx_b}]}` |
| `series` | `getTrafficUsageIncrements` | `start_ms?`,`end_ms?`,`aggregation?`,`mac?`,`network_type?` | `{start,end,agg,net,mac,inc:[{…w_rx_avg/max/p90/p95/p99…}]}`（长区间图；区间上限见 §4） |
| `connections` | `getConnection` | — | `{cnt,last,g:{total,tcp,udp,tcp_est,tcp_tw,tcp_cw,last},d:[…]}`（已 unwrap） |
| `connection_flows` | `getConnectionFlows` | `ip?`,`protocol?`,`state?`,`page?`,`page_size?` | `{…flows, page/page_size}`（已 unwrap；分页上限 §4） |
| `dns_queries` | `getDnsQueries` | `domain?`,`device?`,`is_query?`,`dns_server?`,`query_type?`,`page?`,`page_size?` | 已 unwrap |
| `dns_stats` | `getDnsStats` | — | 已 unwrap |
| `limits` | `getScheduleLimits` + `getRateLimitWhitelist` + `getTrafficQuotas` | — | `{schedule:[…],whitelist:{…},quotas:{…}}`（三次上游读，一次返回——App 少往返） |
| `version` | `getVersion` + `list` 能力探测 | — | `{core,luci_app,bridge,upstream_methods:[…]}` |

- `version.upstream_methods` = `ubus call luci.bandix list` 的键集（缓存 300s）→ **能力探测**：上游改名/缺失时消费方可降级而非报错；
- **`checkUpdate` 不进 v1**（联网打 GitHub API，非确定性；"更新可用"状态属 TDD §4.4 治理面，随该清单落地再评估——与 `services.bandix.core` 版本读取已就位的现状不冲突）。

### 3.3 写方法（v1 白名单，10 个）

| 契约方法 | 上游 | 入参 |
| :--- | :--- | :--- |
| `limit_default_set` | `setDefaultRateLimit` | `wan_rx_rate_limit`,`wan_tx_rate_limit`（Byte/s） |
| `limit_schedule_set` | `setScheduleLimit` | `mac`,`start_time`,`end_time`,`days[]`,`wan_tx_rate_limit`,`wan_rx_rate_limit` |
| `limit_schedule_update` | `updateScheduleLimit` | `id` + 同上 |
| `limit_schedule_delete` | `deleteScheduleLimit` | `id` |
| `whitelist_add` / `whitelist_delete` | `addRateLimitWhitelist` / `deleteRateLimitWhitelist` | `mac` |
| `whitelist_enabled` | `setRateLimitWhitelistEnabled` | `enabled`（0/1） |
| `quota_set` / `quota_delete` | `setTrafficQuota` / `deleteTrafficQuota` | `mac`, 六个周期字节数（minute/hourly/daily/weekly/monthly/total） |
| `hostname_set` | `setHostname` | `mac`,`hostname`（空串=清除绑定） |

**明确不暴露**（决策 D4）：

| 上游方法 | 不暴露理由 |
| :--- | :--- |
| `installUpdate` | 从 GitHub release 下载并 `apk add --allow-untrusted`（**无签名校验**）——供应链面不进 App 通道；bandix 升级走既有通道（软件包面 `packages.*` 或 OTA，WP-9/WP-5） |
| `clearData` | 破坏性 + 无确认层；需要时 P2 复议（App 侧必须先有确认层） |
| `deleteDevice` | 删除设备历史数据，语义与 App 场景不符；P2 复议 |
| `restartService` | **不由 Bridge 提供**——走 WP-5 既有 `service.restart`（把 `bandix` 加入允许集，一行），避免双入口不一致（D5） |

---

## 4. 安全（TDD §5 硬要求落地）

- **无 shell**：全程 ucode 内存内 JSON + `ubus.call`，不存在拼接路径（上游 shell 实现的 `\s` 正则/`sed` 拼串风险面在我们这层不存在）；
- **入参校验表**（校验失败 → `{"ok":false,"error":{"code":"bad_request"}}`，不转发）：

| 参数 | 规则 |
| :--- | :--- |
| `mac` | `^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$`，归一化为小写 |
| `hostname` | `^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$` 或空串（清除） |
| 速率/配额（Byte/s 与字节数） | 整数，`0 … 2^40`（1 TiB），禁负/小数/科学计数 |
| `page` / `page_size` | 整数，`page ∈ 1..10000`、`page_size ∈ 1..100` |
| `start_ms` / `end_ms` | 整数（epoch ms），且 `end ≥ start`、区间 ≤ 90 天 |
| `days` | 整数数组，元素 ∈ 0..6（重建为 JSON 数组转发，不经字符串拼接） |
| `start_time` / `end_time` | `^([01][0-9]\|2[0-3]):[0-5][0-9]$` |
| `id` | `^[A-Za-z0-9_-]{1,64}$` |
| `protocol` | 枚举 `tcp`/`udp`/空 |
| `state` | 枚举 `ESTABLISHED|SYN_SENT|SYN_RECV|FIN_WAIT|FIN_WAIT2|TIME_WAIT|CLOSE_WAIT|LAST_ACK|CLOSE`/空（取自上游前端下拉） |
| `is_query` | `0`/`1`/空 |
| `domain` | `^[A-Za-z0-9._*-]{0,253}$`（禁空格/引号/控制字符） |
| `dns_server` / `query_type` | 字符集白名单（`^[A-Za-z0-9._:-]{0,64}$`），枚举随上游 API 在 VM 实测后收紧 |
| `aggregation` | 枚举 `1m`/`5m`/`1h`/`1d` |
| `network_type` | 枚举 `wan`/`lan`/`all` |

- **ACL：不投放 `acl.d`（默认拒绝）**。效果：经 rpcd 会话（LuCI/局域网 `/ubus`）**无法**调用 `quartz.bandix`；只有本机 root 侧（= `quartz-app-api` 以 root 经 rpcd 执行后转发的 `ubus call`）可调。
  *反例理由（为何不照抄 app-api 的 `unauthenticated` 授权）*：那对 app-api 成立是因为它自己内部做 JWT 认证；Bridge 没有自带认证，若授 `unauthenticated`，**局域网任何能到达 `/ubus` 的客户端都能改限速**。
  落地时以 VM 实测复核（正例：app-api 路径通；反例：LuCI 会话调用被拒），并写进 README 的诊断说明（SSH root `ubus call quartz.bandix status`）。
- **审计**：写动作落 logd 一行（`bandix-bridge action=… ok|fail dur_ms=…`，与 app-api 同款纪律）。
- **供应链**：不暴露 `installUpdate`；bandix 二进制仍由 thirdparty 包锁定 SHA256（现状不变）。

---

## 5. 预装与默认开启

### 5.1 预装（现状 + 本 WP 追加一行）

- **现状已预装**：`quartz-defaults` DEPENDS 已含 `+bandix +luci-app-bandix`（WP-7 D5，`meta/quartz-defaults/Makefile`）→ x86_64 默认种子与 `devices/*` 设备构建均预装；
- **本 WP 追加**：同一 Makefile 追加 **`+quartz-bandix-bridge`** 一行（遵守"各 WP 只追加自己的 DEPENDS 行、quartz 在前"约定，登记 + 通知 WP-1/2/3/4/5/6/7）；
- **边界（如实说明）**：通用目标种子 `config/seed-generic.config` **不带 `quartz-defaults`** → 那 94 个通用 target 构建**不预装** bandix（设计如此：含按架构预编译二进制，非全架构可得）。"设备预装"= x86_64 与设备适配层（如 `erya_fnet-3399`，aarch64 二进制齐备）。
- **断言（修正 v0.1 的判断）**：`scripts/check-manifest.sh` 无需改动——Gate B ② **现读 `quartz-defaults` 的 DEPENDS 生成断言**（脚本头部即写明"增删依赖无需改本脚本"），因此追加 `+quartz-bandix-bridge` 一行即自动获得 manifest 断言（含 bandix 本身）。v0.1 写"需增正断言"是误判，已更正。

### 5.2 默认开启：`/etc/uci-defaults/99-quartz-bandix`

```sh
#!/bin/sh
# 出厂默认开启 bandix 流量监控（WP-8）。只生效一次。
# 标记位放在 uci 内：/etc/config/bandix 是 conffile → 随 sysupgrade 保留，
# 防止"固件升级后 uci-defaults 回放"把用户手动关掉的开关又打开。
[ "$(uci -q get bandix.general.quartz_defaults)" = "1" ] && exit 0
uci set bandix.traffic.enabled='1'
uci set bandix.general.quartz_defaults='1'
uci commit bandix
/etc/init.d/bandix enable
```

**机理（已核实，逐条）**：
1. 首次启动 `S10boot` 运行 `/etc/uci-defaults/*`（`base-files` init `boot`：`START=10`，`uci_apply_defaults`）→ 早于 `S99bandix`，同一次启动内即生效；
2. 装包时 `default_postinst` 已自动 `enable` init 脚本 → 本脚本里的 `enable` 是**幂等加固**（防止未来某路径下 uci-defaults 先于符号链接建立）；
3. 标记位放在 `bandix.general` 而非 `/etc/quartz/…`：`/etc/config/bandix` 是 conffile，`sysupgrade` 必保留 → **升级后回放不会覆盖用户选择**（这是本设计要避开的坑，见 §10 风险 3）。

**默认开启哪些模块（D2）**：推荐**只开 traffic（流量监控）**；`connections`（3s 读全量 `nf_conntrack`）与 `dns`（RingBuf 常驻 + 最多 10000 条内存记录）默认关，开关留在同一份 uci —— 网页「设置」页与 App 看到的是**同一事实**（§7 一致性地基）。

---

## 6. Dashboard 显示（跨 WP，WP-3 侧）

### 6.1 P1（随 v1 交付，成本最小）

- **「服务与核心」卡加一行 Bandix**：状态点（`services.bandix.running`）+ 版本（`services.bandix.core`）+ 深链 `admin/network/bandix`（`host.hasPage()` 存在性判定机制已有）；
- 数据**已就绪**（WP-7 探针，§1.5），WP-3 只需一行横向行 + po 字符串 + mock 变体 + 截图基线（按 Dashboard DESIGN §6.6 流程）；
- 服务卡当前是**逐行硬编码**（`ServicesCard.vue` 的 openclash/dockerd/quartz-daemon 三行），**没有**未知键泛化兜底 → 这一行必须显式加（成本 = 一处模板 + 一条 i18n）。

### 6.2 P2（建议随后一轮）：真正的「流量明细」卡

- **数据面在 WP-4**（Tier 2 新节）：`bandix` 节（慢刷 **10s**，与既有慢刷节奏一致），经 **Bridge** 取数（`quartz.bandix` → 唯一集成面；daemon 不直连 8686，保持"插件细节只在 Bridge"的隔离）：

```json
"bandix": { "available": true, "devices": 12, "online": 9,
            "wan": { "rx_bps": 0, "tx_bps": 0 },
            "top": [ { "mac":"…","host":"…","ip":"…","rx_b":0,"tx_b":0,"pct":0 } ],
            "conn": { "total": 0, "tcp": 0, "udp": 0 },
            "dns": { "queries": 0 } }
```

- 上限自守：`top` ≤ 8（契约 §3 快照 ≤32KB 纪律）；`available=false`（未装/未开 traffic）→ 整卡隐藏（与 docker 卡同款 `visible` 谓词）；
- **卡片在 WP-3**：`src/cards/` 加 `bandix.js` + 注册表一行 + mock 变体 + 双模截图基线。
- 落地顺序：P1 先上（零依赖），P2 待"数据面（WP-4）+ 卡片（WP-3）"两侧各一处改动后合入。

---

## 7. App 与网页的一致性口径（回应用户诉求）

**"复用其 LuCI 前端 ⇒ 两端体验一致"落地为三个锚点**（不复制 HTML，复制事实）：

| 锚点 | 机制 |
| :--- | :--- |
| ① 同一数据源 | 网页与 App 都经 `luci.bandix` → 同一份 core 内存态；同一时刻抽样数值应逐字相等（**验收项**，§9-2） |
| ② 同一动作语义 + 同一状态 | 限速/白名单/主机名经同一上游方法；模块开关（traffic/connections/dns）在同一份 uci → App 里看到的开关状态与网页一致 |
| ③ 同一字段与单位 | Bridge 契约沿用上游字段名与单位（Byte/s、字节、epoch ms），App 只做显示换算（KB/s=1024） |

**App 侧交付梯度**（对齐 App UI-DESIGN §4.1 成本阶梯，D6）：

| 层 | 内容 | 成本 |
| :--- | :--- | :--- |
| L0/L1（**v1 立即获得，零 App 改动**） | 应用列表泛化行（`services.bandix` 已可见）+ 应用详情（现成 `service.restart`，WP-5 允许集加 bandix） | 0 行 |
| L2（1 条注册表项） | bandix 服务富化行（版本/状态）+ 动作条目（限速默认值/白名单/主机名对话框）——需 WP-5 打开"ubus 转发项"机制（其 DESIGN §5 表中已预留 v1 未实现） | 1 条 |
| L3（1 个 feature 模块，**建议后置**） | 流量页：设备 Top / 连接 / DNS 摘要 + 动作，与网页四页能力对齐；不复制网页设置表单全字段 | 1 模块 |

**明确不做**：App 内嵌 webpage；App 侧编辑 `port`/`iface`/`data_dir` 等引擎参数（留网页）。

---

## 8. W8.x 实施分解（拍板后执行）

| 步骤 | 内容 | 依赖 |
| :--- | :--- | :--- |
| W8.1 | 设计定案（本文档 → v1.0，D1–D6 拍板） | — |
| W8.2 | 包骨架 + `.uc` 读面 + 信封/错误码 + 能力探测 | D1/D3 |
| W8.3 | 写面 8 方法 + 校验表 + logd 审计 + README 诊断 | D4/D5 |
| W8.4 | `uci-defaults` 默认开启 + `quartz-defaults` 追加 + `check-manifest.sh` 断言（跨 WP） | D2 |
| W8.5 | Dashboard P1 服务行 + 深链（WP-3 文件，登记后动） | D1 |
| W8.6 | 单包编译 → 全量构建 → **VM 验收（§9）** | W8.2–W8.5 |
| W8.7 | 文档同步（WORKPLAN §2/§4、TDD 如涉及、ThirdParty DESIGN 交叉引用、README） | W8.6 |
| W8.8（P2，另轮） | Dashboard 流量卡 + WP-4 `bandix` 节 + WP-3 卡片 | §6.2 |

---

## 9. 验收标准（本 WP 完成定义）

**已完成的本地验证（W8.2–W8.5）**：
- **宿主单测 64/64 PASS**（`dev/unit-test.sh`，用锁定基座目标架构 ucode + 桩替换 `ubus`/`log`）：方法表形状与数量（20）、动作面收敛（`installUpdate`/`clearData`/`deleteDevice`/`checkUpdate`/`restartService` 均不可见）、校验正反例（MAC 归一化与注入拒绝、区间先后与 90 天上限、枚举/字符集/分页上限/主机名长度、days 数组元素域）、上游两形态归一（`{status,data}` 与 unwrap）、错误映射（`success:false` → `data_unavailable` 且带出上游原因）、参数转发（含 days 数组与小写 MAC）、能力探测缓存；
- 单包编译通过（aarch64 目标）；x86_64 增量全量构建 → 镜像内 `quartz-bandix-bridge 0.1.0-r1` 在列；
- **VM 验收 41/41**（`dev/vm-accept.sh`，命令与原始 KEY=VALUE 见 §12）；视觉证据：Dashboard 卡（`luci-app-dashboard/dev/shots/vm/dashboard-*-wp8.png`）与 bandix 四页（`dev/shots/vm/bandix-*-wp8.png`）。
- **验收过程发现并修掉 3 个真实缺陷**（宿主单测与文档推理都没抓到，见 §12）：同步委托死锁、数值入参类型提示与传输层不匹配、写面 `data:null` 被误判为失败。

**VM 验收（x86_64，2026-10-17，`dev/vm-accept.sh` → **41/41 PASS**）**：

1. **构建**：单包编译通过；全量构建镜像内 `quartz-bandix-bridge` 在列（manifest 断言）；`ubus -v list` 见 `quartz.bandix`（10 读 + 8 写）；
2. **默认开启 + 真数据（x86_64 VM）**：全新镜像首启 → `uci get bandix.traffic.enabled`=1、`/etc/init.d/bandix` running、`127.0.0.1:8686` 在听、`ubus call quartz.bandix status` 返回 ≥1 设备真数据、快照 `services.bandix={running:true,core:"0.12.11"}`；
3. **一致性实测（本设计的关键验收）**：同一时刻 `quartz.bandix status` 的设备速率/字节与 LuCI bandix 状态页显示**相等**（抽样 ≥3 台）；反向：`limit.default.set` 生效后网页「限速」区同步显示；
4. **安全负例**：非法入参全拒（MAC 注入/超范围/未知枚举/超长分页）；`installUpdate`、`clearData` 不在契约（`list` 可见性核对）；LuCI 会话调用 `quartz.bandix` 被拒（只有 root 侧可调）；
5. **默认开启幂等**：二次启动不重复写 uci；手动关 traffic 后重启**不被重新打开**；模拟 sysupgrade（保留 `/etc/config/bandix`）后标记位生效、用户选择保持；
6. **eBPF/BTF + flow offloading 交互**（与 WP-7 D4 合并）：x86_64 与 aarch64 双目标；`flow_offloading` 开/关的统计差异记录成表（结论写回 §1.7）。**✅ 2026-10-17 双目标完成**：x86_64 见 WP-7 D4；aarch64/FNet3399 真机 A/B 见 §1.7（差 <2%，无需关软件卸载；硬件卸载默认关）；
7. **开销记录**：bandix 常驻内存/CPU（traffic-only 与 traffic+connections+dns 两档）记入设计文档，作为 D2 的复核依据。**✅ 2026-10-17**：FNet3399 真机 traffic-only = RSS 4.6 MB / CPU 0.1%（30s 均值）；x86_64 VM 两档见 `utils/quartz-daemon/dev/accept-bandix-p2-*.metrics`。

---

## 10. 决策记录（**已拍板**：2026-10-17 用户「按推荐来吧」，D1–D6 全按推荐）

| # | 决策项 | 结论 |
| :--- | :--- | :--- |
| **D1** | Bridge 上游 | **① 委托 `luci.bandix`**（一致性最强、零重复；逃生舱=内部可换直连 core HTTP，对外契约不变）。②直连 core、③App 内嵌网页 均否决（§2.2） |
| **D2** | 默认开启模块 | **仅 `traffic`**；connections/dns 默认关（两端可开，同一份 uci） |
| **D3** | 包名/对象名 | `quartz-bandix-bridge` / ubus 对象 `quartz.bandix` |
| **D4** | 动作面 | 不暴露 `installUpdate`/`clearData`/`deleteDevice`/`checkUpdate`；`restartService` 不重复提供（走 WP-5 `service.restart`，允许集加 `bandix`） |
| **D5** | WP-5 衔接 | 本轮只做允许集加 `bandix`；"ubus 转发项"机制（App L2 用）随后 |
| **D6** | App 交付梯度 | L0/L1（v1 零改动）→ L2（1 条注册表项）→ L3 流量页（后置） |

### 已评估并否决

- **直连 core HTTP API**（§2.2-②）：重复实现 + 追 API 漂移（上游 main 与 0.12.11 端点已现差异）→ 一致性反而下降；
- **给 Bridge 投 `unauthenticated` ACL**：等于把写面（限速）暴露给局域网 `/ubus`（§4）；
- **把 `installUpdate` 暴露给 App**：无签名校验的 `apk add --allow-untrusted` 不进 App 通道（§3.3）；
- **改上游 `net/bandix`、`net/luci-app-bandix` 镜像内容**：v1 零上游改动（"不可变上游"）；若需适配（如 probe 扩展）走 fork `quartz` 分支 + `-quartzN` tag（TDD §4.7）。

### 风险

1. **上游 `luci.bandix` 方法/字段改名** → 能力探测（`version.upstream_methods`）+ Bridge 对外契约兜底：App 无感、监控面可降级；上游活跃（0.12.12，2026-09 仍在发版），"上游修复自动跟进"与本项目 feed 策略同源；
2. **core API 与文档漂移**（README main ≠ 0.12.11）→ 契约以**锁定版本 + VM 实测**为准（§9-2/3 定稿字段与枚举），锁 arch SHA256 不变；
3. **sysupgrade 后 uci-defaults 回放覆盖用户选择** → 标记位放 conffile 内（§5.2），验收 §9-5 覆盖；
4. **eBPF/BTF/flow offloading 在目标内核的实际表现** → §9-6 验收项（RK3399 6.12 内核与 x86_64 双目标）；
5. **常驻开销**（默认开启的代价）→ §9-7 实测记录；D2 可据此收窄；
6. **单人维护成本** → 契约刻意做小（读 10 + 写 8）、实现集中在单文件、不追平上游 32 方法（§3.1）。

---

## 11. 跨 WP 登记（状态：2026-10-17 本地落地）

| # | 文件/路径 | 所有者 | 内容 | 状态 |
| :--- | :--- | :--- | :--- | :--- |
| 1 | `Packages/meta/quartz-defaults/Makefile` | 共享（追加式） | 追加 `+quartz-bandix-bridge` 一行 | ✅ 已落地（本地，待推送） |
| 2 | `Packages/luci/applications/luci-app-dashboard/`（`ServicesCard.vue` + `app.css` + po + mock + 重建产物） | WP-3 | P1 Bandix 行（状态点 + 版本 + 存在性判定的详情深链）；服务网格 3→4 列（1120px 断点 2 列） | ✅ 已落地（本地，待推送） |
| 3 | `Packages/utils/quartz-daemon/`（新节 `bandix`） | WP-4 | **P2 已落地（2026-10-17）**：经 `quartz.bandix` 取数慢刷 10s；字段与网页同源（online/wan/top≤8/conn/dns）；契约 §3.7 + 单测 6 例 + VM 验收组（`dev/vm-accept-bandix-p2.sh`） | ✅ 已落地并推送 |
| 4 | `Packages/utils/quartz-app-api/` | WP-5 | **已落地（2026-10-17）**：允许集加 `bandix`；「ubus 转发项」机制（DESIGN §5.1；bandix 写面 5 条目 + 唯一读条目 `bandix.limits`；无任意透传、logd 审计）；单测 10 例 + VM 验收 6b 组 | ✅ 已落地并推送 |
| 5 | `Builder/scripts/check-manifest.sh` | WP-1 / WP-12 | **无需改动**——Gate B ② 现读 quartz-defaults DEPENDS 派生断言（§5.1 更正） | — |
| 6 | `Builder/config/seed-generic.config` | WP-12 | **无需改**（通用目标不预装，如实说明 §5.1） | — |
| 7 | `ThirdParty/net/luci-app-bandix`（fork） | WP-7 | v1 零改动（不可变上游）；后续适配走 fork `quartz` 分支 | — |
| 8 | WORKPLAN §2（WP-8 状态）/ §4（本设计稿与跨 WP 行） | 本对话 | 设计稿落地时同步，实现/验收后补记 | ✅ 已回写（验收后补记） |
| 9 | `QuartzWrt-App`（`features/registry.dart` + `features/bandix/*`） | WP-6 | **L2 已落地（2026-10-17）**：ServiceRowSpec 首个条目（列表富化行 + 详情「流量与限速」卡 + 默认限速/白名单/主机名三对话框）；**L3 已落地**：流量明细页 `/apps/bandix`（摘要 + Top≤8 + 动作入口）；App v0.4.0，测试 77 项 | ✅ 已落地并推送 |
| 10 | `Builder/devices/erya_fnet-3399/`（仅交叉引用） | WP-11 | 真机核验证据引用（本包 dev/rk3399-device-check.sh + accept-rk3399-*.log）；**待办**：刷入 10-17+ 镜像后复验 bridge+P2 链路的真机路径 | 📎 引用登记 |

---

## 12. 实现记录（W8.2–W8.5，2026-10-17）

**交付物（本地，待推送）**：

```
bridge/quartz-bandix-bridge/
├── Makefile                                     # PKGARCH:=all，纯数据/脚本包（Build/Compile=true）
├── README.md                                    # 用法/诊断/ACL 说明/单测说明
├── docs/DESIGN.md                               # 本文档
├── files/usr/share/rpcd/ucode/quartz-bandix-bridge.uc   # 契约实现（唯一逻辑文件，~560 行）
├── files/etc/uci-defaults/99-quartz-bandix      # 出厂默认开启（标记位防 sysupgrade 回放）
└── dev/{unit-test.sh,fixture.json,stub.uc,tests.uc}     # 宿主单测（不进安装包）
```

**实现要点（与设计稿的差异均已回写正文）**：
1. **懒连接**：`connect()` 在首次调用时建立（rpcd 装载脚本时 ubusd 未必就绪），装载期不可能因 ubus 失败——比基座 `ddns.uc`（顶层 `let ubus = connect()`）更稳；
2. **方法名下划线风格**（`limit_schedule_set`），避免点号在 ubus 工具链中的歧义；`args` 声明同时充当 rpcd 类型策略（`''`=string / `64`=int64（epoch ms 必需）/ `[]`=array）；
3. **上游失败原因带出**：`normalize()` 记录最近一次失败原因，`data_unavailable` 的 `message` 直接携带（如"Connection monitoring is not enabled"）——App 无需猜测；
4. **审计**：写动作成功/失败各落 logd 一行（`openlog`/`syslog`，与 app-api 同款纪律）。

**宿主单测发现并修掉的两个真实缺陷**（若直接上机会在设备上暴露）：
- `_last_error` 声明位置在使用它的箭头函数**之后** → ucode 报 `access to undeclared variable`（任何读失败路径都会炸）→ 声明上移；
- `v_enum(args.is_query, { 0: 1, 1: 1 })` 用数字字面量做对象键 → ucode **语法错误**（`Expecting label`）→ 改为 `v_int(is_query, 0, 1)`。

**Dashboard P1（WP-3 文件，本对话代改并登记）**：`ServicesCard.vue` 增 Bandix 行（`services.bandix.running` 状态点 + `core` 版本 + `host.hasPage('admin/network/bandix')` 存在性判定的"详情"深链）；`app.css` 服务网格 3→4 列（≤1119px 2 列、≤699px 1 列）；`po/zh_Hans` 增 `Details`；`dev/mock/status.json` 增 bandix（降级样例 `status.partial.json` 保持无 bandix，覆盖"未安装"态）；产物已重建（`app.js` gzip 28.7KB / `app.css` 2.5KB，均在预算内）。

**VM 验收（2026-10-17，41/41 PASS）暴露并修掉的 3 个真实缺陷**（宿主单测与纸面推理都没抓到）：

| # | 现象 | 根因 | 修法 |
| :--- | :--- | :--- | :--- |
| 1 | `ubus call quartz.bandix version` → **Request timed out** | **同步委托死锁**：`luci.bandix` 与我们是同一个 rpcd 进程提供的对象，ucode 插件里同步 `ubus.call` 会把 rpcd 的事件循环占住，嵌套调用永远等不到应答 | 全面改 **异步**：`conn.defer({object,method,data,cb})` + 回调里 `request.reply()`，把 deferred 资源交回 rpcd（其 `pending_replies` 机制等待 + 守护超时）。rpcd 的 ucode 支持正是为此设计的 |
| 2 | `limit_default_set` 直接 **Invalid argument**（连处理器都没进） | rpcd 的 ucode 策略要求**每个传入参数都在 args 里声明且 blobmsg 类型精确匹配**（未声明即拒）；而传输层按数值大小自动选类型（`blobmsg_json`：int32 范围内 → INT32，超出 → INT64）——**单一数值提示必然拒掉一半调用**（声明 64 时 `102400` 被拒） | 含数值字段的方法统一改 **`data: {}` 对象**形态（与基座 `luci-app-tailscale` 的 `form_data` 同款）：对象内部字段不做策略检查，数值合法性由本层校验器把关（ucode 里 int32/int64 同为 `'int'`）。纯字符串方法保持扁平声明 |
| 3 | `limit_default_set` 报 `action_failed`（上游其实成功） | 上游写面常返回 `{"status":"success","data":null}`——归一后 `data == null`，被我误当失败 | 成功判据从 `data != null` 改为 **`_last_error == null`**；`limits` 聚合的"全失败"判据同步改用错误标记 |

（三个缺陷连同构建/验收侧的机制坑已写入 WORKPLAN 坑位 **#60–#65**：ucode 同步委托死锁、入参策略与数值宽度、写面 data:null、构建期两个静默少包陷阱、WSL PATH 空格拦 `find -execdir`、VM 验收工具事实。）

另记一条**观测特性**（非缺陷）：`hostname_set` 的生效在设备表上有**数秒到十几秒延迟**（绑定文件即时写入，设备表按自身刷新周期更新）——App 侧做"设置后回读"需容忍该延迟（验收脚本用 20s 轮询窗口）。

**真机核验（FNet3399/RK3399，2026-10-17，P2 收尾轮）**：`dev/rk3399-device-check.sh`——①eBPF 真数据（无 BTF 亦可）；②flow offloading A/B（受控 LAN 客户端 10 MB 下载，两轮计数 ≥91%，差 <2%；测后恢复原值）；③开销 RSS 4.6 MB / CPU 0.1%。结果并入 §1.7 与 §9-6/9-7。**边界如实记录**：真机当时为 10-16 构建（**不含 bridge 包**），本包与 daemon `bandix` 节的真机联调待用户刷入 10-17+ 镜像后复验（VM 侧已全验）。

**验收命令与证据**：

```sh
dev/unit-test.sh                       # 宿主单测 64/64（桩替换 ubus/log）
BUILD_TREE=... scripts/build-local.sh  # 或手动：compose-seed + defconfig + make（x86_64）
QuartzWrt-Builder/scripts/test-in-vm.sh --detach
dev/vm-accept.sh                       # 41/41 PASS（远端脚本经宿主 HTTP + VM wget 推送）
python3 dev/vm-shot.py --tag wp8       # bandix 四页视觉证据
# Dashboard 侧证据（WP-3 工具）：luci-app-dashboard/dev/vm-shot.py --tag wp8
```

**锁定形态复验（2026-10-17，收尾）**：把构建树的 quartz feed 从临时 src-link 切回 `feeds.conf.extra` 的锁定行（`3a8d172`）后，按 build-local 的顺序（feeds → 种子装配 → defconfig → Gate A → make）重建：镜像 manifest 含 `quartz-bandix-bridge 0.1.0-r1` / `quartz-defaults 0.1.0-r4` / `luci-app-dashboard 26.290.32689~3a8d172`（**规范版本戳** = feed 提交），**VM 验收再次 41/41**。过程另踩中两个"静默少包"陷阱（`feeds install` 重写 `.config`；删 apk 不清构建戳）——见 WORKPLAN 坑位 #63。

**构建期踩到的环境坑（与代码无关，但会拦构建）**：WSL 宿主 PATH 含空格项（`/mnt/c/Program Files/...`）时，基座 `include/rootfs.mk` 的 `find -execdir`（SOURCE_DATE_EPOCH 触发）会以 "relative path ... is insecure" 拒绝执行 → `package/install` 失败。用不含 `/mnt/c` 的 PATH 跑构建即可（本 WP 记录，见 WORKPLAN 坑位）。

---

*变更记录：*
*v0.1 2026-10-17 —— 首稿（讨论稿）。事实核实基于本地锁定产物（core 0.12.11 / luci 0.12.12 镜像 + 锁定基座 rpcd 源码）；宿主无法复现 core 运行时（非 root 无 CAP_BPF），字段与枚举的最终形状列 VM 验收定稿；D1–D6 待用户拍板。*
*v1.0.1 2026-10-17 —— **VM 验收 41/41**：修掉 3 个真缺陷（同步委托死锁→异步 defer；数值入参类型提示→data 对象；写面 data:null 误判→错误标记判据），补视觉证据与构建期环境坑（PATH 空格 → find -execdir），§9/§12 同步。*
*v1.0 2026-10-17 —— **定案并开工**（用户「按推荐来吧」，D1–D6 全按推荐）。W8.2–W8.5 落地：契约实现（读 10 + 写 10，方法名下划线化）、uci-defaults 默认开启、quartz-defaults 追加、Dashboard P1 行与产物重建、宿主单测 64/64（含两个真实缺陷修复，见 §12）；§5.1 更正"需增 manifest 断言"的误判（Gate B ② 派生覆盖）；§9 增本地验证结论。*
*v1.0.2 2026-10-17 —— **P2 收尾轮（用户「把没做的做了吧」）**：①真机核验（FNet3399/RK3399）——eBPF 无 BTF 亦可加载、软件 flow offloading 不遮蔽统计（A/B 差 <2%）、开销 4.6 MB/0.1%（§1.7/§9-6/9-7 定稿，工具与证据 `dev/rk3399-device-check.sh` + `dev/accept-rk3399-*.log`）；②跨 WP 收口登记见 §11（WP-4 daemon 节 / WP-3 卡 / WP-5 转发项与允许集 / WP-6 App L2+L3 全部落地）。*

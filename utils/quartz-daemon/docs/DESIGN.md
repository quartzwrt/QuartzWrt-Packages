# quartz-daemon 设计规范（讨论稿）

**版本**: v0.1（**讨论稿**）
**日期**: 2026-10-12
**状态**: 待用户拍板（§9 七项）；§1 事实与 §3 数据源均已按锁定基座 / VM 实机核实，可直接作为拍板依据
**只读契约（实现必须对齐，本文不得与之冲突）**: `luci/applications/luci-app-dashboard/docs/AGGREGATE-API.md`（冻结 v1）——
本文描述"如何实现"，不新增字段；需要新数据一律走契约加法流程（契约 §0）
**依据**: TDD §3.3 / §3.4 / §4.4 / §8；WORKPLAN §3 WP-4；WP-3 `DESIGN.md`（消费端，含 §2.2 降级四态）
**验收（WORKPLAN §3 WP-4）**: ① `make package/feeds/quartz/quartz-daemon/compile` 通过；② 设备上接口返回正确数据

---

## 0. 定位与范围

`quartz-daemon` 是 QuartzWrt 的数据聚合守护进程：后台定时采集系统负载、网络吞吐、Docker 状态、在线设备，
经**冻结契约**输出到文件（Dashboard 主通路）与 HTTP（WP-5/App 通路）。

**v1 明确不做**（防范围蔓延）：

| 不做 | 理由 / 归属 |
| :--- | :--- |
| 认证与对外暴露 | HTTP 仅监听回环；对外认证由 WP-5 定义（契约 §1） |
| 写操作 / 读时触发采集 | 契约 §5"只读"义务；写操作走 WP-5 rpcd 体系 |
| 联网检查（核心版本比较、资源下载） | TDD §4.4 运行时资源治理，WP-7/8 链路，后置 |
| 历史持久化（月度流量等） | series 仅内存环形缓冲；持久化需求出现再评估 |
| 直读第三方插件内部文件 | 一律经 ubus / bridge（TDD §5）；openclash core 版本为"尽力"探测（§3.6） |
| App 专属机制（事件发送、mDNS、TLS pinning） | 归 WP-5/6；daemon 仅预留事件检测 seam（§12） |

## 1. 事实核实（2026-10-12，锁定基座 + VM 实机）

### 1.1 构建侧（基座源码 / 构建树核实）

| 事实 | 值 | 对设计的影响 |
| :--- | :--- | :--- |
| 基座 Go 工具链 | **1.26**（`feeds/packages/lang/golang`，`GO_DEFAULT_VERSION:=1.26`） | 交叉编译走 feed 工具链，无需自备 |
| golang-package.mk | module 模式：`go.mod` 存在 → `go list` + `go install`（GOPATH 隔离于 `.go_work/`，`GOTOOLCHAIN=local`） | **in-tree 源码可行**（Build/Prepare 拷贝 `./src`，无需自我引用 git） |
| 链接方式 | 默认 `CGO_ENABLED=1` + 外链（动态链接 musl libc）——tailscale 等 feed 内 Go 包同款 | "静态编译"措辞建议修正（§11）；不为此 hack 构建系统 |
| 宿主 Go | **未安装**（Debian 13；apt `golang-go` = 1.24 可装） | dev 侧单测需装 Go；go.mod 定 `go 1.22` 兼容 |
| 许可证 | 仓库 LICENSE = GPL-2.0 | `PKG_LICENSE:=GPL-2.0-only` |

### 1.2 运行侧（VM 实机核实，QEMU x86 + 锁定固件镜像）

| 数据源 / 项 | 实测值 | 对设计的影响 |
| :--- | :--- | :--- |
| `/tmp/sysinfo/model` / `board_name` | `QEMU Standard PC (i440FX + PIIX, 1996)` | `host.model` 主源（真机 = 设备型号） |
| `/etc/openwrt_release` | `ImmortalWrt 25.12-SNAPSHOT r0+38247-ce35a9e4ef` | `release` **兜底**源；QuartzWrt 发行版标识缺失 → §9-D7 新增 `/etc/quartz-release` |
| `/sys/class/thermal` | 仅 `cooling_device*`，**无 thermal_zone**；无 hwmon | `temp_c` 必须支持 `null`（契约已允许）；真机（x86 coretemp / R4S soc_thermal）走探测链 |
| `/proc/net/route` | **无默认路由**（VM 网络仅 lan + docker，无 wan 段） | WAN 解析链与"无 WAN"降级路径在 VM 即可实测覆盖 |
| ubus CLI | `/bin/ubus` 可用；`ubus call network.interface.wan status` → `Command failed: Not found` | ubus 调用必须容忍 not found（属合法态，非 errors） |
| `ubus call service list` | 含 dockerd / dnsmasq / network…；openclash init 为 `USE_PROCD=1`（当前未运行） | `services` 节数据源（一次调用取全量） |
| Docker | `/var/run/docker.sock` = `srw-rw---- root:docker 660`；`dockerd`/`docker` 在 `/usr/bin` | daemon 以 root 运行可直连；`available` 判定看可执行文件 |
| `/tmp/dhcp.leases` | 存在、当前为空；dnsmasq `dhcp-leasefile=/tmp/dhcp.leases` | clients 主机名源 |
| `/proc/net/arp` | 1 条（`192.168.1.2` on `br-lan`，slirp 网关） | clients 在线判定主源；VM 冒烟有 1 条可测 |
| `/var/run` | 符号链接到 `/tmp/run`（**tmpfs，重启清空**） | 目录须启动自建；无需 fsync；开机后首写要快 |
| 端口占用 | 80 / 53 / 7681 / 22；**127.0.0.1:8899 空闲** | HTTP 默认端口候选（§9-D4） |
| `/etc/config/quartz` | 已存在（WP-2 主题配置） | daemon 用独立文件 `/etc/config/quartz-daemon`（§6） |
| 内核 / 主机名 | `6.12.112` / `ImmortalWrt` | `/proc/sys/kernel/osrelease`、`os.Hostname()` 直接可用 |

## 2. 架构（进程模型与采集节奏）

```
                    ┌─ fast 循环（每 interval=2s，纯 /proc + /sys 文件读，<10ms）
                    │     host / system / net 快字段 + 装配全部节
       采集侧 ──────┤
    （2 个 goroutine）└─ slow 刷新（每 10s，独立超时 2–3s）
                           ubus（WAN 详情 / service list / board）、docker socket、
                           无线关联（可选，15s）→ 写入小粒度缓存（mutex）
                    │
   每周期：装配快照 → 一次 marshal → ① 原子写文件  ② atomic.Value 供 HTTP 直读
                                        （同一字节，两条通道严格一致）
```

- **快慢分离的理由**：2s 热周期绝不被 exec / socket 调用拖慢；慢采集超时只记 `errors`，不阻塞快周期。VM 实测 `ubus call` 为进程级 exec（fork 开销），不适合逐周期调用。
- **单写者**：只有 fast goroutine 产出快照字节；slow 只更新缓存字段 → 无写竞争，HTTP 侧零锁（`atomic.Value`）。
- **ticker 语义**：周期重叠时跳过（防积压）；`interval` 变更经 procd reload 重启进程生效（§6）。
- **启动即采**：首个快照不等首个 interval（tmpfs 重启清空，Dashboard 尽快有数据）。
- **HTTP 非致命**：bind 失败（如端口被占）→ 记日志 + `errors`，每 60s 重试；文件通道不受影响。
- **生命周期**：`SIGTERM` 优雅退出；崩溃由 procd `respawn` 兜底；启动时无 ubus / 无网络 / 无 docker 均可正常运行（各节按降级规则处理）。

## 3. 逐节采集设计（契约 §3 对照）

### 3.1 `host`（必备）

| 字段 | 数据源 | 口径 / 降级 |
| :--- | :--- | :--- |
| `hostname` | `os.Hostname()`（/proc/sys/kernel/hostname） | 直读 |
| `model` | `/tmp/sysinfo/model` → 兜底 `ubus call system.board` 的 `model` | 均失败 → 空串 |
| `release` | `/etc/quartz-release`（新增，§9-D7）→ 兜底 `/etc/openwrt_release` 的 `DISTRIB_DESCRIPTION` | 空串兜底 |
| `kernel` | `/proc/sys/kernel/osrelease` | 直读 |
| `uptime` | `/proc/uptime` 首值取整 | 直读 |

### 3.2 `system`（必备）

| 字段 | 数据源 | 口径 |
| :--- | :--- | :--- |
| `load` | `/proc/loadavg` 前 3 值 | 直读 |
| `cpu_pct` | `/proc/stat` 首行 delta | `busy = total − idle − iowait`；`pct = busy/total×100`，1 位小数，clamp 0–100 |
| `mem` | `/proc/meminfo` | `total = MemTotal`；`used = MemTotal − MemAvailable`（内核 6.12 具备）；`pct` 1 位小数 |
| `swap` | `/proc/meminfo` | 全 0 为合法（未启用，契约明示） |
| `rootfs` | `statfs("/")` | df 口径：`total = Blocks×Bsize`，`used = total − Bavail×Bsize`，`pct` 1 位小数 |
| `temp_c` | 探测链：`/sys/class/thermal/thermal_zone*`（type 匹配 `cpu`/`soc`/`x86_pkg_temp`/`k10temp` 等）→ `hwmon` 白名单（coretemp / k10temp / cpu_thermal / soc_thermal…） | 匹配集合内取最大（避免逐核抖动）；全部失败 → `null`（VM 实测即 null） |

### 3.3 `net`（必备）

- **WAN 接口解析（分层，快路径为主）**：
  1. 快（每周期）：`/proc/net/route` 默认路由（Destination=00000000）的 Iface；接口地址（`ipv4`）用 Go `net.Interfaces` 取该接口 IPv4（微秒级，不 exec）；
  2. 慢（10s 刷新）：`ubus call network.interface.wan status` 取 `proto`（`l3_device` 用于校正默认路由设备，如 pppoe-wan）；
  3. 无默认路由且无 wan 段（VM 实测形态）→ `up=false`、`ifname/proto/ipv4` 空串——**合法态，不记 `errors`**。
- `up` = 默认路由存在 且 承载接口 operstate 为 up。
- `rx_bps`/`tx_bps`：`/sys/class/net/<ifname>/statistics/{rx,tx}_bytes` delta ÷ Δt；Δ 为负（计数器回绕 / 接口重建）→ 重置基点并记 0。
- `series`：内存环形缓冲，`step = interval`，各数组 ≤60 点，初值 `[0]`（契约示例同款）。

### 3.4 `docker`（可空；慢刷 10s）

| 项 | 实现 |
| :--- | :--- |
| `available` | **dockerd 可执行存在与否**（未安装 → `false` → Dashboard 隐藏整卡，契约语义） |
| `running`/`total` | `http.Client` + `DialContext`(unix) → `GET /v1.24/containers/json?all=1`；`State=="running"` 计数 |
| `images` | `GET /v1.24/images/json` 数组长度 |
| `unhealthy` | 容器 `Status` 字符串含 `(unhealthy)` → 容器名；≤8 截断 |
| 失败 | 二进制在但 socket 不可用 → **整节 `null` + `errors["docker: ..."]`**（区分"未装"与"装了连不上"）；下轮自动重试 |

零依赖实现：原生 HTTP over unix socket，**不引入 docker SDK**（§9-D1）。

### 3.5 `clients`（可空）

- **组合源**：
  1. `/proc/net/arp`——在线判定主源（过滤非 LAN 设备：排除默认路由接口、`docker0` 等；`Flags=0x2` 完整条目）；
  2. `/tmp/dhcp.leases`——`mac → host` 名补全（空则 `host=""`）；
  3. `iwinfo`（可选，存在才用，15s 低频）——无线关联列表；`inactive` 毫秒供 `last_seen` 修正。
- **在线判定与 `last_seen` 语义（本稿关键点，拍板项 D3）**：
  - 在线 = 本轮 ARP 出现 **或** 无线关联中；
  - `last_seen` = **最后一次确认在线的时刻**（连续在线设备 ≈ `ts`；无线设备若有 `inactive` 则用 `now − inactive` 修正，更接近真实活跃时间）；
  - 不做离线宽限（`items` 只含当前在线）；`total` = 在线数；`items ≤32` 按 `last_seen` 降序、并列按 MAC 稳定排序；
  - **局限如实声明**：这是"最后确认在线"而非"最后业务活跃"；每设备真实活跃度 / 流量需要 bandix（模式 A）——后续以加法字段接入，不改本语义。
- VM 现状：1 条（slirp 网关）→ 集成验收冒烟即可；真机形态待 R4S。

### 3.6 `services`（可空，开放映射；慢刷 10s）

- 单次 `ubus call service list` → 探测集 **{openclash, dockerd}**（代码内常量，扩展 = 加键不改契约）；
- `running`：对应服务任一 procd instance `running=true`（openclash init 实测 `USE_PROCD=1`，适用）；
- `services.openclash.core`（**尽力提供**）：core 二进制存在时 exec `<core> -v` 解析版本串，5min 缓存；不存在 → 字段缺省（Dashboard 显示 Unavailable）；`core_update` **后置**（需联网比较，属 TDD §4.4 治理）；
- ubus 不可用 → `services = null` + `errors`。

### 3.7 `errors`

- 机器可读短串 `<组件>: <原因>`；**仅真实采集异常**（"WAN 未配置"是合法态，不入列——与 §3.3 一致）；
- 快照内全量；日志侧同类错误节流 1 次/分钟（防 2s 周期刷屏，走 logd）。

## 4. 输出、自守与错误语义（契约 §6 生产者义务逐条落地）

| 契约义务 | 实现 |
| :--- | :--- |
| 原子写 | `/var/run/quartz/` 启动自建（0755）；写 `.status.json.tmp`（同目录）→ `chmod 0644` → `rename`。目录为 tmpfs：rename 原子性即足，不 fsync（留注释说明） |
| 单节失败不中断 | 每采集器包 `recover` + 独立错误通道；失败节置 `null`（或叶片 `null`），其余照常 |
| 上限自守 | marshal 后量测：>32KB → 阶梯截断（`clients.items`→16、`series`→30）再量，防御性兜底只保留必备节 + `errors` 注记；`items≤32` / `series≤60` / `unhealthy≤8` 在数据入口截断 |
| 字段一致 | `interval` / `series.step` = 实际 tick；`ts` = 采样完成时刻（epoch 秒）；`collect_ms` = 本轮 fast 装配耗时（ms，含 marshal） |
| 契约只增 | 任何字段变更走 `AGGREGATE-API.md` 流程（双端改版号 + WORKPLAN 记录），同步 `dev/mock/` |

## 5. HTTP 接口

- 监听 `127.0.0.1:8899`（默认，uci 可改；VM 实测空闲）。**未来 WP-5 与 App 走此通道**（契约 §1）。
- `GET /api/v1/status` → `200 application/json`（内容 = 最近成功快照字节）+ `Cache-Control: no-store`；非 GET → 405；未知路径 → 404（JSON 短体）。
- 超时：`ReadHeaderTimeout 5s`、`IdleTimeout 60s`；无 TLS / 无认证（回环边界，认证归 WP-5）。
- 不产生任何 DNS 查询（unix socket + 回环），不触发解析器路径。
- **App / WP-5 上游通道**：本接口是专属 App 数据链路的唯一上游（WP-5 认证后转发）；义务边界、预留 seam 与不做清单见 §12。

## 6. 配置（`/etc/config/quartz-daemon`）

```
config daemon 'main'
	option interval '2'        # 采集周期（秒），范围 1–300
	option listen '127.0.0.1'  # 监听地址（回环默认）
	option port '8899'         # HTTP 端口
```

- **读取方式**：启动时 exec `uci -q show quartz-daemon`（输出规范化，免自写 uci 解析）；缺失文件 / 缺项 = 内置默认值；非法值 clamp 到范围。
- **生效方式**：procd `service_triggers` 监听本文件 → reload = 重启进程（重读配置）；`interval` 变更同时重置 ticker 与 `series.step`。
- **独立于 `/etc/config/quartz`（WP-2 主题）**：所有权干净（外观 vs 采集互不跨界）。
- v1 **不设采集开关**（docker/clients/services 自动探测，最小配置面）；确有需求再加法加项。

## 7. 打包与集成

### 7.1 Makefile 要点（golang-package.mk + in-tree 源码）

```makefile
PKG_NAME:=quartz-daemon
PKG_VERSION:=0.1.0
PKG_RELEASE:=1
PKG_LICENSE:=GPL-2.0-only
PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

GO_PKG:=github.com/quartzwrt/quartz-daemon        # 零依赖，路径仅作标识；go 指令 1.22
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/quartz-daemon
GO_PKG_LDFLAGS_X:=$(GO_PKG)/cmd/quartz-daemon.version=$(PKG_VERSION) \
                  $(GO_PKG)/cmd/quartz-daemon.release=$(PKG_RELEASE)
GO_PKG_INSTALL_BIN_PATH:=/usr/sbin

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) ./src/* $(PKG_BUILD_DIR)/
endef

DEPENDS:=$(GO_ARCH_DEPENDS) +ubus
```

- **in-tree 源码**（Build/Prepare 拷贝，非"自我引用 git"）：改动即本地单包编译验证，不必先推 feed。
- `src/` 目录名在本包**无** luci.mk 的"C 源码目录"限制（本包走普通 `package.mk`）；命名保持 `src/` 与其他组件习惯一致。

### 7.2 安装布局与 procd 脚本

| 项 | 值 |
| :--- | :--- |
| 二进制 | `/usr/sbin/quartz-daemon`（`--version` 输出版本/build 信息） |
| init | `/etc/init.d/quartz-daemon`：`USE_PROCD=1`、`START=95`（网络就绪后起，慢刷首轮即有 WAN 细节）、`respawn`、`procd_add_reload_trigger quartz-daemon` |
| 配置 | `/etc/config/quartz-daemon`（`conffiles`，升级不覆盖用户改动） |
| 依赖 | `$(GO_ARCH_DEPENDS) +ubus`；libc 隐式（基座必备） |
| 资产 | **无 htdocs 资产** → 不涉 csstidy/jsmin 坑位（#15）；apk 体系无特殊处理 |

### 7.3 仓库布局

```
utils/quartz-daemon/
├── Makefile
├── README.md                    # 构建/配置/接口速查（W4.7）
├── docs/DESIGN.md               # 本文件（拍板后定稿）
├── src/                         # Go module（in-tree）
│   ├── go.mod
│   ├── cmd/quartz-daemon/main.go
│   └── internal/{collect,snapshot,output,httpsrv,config}/
├── files/quartz-daemon.init     # procd 脚本
├── files/quartz-daemon.conf     # uci 默认配置
└── dev/contract-check.py        # 契约校验脚本（§8）
```

### 7.4 集成点

- `meta/quartz-defaults/Makefile` **追加** `+quartz-daemon`（共享文件追加式，WORKPLAN §4 约定）；
- `luci-app-dashboard` 维持**软依赖**（其四态降级对"采集器未运行"有完整支持）——建议保持，由元包统一安装（§9-D6）；
- 落地后可通知 WP-3：真数据联调（WP-3 DESIGN §8-3 待办）在 W4.6 闭链。

## 8. 测试与验收

**单元测试（host `go test ./...`）**：
- fixtures = **VM 实机样张**（`/proc/stat`、`meminfo`、`route`、`arp`、`leases`、docker JSON、`service list` JSON，W4.5 抓取固化）；
- 覆盖：解析口径（CPU delta / mem / rootfs / route / arp / lease / docker Status）、边界（计数器回绕、空 lease、无 wan、无温度、ubus not found）、上限截断自守、原子写、config clamp。

**验收链（对齐 WORKPLAN 验收①②）**：
1. 单包编译 `make package/feeds/quartz/quartz-daemon/compile` 通过；build-local 全量带包构建通过；
2. **VM 集成**（`scripts/test-in-vm.sh` + pty-ssh）：
   - 运行面：`status.json` 存在、mtime 每 interval 更新、权限 0644、无 tmp 残留、`kill -9` 后 procd 拉活；
   - **契约校验**：`dev/contract-check.py`（ssh 取回快照 → 全字段/类型/可空性/上限断言，对照 `AGGREGATE-API.md`）；
   - HTTP：`wget -qO- 127.0.0.1:8899/api/v1/status`（busybox wget，实测可用）；
   - **真数据联调**：Playwright 打开 Dashboard 落地页，确认 hero + 五卡为真实数据渲染（闭 WP-3 待办）；
3. 真机（R4S 级）后置：与 WP-3 流畅性验收合并——观测 RSS、CPU、2s 周期开销。

**性能预算**：RSS ≤15MB；CPU 平均 <1%（R4S 级）；每周期写 ~2–3KB（tmpfs，无磨损）；无外部请求。

**架构覆盖**：x86_64 + aarch64（R4S/R5S）优先；`GO_ARCH_DEPENDS` 限定受支持架构。

## 9. 待拍板决策（推荐案）

| # | 决策 | 推荐 | 备选与理由 |
| :--- | :--- | :--- | :--- |
| D1 | 依赖策略 | **零第三方依赖**（stdlib-only）：docker 走 unix socket 原生 HTTP；ubus/iwinfo 走 CLI exec + JSON 解析 | docker SDK / go-ubus 绑定：依赖树大、交叉编译负担、供应链面大，收益低 |
| D2 | 采集节奏 | **快慢分离**：2s 纯文件读；10s 慢刷（ubus/docker）；无线 15s 可选 | 全量逐周期 exec：fork 开销 + 抖动，否决 |
| D3 | clients 语义 | 在线 = ARP ∪ 无线关联；`last_seen` = 最后确认在线（无线用 inactive 修正）；无离线宽限；局限如实声明，活跃度后置 bandix | 宽限期方案：`total` 语义会漂移；仅 ARP：无线休眠设备闪断 |
| D4 | HTTP | `127.0.0.1:8899` 默认、GET only、无认证（回环 + WP-5 边界）、端口冲突非致命 | 换端口/加 token：无必要，WP-5 定边界 |
| D5 | 配置载体 | 独立 `/etc/config/quartz-daemon`（interval / listen / port 最小面；reload=重启生效） | 并入 `/etc/config/quartz`：跨 WP 所有权混淆，否决 |
| D6 | services v1 范围 | openclash + dockerd 的 `running`；`openclash.core` 尽力（exec -v 缓存）；`core_update` 后置；dashboard 维持软依赖 | core_update v1 做：需联网比较，越 §0 边界 |
| D7 | 发行版标识 | 新增 `/etc/quartz-release`（由 **quartz-defaults** 提供），daemon 只读；缺失时降级拼串 | daemon 自带版本文件：身份文件应随发行版元包走 |
| D8 | App 适配范围 | daemon 只承诺 **HTTP 一等公民**（§12.1）+ 冻结事件检测 seam（只产生、不发送，v1 不实现）；推送发送 / mDNS / 认证归 WP-5/6 后置 | daemon v1 内建 webhook 发送：推送通道（Bark/PushDeer/Telegram）未定，配置与测试面过早扩大，易返工 |

## 10. W4.x 交付分解

| 序号 | 内容 | 依赖 |
| :--- | :--- | :--- |
| W4.0 | 本设计稿定案（v1.0）+ 决策记录 | 用户拍板 |
| W4.1 | 包骨架 + Makefile（golang 交叉编译打通，`--version` 最小二进制编译通过） | W4.0 |
| W4.2 | 采集核心：fast 循环（host/system/net）+ 快照 marshal + 原子写 + HTTP | W4.1 |
| W4.3 | 慢采集：docker / clients / services + 错误隔离 + 上限自守 | W4.2 |
| W4.4 | 配置：uci 解析 + procd 脚本 + reload 链路 | W4.1 |
| W4.5 | 单测与 fixtures（VM 样张抓取固化）+ `dev/contract-check.py` | W4.2 |
| W4.6 | VM 集成验收：镜像 → 运行面/契约/HTTP/Playwright 真数据（闭 WP-3 待办） | W4.3、W4.4 |
| W4.7 | 文档定稿（README）+ 同步项落地（§11） | W4.6 |

## 11. 同步项与跨 WP 登记（拍板后执行）

| 项 | 动作 | 归属 |
| :--- | :--- | :--- |
| TDD §8 待决表 "Daemon: Go 还是 Rust" | 更新为**已决策 Go**（两副本同步，用户确认后） | 用户 + 架构决策 |
| TDD §3.3 "静态编译为单一二进制" | 措辞建议改"单一自包含二进制（交叉编译；动态链接系统 musl libc，符合 feed 惯例）" | 同上 |
| TDD §4.4 | 补注：openclash core 版本先行接入 `services`，`core_update` 联网比较后置 | 同上 |
| `meta/quartz-defaults` | 追加 `+quartz-daemon`；新增安装 `/etc/quartz-release`（D7） | WP-4 追加（共享文件）；登记通知 |
| `luci-app-dashboard` | 维持软依赖（建议 D6）；`dev/mock/` 无需变更（契约未动） | 登记通知 WP-3 |
| 本 WP 文件所有权 | `utils/quartz-daemon/` 已登记为 WP-4（WORKPLAN §4，无冲突） | — |
| WP-5 / WP-6（App 链路边界） | 登记：daemon 数据面止于 HTTP 回环（§12）；认证/转发归 WP-5，mDNS/TLS/轮询策略归 WP-6；事件 seam 归属随 §9-D8 拍板后登记 | 登记通知两对话 |

## 12. 专属 App（WP-6）适配

**链路**：App **不走文件通道**（那是 LuCI 会话 + rpcd ACL 的专属路径），一律经 HTTP：

```
quartz-daemon ──HTTP 127.0.0.1:8899（本设计 §5）──▶ WP-5 quartz-app-api（JWT / ACL / 转发）
                                                        │ HTTPS（TLS / 证书 pinning 归 WP-6）
                                                        ▼
                                                     Flutter App
```

### 12.1 WP-4 的承诺（本设计已覆盖，此处显式化）

| 承诺 | 落地位置 |
| :--- | :--- |
| HTTP 与文件**同字节** | 单写者双通道（§2）：App 所见与 Dashboard 严格一致，不存在第二份数据逻辑 |
| 任何降级下**仍是合法 JSON** | 单节失败置 `null` + `errors`（§4）：App 无"非 JSON / 白屏"路径 |
| **并发安全** | `atomic.Value` 无锁读：App + WP-5 + Dashboard 并存无压力 |
| **零新增凭据** | daemon 仅回环、无认证（§5）：认证 / 签发 / 吊销全部集中 WP-5，App 链路上不出现第二套认证面 |
| **机器可读输出** | `errors` 短串、服务键、epoch 秒：本地化由 App 做，daemon 零 i18n 负担 |

### 12.2 预留 seam（冻结形状，v1 不实现）

1. **事件检测（TDD §8 "App 推送"待决项的天然落点）**：daemon 是唯一全天候状态观察者，快照序列即事件源（WAN 掉线、设备上/下线、容器转 unhealthy）。v1 **冻结形状不实现**；演进二选一：**(a)** daemon 内建 webhook 外发（出站连接，配置入 uci）；**(b)** WP-5 拉取事件列表。**归属建议**：事件"产生"留在 daemon（只有它有连续视图），"发送/订阅"归 WP-5 或独立模块（§9-D8）。
2. **运行时资源清单（TDD §4.4 点名 daemon）**：插件核心 / 规则库版本与更新状态——经 `services` 开放映射接入（`openclash.core` 即首例），**这就是 §4.4 的物化通道**，无需新机制。
3. **`clients` 全量 / 分页**：`total > items` 语义契约已有；App 设备页如需 >32 条，走契约加法（提升上限或加查询参数），对既有前端无破坏。
4. **OTA / 固件提示（WP-9）**：`system` 节将来以加法字段（如 `upgrade`）进入，App 首页与 Dashboard 同源。

### 12.3 明确不做（防越界）

| 不做 | 归属 |
| :--- | :--- |
| mDNS 发现、TLS / 证书 pinning | WP-5 / WP-6 |
| 推送消息发送（Bark / PushDeer / Telegram） | 后置（通道未定，§9-D8） |
| 写操作 / 控制面（重启服务、触发核心更新） | WP-5 rpcd 体系（契约 v1 注记） |
| App 专属字段 | **禁止**——App 与 Dashboard 共享同一契约；单端需求走"加法可选字段"，不得私加 |

**App 侧义务提醒（WP-6 照契约办）**：轮询降频 / 后台不取数（契约 §5 的移动端版本）、未知字段与未知 `v` 容忍。

---

*变更记录：*
*v0.1 2026-10-12 —— 首稿（讨论稿）：契约实现方案（§3 数据源逐节经 VM 实机核实）、架构（快慢分离 + 双缓冲 + 单写者）、打包（golang-package.mk + in-tree 源码）、验收链（编译 / VM 契约校验 / Dashboard 真数据联调）、待拍板 7 项（§9）。*
*v0.2 2026-10-12 —— 用户评审反馈（"考虑对专属 App 的适配"）：新增 §12（App 经 WP-5 的链路图、五项承诺——同字节/降级合法 JSON/并发/零凭据/机器可读、四项预留 seam——事件检测/运行时资源清单/clients 分页/OTA 字段、不做清单、App 侧义务）；§0 不做表补 App 专属机制行；§5 补上游通道指针；§9 增 D8（App 适配范围与 seam 归属）；§11 登记 WP-5/6 边界。**v1 实现计划（§10 W4.x）不受此节影响**——适配以 seam 冻结为主。*

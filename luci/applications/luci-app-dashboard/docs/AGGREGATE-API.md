# quartz-daemon 聚合 API 契约（冻结版 v1）

**状态**: **冻结**（2026-10-12，用户拍板）——本文是 WP-3 与 WP-4 之间的唯一接口依据
**维护**: WP-3（`luci-app-dashboard`）　**实现**: WP-4（`quartz-daemon`）　**消费方**: WP-3 Dashboard、WP-5/App（HTTP 通道）
**设计依据**: `docs/DESIGN.md` §3 / §6.2（增长规则）／WORKPLAN §3 WP-3/WP-4
**变更流程**: **加法优先**——新增可选字段或新节**不升版**；字段语义/类型变更、删除、必备性变化**升 `v`**；任何改动两侧（WP-3/WP-4）同步改版号并在 WORKPLAN 记录。

---

## 1. 通道与交付方式

| 通道 | 位置 | 机制 | 消费方 |
| :--- | :--- | :--- | :--- |
| 文件（Dashboard 主通路） | `/var/run/quartz/status.json` | daemon **原子写**（临时文件 + `rename`），权限 0644；浏览器经 rpcd `file.read`（LuCI `fs.read`）读取，注册会话鉴权 + 路径级 ACL | WP-3 |
| HTTP | `GET /api/v1/status`（同一 payload） | 监听与认证由 WP-5 定义（当前计划 127.0.0.1） | WP-5 / App |

- **采集周期**：默认 2s（uci 归 WP-4 定义）；`interval` 字段必须与实际周期一致。
- **上限（硬约束）**：整文件 **≤32KB**；`clients.items` **≤32** 条；`net.series` 每数组 **≤60** 点。
- **新鲜度预期**：文件内容滞后 ≤ `interval` + 写耗时；Dashboard 轮询 5s（见 §5）。

## 2. 顶层结构

| 字段 | 类型 | 可空 | 说明 |
| :--- | :--- | :--- | :--- |
| `v` | int | 否 | 契约版本。当前 `1` |
| `ts` | int | 否 | 采样时刻（epoch 秒） |
| `interval` | int | 否 | 采集周期（秒）；`net.series.step` 应等于它 |
| `collect_ms` | int | 是 | 本次采集耗时（毫秒，自诊断用） |
| `host` | object | **否** | §3.1（必备节） |
| `system` | object | **否** | §3.2（必备节） |
| `net` | object | **否** | §3.3（必备节） |
| `docker` | object | 是 | §3.4；整节 `null` = 不可用；`available=false` = 未安装（卡片隐藏） |
| `clients` | object | 是 | §3.5 |
| `services` | object | 是 | §3.6（**开放映射**，键 = 服务/包 id） |
| `bandix` | object | 是 | §3.7（WP-8 bridge 链路，慢刷 10s）；整节缺失 = 上游不可用（errors 记一条）；`available=false` = 未装/未开（卡片隐藏） |
| `errors` | array\<string\> | 否（可为空数组） | 采集失败项，机器可读短串，如 `"docker: socket unavailable"` |

## 3. 各节字段

### 3.1 `host`（必备）

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `hostname` | string | 主机名 |
| `model` | string | 型号/设备描述（如 `FriendlyElec NanoPi R4S`） |
| `release` | string | 发行版标识（如 `QuartzWrt 1.0.0 (ImmortalWrt 25.12)`） |
| `kernel` | string | 内核版本 |
| `uptime` | int | 运行时长（秒） |

### 3.2 `system`（必备）

| 字段 | 类型 | 单位 | 说明 |
| :--- | :--- | :--- | :--- |
| `load` | array[3] of number | — | 1/5/15 分钟负载 |
| `cpu_pct` | number | % | 上次采样以来的 CPU 占用（0–100，一位小数） |
| `mem` / `swap` / `rootfs` | object | 字节 | `{ total, used, pct }`；`pct` 0–100 一位小数；`total=0` 表示未启用（如 swap） |
| `temp_c` | number\|null | °C | 无温度传感器时 `null` |

### 3.3 `net`（必备）

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `wan.up` | bool | 在线状态 |
| `wan.ifname` / `wan.proto` / `wan.ipv4` | string | 接口名 / 协议 / IPv4（无地址时可为空串） |
| `wan.rx_bps` / `wan.tx_bps` | int | 瞬时速率（字节/秒，按 `interval` 窗口计） |
| `series` | object | 可空。`{ step, rx[], tx[] }`：环形缓冲，各 ≤60 点，`step` 秒/点；缺失时前端自累积（§5） |

### 3.4 `docker`（可空）

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `available` | bool | `false` = 未安装 dockerd → Dashboard 隐藏整卡 |
| `running` / `total` / `images` | int | 运行容器 / 容器总数 / 镜像数 |
| `unhealthy` | array\<string\> | 非健康容器名（≤8 个，超出截断） |

### 3.5 `clients`（可空）

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `total` | int | 在线设备总数（可大于 `items` 长度） |
| `items` | array | **≤32 条**，按 `last_seen` 降序；每条 `{ mac, ip, host, iface, last_seen }`（`host` 可为空串，`last_seen` 为 epoch 秒） |

### 3.6 `services`（可空，开放映射）

- 键 = 服务/包 id（如 `openclash`、`passwall`、`dockerd`、`ddns`、`bandix`、`tailscale`）；值为 `{ "running": bool, ... }`，其余字段由该服务自定义（如 `openclash.core`、`openclash.core_update`）。
- 新增服务状态 = **新增键**，不改契约版本（§6.2 加法优先）。
- 消费者义务：未知键/未知字段忽略；值为 `null` 视为不可用。

### 3.7 `bandix`（可空；慢刷 10s；2026-10-17 加法，WP-8 bridge 链路）

数据面经 Bridge 契约 `quartz.bandix`（daemon 不直连核心 8686）。字段语义与网页逐字段同源。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `available` | bool | `false` = 未装 bridge/bandix、未开 traffic 模块、或上游返回空设备表（核心不可达被 `luci.bandix` 伪造成 `{d:[]}`——正常态至少路由器自身在列）→ Dashboard 隐藏整卡（其余字段省略） |
| `devices` / `online` | int | bandix 设备表总数 / 在线数（`last` 在 `bandix.traffic.offline_timeout` 内，判据同上游状态页） |
| `wan` | object | `{ rx_bps, tx_bps }`：设备侧 WAN 速率合计（Byte/s，= Σ `w_rx_r`/`w_tx_r`，与上游 "WAN Traffic" 卡同式） |
| `top` | array | **≤8 条**，按累计 `t_rx_b+t_tx_b` 降序；每条 `{ mac, host, ip, rx_b, tx_b, pct }`（`pct` = 占全部设备累计流量比，1 位小数） |
| `conn` | object\|省略 | `{ total, tcp, udp }`；connections 模块未启用/上游不可用时省略 |
| `dns` | object\|省略 | `{ queries }`；dns 模块未启用/上游不可用时省略 |

- 整节缺失（`null`）= 上游暂不可用，`errors` 记 `"bandix: …"`；下轮自动重试。
- 消费方义务：`available !== true` 或整节缺失 → 不渲染卡片。

### 3.8 `errors`

- 机器可读短串数组，建议格式 `"<组件>: <原因>"`。消费方以弱化提示展示，不做解析。

## 4. 完整示例（基准，同时见 `dev/mock/status.json`）

```json
{
  "v": 1, "ts": 1760000000, "interval": 2, "collect_ms": 12,
  "host": { "hostname": "QuartzWrt-R4S", "model": "FriendlyElec NanoPi R4S",
            "release": "QuartzWrt 1.0.0 (ImmortalWrt 25.12)", "kernel": "6.6.94", "uptime": 306120 },
  "system": {
    "load": [0.42, 0.31, 0.25], "cpu_pct": 7.8,
    "mem":    { "total": 4294967296, "used": 1374389535, "pct": 32.0 },
    "swap":   { "total": 0, "used": 0, "pct": 0 },
    "rootfs": { "total": 1073741824, "used": 402653184, "pct": 37.5 },
    "temp_c": 46.5
  },
  "net": {
    "wan": { "up": true, "ifname": "eth0", "proto": "dhcp", "ipv4": "100.64.7.23",
             "rx_bps": 1312000, "tx_bps": 343000 },
    "series": { "step": 2, "rx": [0], "tx": [0] }
  },
  "docker": { "available": true, "running": 3, "total": 5, "images": 7, "unhealthy": ["nextcloud"] },
  "clients": { "total": 12, "items": [
    { "mac": "00:11:32:7d:55:60", "ip": "192.168.1.10", "host": "NAS", "iface": "br-lan", "last_seen": 1760000000 }
  ]},
  "services": {
    "openclash": { "running": true, "core": "mihomo 1.19.2", "core_update": "1.19.4" },
    "dockerd":   { "running": true }
  },
  "bandix": {
    "available": true, "devices": 12, "online": 9,
    "wan": { "rx_bps": 3481600, "tx_bps": 522240 },
    "top": [
      { "mac": "00:11:32:7d:55:60", "host": "NAS",    "ip": "192.168.1.10", "rx_b": 823456789, "tx_b": 123456789, "pct": 38.6 },
      { "mac": "aa:bb:cc:dd:ee:ff", "host": "laptop", "ip": "192.168.1.20", "rx_b": 456789012, "tx_b": 78901234,  "pct": 21.9 },
      { "mac": "11:22:33:44:55:66", "host": "phone",  "ip": "192.168.1.30", "rx_b": 234567890, "tx_b": 45678901,  "pct": 11.4 }
    ],
    "conn": { "total": 128, "tcp": 96, "udp": 32 },
    "dns": { "queries": 45231 }
  },
  "errors": []
}
```

## 5. 消费者义务（WP-3/WP-5）

1. **只读**：不写快照、不触发采集（v1；写操作走 WP-5 的 rpcd API）。
2. **降级四态**（DESIGN.md §2.2）：文件缺失 → "采集器未运行"；解析失败 → "数据损坏"；节/叶字段为 `null` → 卡内"不可用/—"；`errors[]` → 卡角弱化提示。任何情况不白屏。
3. **未知容忍**：忽略未知字段/未知节；未知 `v` 显示"客户端需升级"提示。老前端对新版 daemon 不得报错。
4. **轮询**：默认 5s；`document.hidden` 时**不取数**（LuCI `Poll` 自身不判断可见性——壳层自行实现），恢复可见时立即补取一次。
5. **series 缺失**时：前端自行从轮询累积（开页后逐点成形），不报错。

## 6. 生产者义务（WP-4）

1. **原子写**：临时文件 + `rename`；权限 0644；路径固定 `/var/run/quartz/status.json`。
2. **单节失败不中断**：该节置 `null`（或叶片字段置 `null`），`errors` 记一条；其余节照常。
3. **上限自守**：≤32KB / `clients.items` ≤32 / `series` ≤60（超出时截断，不超发）。
4. **字段一致**：`interval`、`series.step` 与实际采集周期一致；`ts` 为采样完成时刻。
5. **契约只增**：改契约走 §0 的变更流程并同步 `dev/mock/` 基准样例。

---

*变更记录：2026-10-12 v1 冻结（用户拍板；自 `luci-app-dashboard/docs/DESIGN.md` §3 草案转正，`collect_ms` 提升为顶层字段）。*
*2026-10-17 加法（不升版）：新增可选节 `bandix`（§3.7，WP-8 bridge 链路 P2——daemon 慢刷经 `quartz.bandix`；`available` 隐藏谓词 + `top ≤8`；`errors` 顺延为 §3.8）；§4 示例与 `dev/mock/` 同步。*

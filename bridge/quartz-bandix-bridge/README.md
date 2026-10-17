# quartz-bandix-bridge

bandix 的 **Bridge 适配层**（WP-8，TDD §5 模式 B 首个落地）。把上游 LuCI 的 rpcd 薄壳
`luci.bandix` 收敛成稳定的 ubus 契约 **`quartz.bandix`**，供 `quartz-app-api` 转发给 App；
并投放 uci-defaults 把 bandix 流量监控置为出厂默认开启。

设计规范（含选型、安全模型、跨 WP 分工）：[`docs/DESIGN.md`](docs/DESIGN.md)。

## 它解决什么

| 问题 | 做法 |
| :--- | :--- |
| App 若直连上游 `luci.bandix`：上游改名/字段漂移直达 App | 契约层隔离——对外方法名/信封/单位冻结，上游变化只改本包一处（能力探测可降级） |
| 上游把 `installUpdate`（无签名校验的 `apk add --allow-untrusted`）等动作一并暴露 | 动作面收敛：只暴露 10 个读 + 10 个写，供应链与破坏性动作不进 App 通道 |
| 上游 shell 实现无入参校验 | 本层全量白名单校验（MAC/整数范围/枚举/字符集/分页上限），不拼接 shell |
| 出厂状态 bandix 三模块全关、服务不启动 | `uci-defaults` 默认开启 traffic（只一次，防升级回放覆盖用户选择） |
| 网页端与 App 端体验不一致 | 两端落在**同一份实现**（都经 `luci.bandix` → bandix 核心内存态），数值/动作/单位一致 |

## 调用（诊断用）

```sh
# 读：设备表（等价网页「状态」页的设备列表）
ubus call quartz.bandix status

# 读：聚合限速/白名单/配额
ubus call quartz.bandix limits

# 读：版本与上游能力（上游方法名集合，缓存 300s）
ubus call quartz.bandix version

# 写：默认限速（Byte/s）——**含数值字段的方法参数收在 data 对象里**（见下）
ubus call quartz.bandix limit_default_set '{"data":{"wan_rx_rate_limit":102400,"wan_tx_rate_limit":51200}}'

# 写：设备主机名绑定（空串 = 清除绑定；纯字符串方法保持扁平参数）
ubus call quartz.bandix hostname_set '{"mac":"aa:bb:cc:dd:ee:ff","hostname":"laptop"}'
```

**为什么有的方法参数在 `data` 对象里**：rpcd 的 ucode 插件策略要求每个传入参数都在 `args` 中声明、
且 blobmsg 类型精确匹配；而传输层按数值大小自动选 int32/int64（`blobmsg_json`），单一数值类型提示
必然拒掉一半调用。因此**含数值字段的方法**（`status`/`ranking`/`series`/`connection_flows`/
`dns_queries`/`limit_default_set`/`limit_schedule_*`/`whitelist_enabled`/`quota_set`）把参数收进
`data` 对象，数值合法性由本层校验器把关（与基座 `luci-app-tailscale` 的 `form_data` 同款做法）。
纯字符串方法（`device_metrics`/`hostname_set`/`whitelist_add`/`whitelist_delete`/`quota_delete`/
`limit_schedule_delete`）保持扁平参数。

**ACL：本包不投放 `acl.d`** —— 即经 rpcd 会话（LuCI / 局域网 `/ubus`）无法调用 `quartz.bandix`，
只有本机 root 侧（SSH、或 `quartz-app-api` 以 root 转发）可调。这是刻意的：本对象没有自带认证，
若授给 `unauthenticated`，局域网任何能到达 `/ubus` 的客户端都能改限速。

## 方法表

读：`status` `device_metrics` `ranking` `series` `connections` `connection_flows`
`dns_queries` `dns_stats` `limits` `version`

写：`limit_default_set` `limit_schedule_set` `limit_schedule_update` `limit_schedule_delete`
`whitelist_add` `whitelist_delete` `whitelist_enabled` `quota_set` `quota_delete` `hostname_set`

信封：`{"ok":true,"api":1,"data":…}` / `{"ok":false,"api":1,"error":{"code","message"}}`；
错误码 `bad_request`（校验不过）/ `data_unavailable`（上游不可用或该模块未启用，message 带上游原因）/
`action_failed`（写动作失败）。字段与单位沿用上游（`t_rx_b` 字节、`t_rx_r` Byte/s、`last` epoch ms）。

**不暴露**（刻意）：`installUpdate`、`clearData`、`deleteDevice`、`checkUpdate`、`restartService`
（重启走 `quartz-app-api` 既有的 `service.restart`）。

## 单测（宿主，不需要路由器）

```sh
dev/unit-test.sh                      # 自动探测本地构建树（按宿主架构选目标 ucode）
BUILD_TREE=/path/to/immortalwrt dev/unit-test.sh
```

用锁定基座**目标架构的 ucode**（musl 二进制 + musl loader 在宿主直跑）执行「源码 + 断言体」
拼接产物；`ubus`/`log` 两个 import 在装配时替换为桩，`dev/fixture.json` 提供上游应答。
覆盖：方法表形状、动作面收敛、校验正反例、上游两形态归一、错误映射、参数转发。
真机侧（rpcd 装载、真 ubus、真 `luci.bandix`）由 VM 验收覆盖：`dev/vm-accept.sh`（**41/41 PASS**，
2026-10-17：契约装载/默认开启/真数据/与网页同源一致性/写面往返/负例/幂等/探针快照/Dashboard 产物）
与 `dev/vm-shot.py`（bandix 四页视觉证据）。

## 依赖

`ucode` + `ucode-mod-ubus`（rpcd ucode 插件机制）+ `bandix` + `luci-app-bandix`（上游实现与数据源）。
随 `quartz-defaults` 预装（x86_64 与设备适配层目标；通用 target 种子不含 quartz-defaults）。

# luci-app-tailscale（QuartzWrt 适配版）设计规范

**版本**: v0.1（定案——用户按推荐拍板五点，2026-10-13）
**上游基线**: `asvow/luci-app-tailscale` v1.2.6（`534eb3f`，GPL-3.0-only，最后提交 2025-05-09）
**依据**: WORKPLAN §3 WP-10；TDD（本文说明"为何 vendoring 停更上游而非薄 Makefile 收录"的偏离记录）

---

## 1. 背景与立项理由

- 上游已停更；基座 luci feed **无** `luci-app-tailscale`（无遮蔽问题）；基座 `tailscale` 包（feeds/packages，1.98.3）持续维护但只有最简 daemon 壳，无 LuCI 界面。
- 用户指令：以该停更项目为起点做 QuartzWrt 专门适配版。此属**我方再开发**（quartz feed 自有组件），非 thirdparty 收录，不触 TDD"不可变上游"（该约束面向第三方收集）。
- 偏离记录：vendoring（而非薄 Makefile + 补丁）的理由——上游停更且我们要做结构性改造（架构切分/安全收紧），补丁集将迅速超越原代码体量，vendoring + 明确 provenance 更可维护。

## 2. 架构（定案）：daemon 层吃基座、功能层归本包

```
基座 tailscale 包                         本包 luci-app-tailscale
├─ /usr/sbin/tailscaled                  ├─ 三个 LuCI 视图（setting/interface/log）
├─ /etc/init.d/tailscale（最简 daemon）    ├─ /usr/sbin/tailscale_helper（幂等配置器）
├─ /etc/config/tailscale（基础选项）       ├─ /etc/init.d/tailscale-quartz（boot 钩子，新文件名）
└─ /usr/sbin/tailscale（CLI）             ├─ hotplug（WAN up → apply）
                                          ├─ menu.d / acl.d（exec 白名单）/ po
                                          └─ probes.d/tailscale.json（daemon 联动）
```

**关键约束：零文件重叠。** 上游包自带 `/etc/init.d/tailscale` 与 `/etc/config/tailscale`，
与基座包同名——apk 时代同路径双属主不可行（冲突/覆盖不确定）。故：
- 本包不携带上述两个文件；
- 上游 init 的全部功能（`tailscale up` 偏好旗标、MagicDNS dnsmasq 条目、`network.tailscale`、
  子网路由、防火墙 `tszone`+forwarding、exit-node 桥接、enabled 语义）由 `tailscale_helper` 承接；
- daemon 生命周期：`helper` 需要时调 `/etc/init.d/tailscale start`（基座 init，可重复调用）；
  `enabled=0` 时由 helper `stop` 收口（基座 init 开机无条件拉起，`tailscale-quartz` START=95 在其后运行）。

## 3. 相对上游的改动清单

| 项 | 上游 v1.2.6 | 本包 | 理由 |
| :-- | :--- | :--- | :--- |
| init/config 载体 | 自带（与基座冲突） | 不带；boot 钩子新文件名 | 零文件重叠（§2） |
| 配置应用时机 | procd 实例 `tailscale up --reset "$@"`（每次重启全量重放） | `tailscale_helper apply`：`Running` → `tailscale set`（增量、不擦其他偏好）；`NeedsLogin` → `up --authkey`（或等交互登录） | `--reset` 会清掉未在 uci 中的偏好；set 更稳 |
| 幂等性 | 只增不减（如子网路由改短后残留） | 全部 reconcile（先清我方命名段再重建；比较后写） | 重复 apply 无漂移；缩配置可正确回退 |
| 回退 | stop 里 grep/索引推断要删的段 | 隐藏状态记录（`_ts_dns_suffix`/`_ts_lanwan_fwd`）驱动 cleanup | 离线（daemon 已停）也能清干净 |
| `enabled=0` | init 不启动（但**删 state/unbind**） | 停 daemon + 清胶水，**保留 state/身份**（unbind 走登出按钮） | 禁用≠解绑；下次启用免重登 |
| 自由 `flags` 直通 | UI 提供，原样拼进命令 | **移除**（改显式白名单项 `ssh`/`shields_up`） | 注入面/任意旗标 |
| `authkey` | UI 写 `authKey`、init 读 `authkey` → **实际断链** | 统一 `authkey`；密码掩码 + 明文警示 | 修 bug + 安全提示 |
| 值校验 | 无 | hostname/token/CIDR 字符集校验；非法值拒用并记 logd | 参数安全 |
| `config_path` 选项 | UI 可改（驱动 state 位置） | 移除（state 位置归基座 `state_file`） | 与基座 config 单一来源 |
| ACL | ip/logread/tailscale 三命令 exec + uci | 同款 + `tailscale_helper apply/cleanup` 白名单 | 最小新增面 |
| 联动 | 无 | `probes.d/tailscale.json` 投放（procd: tailscale；extras.ip=`tailscale ip -4`） | daemon 零重编可见（WP-4 §13 机制） |

## 4. 与 QuartzWrt 的集成

- **主题**：三视图在 `luci-theme-quartz` 下逐页测试（复用 WP-2 `dev/vm-test/` 工具链）。
- **App 联动**：App 远程引导（W6.5）指到本页；App 应用页经快照 `services.tailscale` 显示状态。
- **默认集**：**不预装**（用户拍板）；经 App 引导或 `apk add`。

## 5. 验收（VM；真 tailnet 归真机阶段）

1. VM `apk add tailscale luci-app-tailscale`（VM 有外网；kmod-tun 依赖自动）；
2. 三视图逐页截图（`NeedsLogin` 态渲染；zh/en i18n）；
3. `tailscale login` 真实产生 auth URL（展示流验，不完成授权）；
4. `helper apply` 幂等核对：连续 apply×2（uci 无漂移、无重复监听）；改 `access`/`subnet_routes`/`accept_routes` 后核对防火墙/network/dnsmasq 变化；`enabled=0` → daemon 停 + 胶水净；
5. **authkey 链路**（可选，需用户提供一次性 key 或自建 headscale）：`up --authkey` 自动登录；
6. 联动：快照 `services.tailscale` 出现（daemon 侧零改动）。

## 6. 变更记录

*v0.1 2026-10-13 —— 定案：架构切分（daemon 吃基座/功能归 helper）、安全收紧清单、验收路径；用户按推荐拍板（版本 1.3.0 血缘 / 保留 enabled / 保留 authkey 掩码 / 不预装 / WP-10 立项）。*

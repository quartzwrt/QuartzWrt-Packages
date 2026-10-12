# quartz-app-api 设计规范

**版本**: v1.0.1 —— **定案**（2026-10-12 用户拍板「按推荐案实施」；v1.0.1 为实施期机制修正，见变更记录）
**日期**: 2026-10-12
**状态**: 定案并开工（W5.1 起实施，跟踪见 WORKPLAN §2/§3）
**依据**: TDD §3.4（认证与 API 隔离）/ §3.2（元包）/ §5（模式 A/B）/ §8（App 推送待决）；WORKPLAN §3 WP-5；
WP-4 `quartz-daemon/docs/DESIGN.md` §12（App 承诺与 seam）/ §12.4（控制面归属与动作路径表）；
数据契约 `luci/applications/luci-app-dashboard/docs/AGGREGATE-API.md`（冻结 v1）
**验收（WORKPLAN §3 WP-5）**: 认证、吊销、权限边界按 TDD §3.4 验证通过
**核实口径**: §1 每行标注「源码」（锁定基座构建树路径）或「VM 实测」（QEMU 预览台，SSH 2222 / HTTP 8080）；
未标注出处者为设计建议（实现前按 WP-4 惯例逐项复核）。

---

## 0. 定位与范围

`quartz-app-api` 是 **专属 App 的唯一对外 API 与认证边界**（TDD §3.4「App 权限统一收敛到 quartz-app-api」）：

- **数据面**：认证后代理 `quartz-daemon` 回环 HTTP 快照（同一数据模型，App 与 Dashboard 严格同源）；
- **控制面**：白名单动作执行（重启服务/容器、系统重启等；归属见 WP-4 DESIGN §12.4）；
- **发现面**：mDNS 服务宣告（umdns），供 App 局域网自动发现；
- **认证面**：短时 JWT（access）+ 旋转刷新令牌（refresh），支持每设备吊销与轮换。

**v1 明确不做**（防范围蔓延）：

| 不做 | 理由 / 归属 |
| :--- | :--- |
| 推送消息发送 / 事件订阅 | TDD §8 待决项；daemon 事件检测 seam 已冻结（WP-4 §12.2），通道定案前不做 |
| 用户体系（多账号/权限分级） | 单管理员模型（root/系统密码），与 LuCI 一致；多账号需求出现再评估 |
| 长效 API Key | TDD §3.4 为「JWT **或** API Key」二选一；v1 取 JWT+refresh，API Key 列为后置可选项（D2） |
| OTA / 固件升级触发 | WP-9 |
| TLS 证书管理 UI / ACME | 自签 + 指纹 pinning（D4）；证书续期策略见 §6 |
| 写操作直通（任意 ubus 透传） | **禁止**：动作只经白名单表（§5）；任意透传=把 root ubus 面暴露给凭据层 |
| App 专属字段 / 私有端点 | 契约纪律（TDD 双端同源），单端需求走加法流程 |

## 1. 事实核实（2026-10-12）

### 1.1 rpcd 机制（源码：`build/immortalwrt/build_dir/target-x86_64_musl/rpcd-2026.10.04~d99f703e`）

| 事实 | 值 / 行为 | 对设计的影响 |
| :--- | :--- | :--- |
| 版本 | `2026.10.04~d99f703e`（与 WP-4 坑位 #25 同源） | ACL 行为以此为准 |
| exec 插件协议 | 插件=可执行文件放 `/usr/libexec/rpcd/`；启动时 rpcd 执行 `<插件> list`（stdout 输出 JSON 方法表 `{"method":{"arg":"type"}}`）注册为 ubus 对象；**对象名 = 文件名**（`plugin.c: obj->name = strdup(name)`） | 文件名必须恰为 `quartz-app-api`（ACL 对象名同名） |
| 调用方式 | 每次调用 fork+exec `<插件> call <method>`，参数 JSON 经 **stdin**，回复从 **stdout** 读单个 JSON 对象（`plugin.c: rpc_plugin_call`） | 无常驻进程；状态入文件；每次调用进程启动开销（Go 二进制毫秒级） |
| 插件身份 | rpcd 子进程 → **root** | 插件内可执行 ubus CLI / init 脚本（白名单之下） |
| 错误传播 | `rpc_plugin_call_finish_cb`：**stdout 为合法 JSON 对象即 `UBUS_STATUS_OK`，子进程退出码不影响回复**；stderr 被吞（`rpc_plugin_call_stderr_cb` 直接丢弃） | **错误必须 in-band**（§4.3）；审计日志走 `logger`（logd），不能靠 stderr |
| exec 超时 | 默认 120s（`RPC_EXEC_DEFAULT_TIMEOUT`，`rpcd -t` 可调） | 插件内所有子操作须远小于此（§4.4 超时表） |
| reload 语义 | SIGHUP → `rpc_session_freeze()` + 退出重入（`RPC_HANGUP=1` → `rpc_session_thaw()`）→ **重扫插件目录且不丢会话**（`main.c: handle_signal / exec_self`） | 运行时安装新插件需 `/etc/init.d/rpcd reload`；烘焙镜像无此问题（§7.4） |
| ACL 检查链 | uhttpd `/ubus` 前置调 `session.access(sid, scope, object, function)`；rpcd 按该会话 ACL 表判定（`session.c: rpc_session_acl_allowed`、uhttpd `ubus.c: uh_ubus_allowed`） | ACL 是 rpcd 原生机制，本设计不自造鉴权面 |
| 默认会话 | rpcd 启动时建立 sid=`0000…0` 的默认会话；其 ACL = **全部 acl.d 文件中组名 `unauthenticated` 的条目**（`rpc_login_test_permission(login=NULL)`；timeout=0 → **永不过期**，`rpc_touch_session` 仅对 `timeout>0` 生效） | App 无 rpcd 会话即可调用本对象（ACL 授权见 §1.2） |
| unauthenticated 组来源 | rpcd 自带 `unauthenticated.json` 授 `session:[access,login]`；**同名组可由任意 acl.d 文件追加条目**（逐文件合并，`rpc_login_setup_acl_file`） | 本包 acl.d 以同名组追加 `quartz-app-api` 对象方法授权 |
| session.login | 默认会话即可调（实测见 §1.3）；credential 校验走 `/etc/config/rpcd` login 段（root + 哈希） | 插件内用它做「凭据校验」，校验后立即 destroy 探测会话 |
| rpcd 配置（VM） | `option socket /var/run/ubus/ubus.sock`；`config login root`（read/write `*`） | 与既有一致，本设计不改 |

### 1.2 uhttpd 机制（源码：`build_dir/.../uhttpd-2026.08.24~373145f7` + VM 配置）

| 事实 | 值 / 行为 | 对设计的影响 |
| :--- | :--- | :--- |
| /ubus 端点 | `mod_ubus`（`/usr/lib/uhttpd_ubus.so`，VM 已装 `uhttpd-mod-ubus`）；JSON-RPC `params=[sid, object, function, args]`；POST ≤64KB（`UH_UBUS_MAX_POST_SIZE`）；错误码表 `-32002 Access denied` 等（`ubus.c: json_errors`） | App 的全部调用走此端点；快照 32KB 上限内无虞 |
| 多实例 | 每个 `config uhttpd <name>` 段 = 一个 procd 实例（独立 listen/cert/ubus_prefix…） | 专用 `app` 实例（D3） |
| no_ubusauth | `-a` 跳过 session.access 检查（不鉴权） | **不用**（会暴露全部 ubus 对象）；保持 ACL 检查开 |
| TLS 证书生成 | 实例配置 `listen_https` 且 cert 缺失时，uhttpd init 自动生成（优先 `px5g`（`/usr/sbin/px5g`），回退 `openssl req`）；输出 **DER**、默认 397 天、`CN=SAN commonname`（当前配置段 `config cert 'defaults'`：CN=ImmortalWrt、ec P-256） | fp 可直接 = `sha256sum` 证书文件（§6.2）；需补 px5g 依赖（VM 无 px5g/openssl → 当前无 `/etc/uhttpd.crt`） |
| TLS 前置 | 需 `libustream-ssl`（VM 已装 `libustream-openssl20201210`） | 依赖已满足 |
| VM 监听现状 | 80（uhttpd 主实例，`ubus_prefix /ubus` 已开）、22、53、7681、127.0.0.1:8899（daemon）；**443 空闲** | App 实例落 443 |
| script_timeout（VM 主实例）=3600 | uhttpd 对 session.access 检查的 invoke 超时 = script_timeout×500ms | app 实例设 60（=30s 上限），避免检查环节长挂 |

### 1.3 VM 实测（curl → `http://127.0.0.1:8080/ubus`，即 VM:80）

```
① 默认 sid 调 file.read        → {"error":{"code":-32002,"message":"Access denied"}}   ← ACL 生效，非白名单对象不可及
② 默认 sid 调 session.access   → [0,{"access":false}]                                  ← unauthenticated 组内；查询本身可用
③ 默认 sid 调 session.login    → [0,{"ubus_rpc_session":"eefb…","timeout":300,"acls":{…}}] ← 凭据通道可用
④ SSH CLI：ubus call session login {"username":"root","password":"quartz"} → 同 ③        ← 插件内嵌套调用通道可用
```

结论：**「无回报 rpcd 会话 + unauthenticated 组授权本对象 + 插件内自校验 JWT」的方案在锁定基座上成立**（比较：给 App 铸造 rpcd 会话的方案需要第二套凭据/授权逻辑，且与 TDD「摒弃短效 session cookie」相悖，否决）。

### 1.4 其他

| 项 | 事实 | 出处 |
| :--- | :--- | :--- |
| umdns | 基座包（`package/network/services/umdns`）；**未预装**；服务定义 = `/etc/umdns/*`（JSON，schema：`instance?/service/port/txt[]/hostname?`，见 mdnsd `service.c` policy）；init 自动加 lan 5353/udp 防火墙规则；procd trigger `instance.update`（5s）触发 `ubus call umdns update` | 源码 + 包内 files |
| px5g | 虚拟包 `px5g`（`px5g-openssl` / `px5g-mbedtls` / `px5g-wolfssl` 均 `PROVIDES:=px5g`）；二进制 `/usr/sbin/px5g` | 源码 |
| Tailscale | `feeds/packages/net/tailscale` v1.98.3（uci `/etc/config/tailscale`，`fw_mode nftables` 默认；**luci feed 无 luci-app-tailscale**） | 源码 |
| ZeroTier | `feeds/packages/net/zerotier` + `feeds/luci/applications/luci-app-zerotier` | 源码 |
| 防火墙（VM） | lan zone `input ACCEPT`（443 无需规则即达）；wan REJECT；无 tailscale 区域 | VM `uci show firewall` |
| daemon 上游 | `127.0.0.1:8899` `GET /api/v1/status`；并发安全 / 同字节 / 降级合法 JSON 五项承诺 | WP-4 DESIGN §5/§12.1 |
| 时间 | VM 有 NTP；无 RTC 机型开机钟不可信 | WP-4 事实 + 通用 |

## 2. 架构与选型

### 2.1 链路（推荐案）

```
Flutter App
   │ ① mDNS 发现（_quartz-app._tcp，TXT: v/fp/path/rel）——局域网
   │ ② HTTPS 443（自签 + 指纹 pinning）
   ▼
uhttpd 'app' 实例（TLS-only，ubus_prefix=/ubus，独立 procd 实例）
   │ ③ POST /ubus JSON-RPC [默认 sid, "quartz-app-api", method, args{..., access_token}]
   ▼
rpcd（2026.10.04）── session.access 预检（默认会话 → unauthenticated 组 → 仅本对象方法）
   │
   ▼
quartz-app-api（exec 插件，root；每调用一进程）
   ├─ 认证：JWT 校验（HMAC 密钥 / 设备注册表 / 限速）+ refresh 旋转 + 审计（logd）
   ├─ 数据：GET http://127.0.0.1:8899/api/v1/status（1s 超时）→ 原样内嵌返回
   └─ 动作：白名单表 → ubus CLI / init 脚本（无 shell 拼接）
                     ▲
        （模式 B 动作未来经 WP-8 Bridge 的 ubus 对象接入本表；模式 A 直调）
```

### 2.2 选型对比

| 案 | 形态 | 优点 | 缺点 | 结论 |
| :--- | :--- | :--- | :--- | :--- |
| **案 1（推荐）** | **rpcd exec 插件 + uhttpd 专用实例** | 复用 rpcd ACL 机制（TDD §3.4 原文）/ uhttpd TLS / ubus 传输统一；无常驻进程（零常驻内存）；与 daemon「零依赖 + CLI 调 ubus」同构；ACL 收敛 = 一个 ubus 对象 | 每调用 fork 一次（毫秒级）；错误 in-band；状态需落文件 | ✅ |
| 案 2 | 独立 Go HTTP 服务（自持 TLS/JWT，直连 ubus socket） | 完全掌控；可长连接/SSE（事件 seam 友好）；无 rpcd 学习成本 | 自造第二套 HTTP/TLS/证书/防火墙面；**绕过 rpcd ACL 机制（与 TDD 分歧，需用户批准）**；多一个常驻进程 | ✗（可作事件推送阶段的演进备选） |
| 案 3 | uhttpd CGI/ucode 脚本 | 无二进制 | 每请求 fork shell + 无 JWT 生态 + 弱并发；校验/审计代码难以测试 | ✗ |

### 2.3 认证分层（为什么不是「把 rpcd sid 给 App」）

- App 持**自有凭据**（JWT/refresh），不接触 rpcd 会话；rpcd 会话面（默认会话）只是 ACL 通道，不含凭据。
- 不做「插件铸造 rpcd 会话返回 App」（session.create+grant 可行但）：
  ① 会话表在 rpcd 内存，reload/重启语义另成一套；
  ② 等于两套授权逻辑（grant 表 + 设备表）需要保持同步；
  ③ TDD §3.4 明确摒弃 session cookie 路线。
- 「统一收敛到 quartz-app-api」的落地即：**App 的 ubus 可达面 = 恰好一个对象的方法集**（§7.2 acl.d），其余全被 rpcd 拒绝（§1.3 ①实测）。

## 3. 认证模型

### 3.1 凭据

| 凭据 | 形态 | 有效期（可配） | 存储（设备端） | 存储（App 端） |
| :--- | :--- | :--- | :--- | :--- |
| access_token | **JWT HS256**：`{iss:"quartz-app-api", sub:<device_id>, iat, exp, jti}` | 15 min | 不存（无状态校验 + 设备表复核） | 内存（进程内存，不落盘） |
| refresh_token | **opaque** 32B 随机（base64url）；设备端只存 **SHA-256 哈希** | 30 天滑动 / 180 天绝对上限 | `/etc/quartz-app-api/state.json`（0600） | 安全存储（Keychain/Keystore） |
| HMAC 密钥 | 32B 随机 `secret` | 长期（重置即全员失效） | `/etc/quartz-app-api/secret`（0600） | — |

- **每次调用**同时校验：JWT 签名/时限 → `sub` 设备在注册表且未吊销 → 限速/锁定状态。
- **refresh 旋转**：每次 refresh 换发新 refresh（旧的立即作废）；**复用检测**（旧 refresh 被再次使用）→ 判定泄露 → 吊销该设备并审计。
- **logout** = 删除设备记录（含 refresh）；**revoke** = 停止其他设备；**revoke_all** = 清空注册表 + 换 `secret`（全员强制重配对）。
- JWT 是 TDD 「短时有效 + 刷新机制」的直译；长效 API Key 见 D2（后置）。

### 3.2 时钟策略（无 RTC 机型）

- 校验 `exp` 允许 ±5 min 偏斜；`iat > now`（时钟回拨）→ 视为 `token_invalid`，App 走 refresh（refresh 不依赖时钟：凭据有效性由注册表 + 服务器单调计数判定）。
- `login` 响应携带路由器当前时间（`router.ts`）——App 可检测到严重偏差并提示「路由器时间未同步」，避免把时钟问题误报成认证故障。
- 设备端时钟未同步期（开机到 NTP 之间）不影响登录/刷新主链；仅 access 的 exp 判读放宽（注册表复核兜底）。**这是选「opaque refresh + 设备表复核」而不是纯无状态 JWT 的原因。**

### 3.3 防爆破与审计

- 登录限速（**v1.0.1 机制修正**：uhttpd `/ubus` 端点**不向插件转发客户端 IP**——源码核实仅注入 `ubus_rpc_session`，per-IP 键不可实现；v1 采用**全局键**：任意来源失败统一计数，对单管理员口令场景等价且更强〔不可绕〕，per-IP 维度待机制出现再启用）：默认 5 次失败 → 锁定 5 min（指数退避 ×2，上限 64×）；成功登录清零。限速状态落 `/var/run/quartz-app-api/ratelimit.json`（tmpfs，重启清空可接受）。
- 令牌比较恒定时间；错误统一返回 `invalid_credentials`（不区分「用户不存在/密码错误」）。
- 审计（logd 短串，机器可读）：`login ok/fail ip=<ip>`, `refresh rot/deny`, `revoke <device>`, `action <id> <result>`；不记录任何凭据材料。

### 3.4 存储布局

```
/etc/quartz-app-api/            (0700 root)
├── secret                      32B 随机 HMAC 密钥（0600；不存在则首调用生成）
└── state.json                  设备注册表（0600；原子写 .tmp+rename + flock）
     {"devices":[{ "id":"a1b2…", "name":"iPhone 15", "created":1697…, "last_seen":…,
                   "last_ip":"192.168.1.23", "refresh_sha256":"…", "refresh_exp":…,
                   "rot_counter":3, "revoked":false }]}
/var/run/quartz-app-api/        (0700；tmpfs)
└── ratelimit.json              限速/锁定状态（丢之无害）
```

## 4. API 面（ubus 对象 `quartz-app-api`）

### 4.1 方法表

| 方法 | 认证 | 参数 | 返回（成功信封） | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| `info` | 无 | `{}` | `{ok,api,name,model,release,kernel,features[]}` | 配对前展示路由器身份（mDNS 兜底） |
| `login` | 凭据 | `{username,password,device_name,client_ip?}` | `{ok,api,access_token,access_exp,refresh_token,refresh_exp,device_id,router{…,ts}}` | 用户名/密码=系统管理员（rpcd login 校验，§1.1）；限速+审计；`client_ip` 为 App 自报的展示元数据（不参与安全决策，v1.0.1） |
| `refresh` | refresh | `{device_id,refresh_token}` | 同 login（换发两枚令牌；旋转） | 复用检测→吊销（§3.1） |
| `logout` | refresh | `{device_id,refresh_token}` | `{ok,api}` | 删设备记录 |
| `status` | access | `{access_token}` | `{ok,api,snapshot:{…AGGREGATE-API v1 原文…}}` | 代理 daemon 回环 HTTP；语义同构（经 ubus JSON 重序列化，非字节级） |
| `action` | access | `{access_token,action,args{}}` | `{ok,api,action,result{}}` | 动作白名单见 §5 |
| `devices` | access | `{access_token}` | `{ok,api,devices:[{device_id,name,created,last_seen,current}]}` | 已配对设备清单 |
| `revoke` | access | `{access_token,device_id}` | `{ok,api}` | 踢出指定设备（含自身=登出） |

### 4.2 信封

```json
{"ok": true,  "api": 1, …载荷…}
{"ok": false, "api": 1, "error": {"code": "invalid_credentials", "message": "…", "retry_after": 300}}
```

- `api` 为主 API 版本（`info.api`）；App 不识别时提示升级（契约 §5 同款纪律）。
- **in-band 错误是 rpcd exec 插件的机制约定**（§1.1），App 判 `ok` 字段而非 HTTP/JSON-RPC 状态。

### 4.3 错误码（v1 全表）

| code | 触发 | App 侧行为 |
| :--- | :--- | :--- |
| `invalid_credentials` | 密码错/设备未知 | 提示重试（计限速） |
| `rate_limited` | 触发锁定（附 `retry_after`） | 倒计时提示 |
| `token_expired` | access 过期 | **静默 refresh 后重试**（不是重新登录） |
| `token_invalid` | 签名/格式/时钟回拨异常 | 强制重新配对 |
| `device_revoked` | 设备被吊销 | 强制重新配对 |
| `forbidden_action` | 不在白名单 | 提示不可用 |
| `action_failed` | 执行失败（附 `detail`） | 展示错误 + 刷新快照 |
| `data_unavailable` | daemon 未运行/读取失败 | 降级横幅（与 Dashboard 四态同语义） |
| `bad_request` | 参数缺失/非法 | 客户端 bug（上报日志） |

### 4.4 超时预算

| 环节 | 上限 | 说明 |
| :--- | :--- | :--- |
| daemon 取数 | 1s | 回环、内存响应，正常 <10ms |
| 凭据校验（嵌套 `ubus call session login`） | 3s | 仅 login 路径 |
| 动作执行（init/ubus 调用） | 5s | reboot 返回即算成功 |
| 插件总时长 | <10s | 远低于 rpcd exec 120s |

## 5. 控制面（动作白名单）

机制：`action` 表 = **代码内注册表**（id → 校验器 + 执行器）；参数只按**结构校验 + 字符集白名单**，绝不拼接进 shell；执行走 `ubus call` CLI 或 `/etc/init.d/<name> <op>`（`<name>` 须 ∈ 已装 init 脚本 ∩ 允许集合）。

| action | 参数 | 机制 | v1 |
| :--- | :--- | :--- | :--- |
| `service.start` / `service.stop` / `service.restart` | `{name}` | `/etc/init.d/<name> <op>`；默认允许集：`openclash` `dockerd` `dnsmasq` `quartz-daemon` `tailscale` `zerotier`（uci 可调）；`network`/`dropbear` 等自断链路项默认**不**入集 | ✅ |
| `system.reboot` | — | `ubus call system reboot` | ✅ |
| `docker.container.start/stop/restart` | `{id}` | `docker container <op> <id>`（字符集校验；docker 未装 → `action_failed: docker unavailable`） | ✅（存在即用） |
| 核心/规则更新触发（TDD §4.4） | — | 归属 WP-7/WP-8 链路；**版本状态由 daemon `services` 报告**（已有），触发器后置 | ✗ 后置 |
| bandix 限速 / DDNS 配置（模式 A/B） | — | 模式 A 直调 ubus / 模式 B 经 WP-8 Bridge；经本表「ubus 转发项」接入的机制 v1 预留、不实现 | ✗ 后置 |

- **控制后可视化**：动作成功 → daemon 快照 ≤2s 反映新状态（WP-4 §12.4 承诺），App 无需专门回读。
- **能力可见性同源**：App 依据 `status.snapshot.services`/`docker.available` 决定显示哪些控制入口（数据与动作判定共用一份事实）。

## 6. 传输与暴露面

### 6.1 uhttpd 专用实例（`config uhttpd 'app'`，由本包 uci-defaults 幂等注入）

```
config uhttpd 'app'
	list listen_https '0.0.0.0:443'
	list listen_https '[::]:443'
	option cert '/etc/uhttpd.crt'        # 复用主证书（§6.2）
	option key  '/etc/uhttpd.key'
	option ubus_prefix '/ubus'
	option script_timeout '60'           # session.access 预检上限 = 30s
	option max_requests '16'
	option no_ubusauth '0'               # 保持 ACL 检查（绝不置 1）
	# 不配 cgi/lua/ucode 前缀：本实例仅承载 /ubus（+静态回退）
```

- 443 在 VM 空闲；lan zone 已是 `input ACCEPT` → **局域网无需新增防火墙规则**。
- `no_ubusauth` 一旦置 1 会跳过全部 ACL（任何对象可调），**设计红线**。

### 6.2 TLS 与指纹

- 证书：uhttpd 首次以 https 启动时自动生成（px5g，DER，397 天，CN/SAN=commonname；本包 DEPENDS 补 `px5g`，并将 cert 段 commonname 调为 `QuartzWrt`）；与 LuCI（如未来启用 https）共用同一对文件。
- **pin = `sha256sum /etc/uhttpd.crt`（DER 字节）**——与 App 在 TLS 握手中取到的证书字节直接可比，无格式歧义。
- 有效期 397 天：过渡期 App 提示「证书已过期/需重新配对」；自动续期（再生成后如何保 pin）为**后置议题**（D4 备选：pin 改 SPKI 并支持新旧双 pin 过渡窗）。
- 重刷/重置固件 → 证书重新生成 → App 检测 pin 变化 → **拒绝自动重连**，要求重新配对（防降级 MITM）。

### 6.3 mDNS（umdns）

`/etc/umdns/quartz-app.json`（本包 init 脚本在启动与证书变化时生成）：

```json
{ "service": "_quartz-app._tcp.local", "port": 443,
  "txt": ["v=1", "path=/ubus", "fp=<sha256>", "rel=<release>", "name=<hostname>"] }
```

- init 流程（START=99，uhttpd 之后）：确保 `secret`/状态目录 → 证书存在则算 fp 写入（缺失则触发一次 uhttpd restart 后重试）→ `ubus call umdns update`（umdns 运行时）。
- umdns 未装/未运行时：**静默跳过**——App 仍有手动地址兜底（D5）。

### 6.4 暴露面与残余风险（明示）

| 面 | 暴露 | 论证 |
| :--- | :--- | :--- |
| 主实例 80 的 `/ubus` | 同可调 `quartz-app-api` 方法（unauthenticated 组是全局的，无法按实例区分） | 与「LuCI 登录本就走 80」相同面：登录仍需管理员密码 + 限速；**App 强制 https**；文档化 |
| 443/TLS | 局域网 + （并入策略后的）tailnet 可达 | 凭据层为 JWT；443 面方法集=白名单八方法 |
| 插件 root 权限 | 插件内可执行 ubus/init（白名单约束） | 与 Bridge（TDD §5 模式 B）同构：**校验在插件内、能力最小化**；无任意透传端点 |
| `/etc/quartz-app-api/state.json` | 含 refresh **哈希**（非明文）+ 设备元数据 | 0600；泄露不直接可用 |

## 7. 打包与集成

### 7.1 仓库布局

```
utils/quartz-app-api/
├── Makefile
├── README.md                       # 接口速查 / 配对流程 / 排障（W5.6）
├── docs/DESIGN.md                  # 本文件
├── src/                            # Go module（in-tree；go 1.22；零第三方依赖）
│   ├── go.mod
│   ├── cmd/quartz-app-api/main.go  # rpcd exec 插件入口（list / call 协议）
│   └── internal/{token,store,actions,daemonfwd,ratelimit}/
├── files/
│   ├── quartz-app-api.init         # boot：状态目录/secret/mDNS 宣告（无常驻进程）
│   ├── quartz-app-api.conf         # /etc/config/quartz-app-api 默认
│   ├── acl.d/quartz-app-api.json   # rpcd ACL（unauthenticated 组追加本对象）
│   └── uci-defaults/99-quartz-app-api  # 注入 uhttpd 'app' 实例 / cert CN / umdns 启用
└── dev/                            # 冒烟与验收脚本（复用 vm-ssh.py 模式）
```

### 7.2 rpcd ACL 文件（`/usr/share/rpcd/acl.d/quartz-app-api.json`）

```json
{
  "unauthenticated": {
    "description": "QuartzWrt App API (JWT-gated; see docs/DESIGN.md)",
    "read": { "ubus": { "quartz-app-api": [
      "info","login","refresh","logout","status","action","devices","revoke" ] } }
  }
}
```

（与 rpcd 内置 `unauthenticated.json` 同名组合并，§1.1；`session.login/access` 行为不因此改变。）

### 7.3 Makefile 要点

```makefile
PKG_NAME:=quartz-app-api
PKG_VERSION:=0.1.0
GO_PKG:=github.com/quartzwrt/quartz-app-api
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/quartz-app-api
GO_PKG_INSTALL_BIN_PATH:=/usr/libexec/rpcd     # 插件目录 = ubus 对象名来源
DEPENDS:=$(GO_ARCH_DEPENDS) +uhttpd +uhttpd-mod-ubus +rpcd +px5g +umdns
# files/: bin + acl.d + init + conf + uci-defaults（install 段逐一 INSTALL_*）
```

### 7.4 集成点

- `meta/quartz-defaults/Makefile` **追加** `+quartz-app-api`（共享文件追加式，WORKPLAN §4）；
- **运行时安装**（`apk add`）后需 `/etc/init.d/rpcd reload` 才会注册插件（SIGHUP 重扫、会话不丢，§1.1）；烘焙镜像天然无此问题；apk post-install 自动 reload 的可行性列入 W5.3 实测；
- `uhttpd`/`umdns` 的 uci 变更集中在**本包 uci-defaults**（幂等、仅追加），不修改基座文件本体；
- daemon 侧**零改动**（HTTP 上游已就绪，WP-4 §5/§12.1）。

## 8. 测试与验收（对齐 WORKPLAN：认证 / 吊销 / 权限边界）

**单元测试（host `go test ./...`）**：JWT 签发/校验/偏斜/回拨；refresh 旋转与复用检测；注册表原子写 + flock；限速状态机；动作表校验（名称字符集/白名单/注入负例）；信封与错误码序列化。

**VM 集成验收**（QEMU + `test-in-vm.sh`，需为 443 增补宿主转发 `APP_PORT`，见 §10 同步项）：

1. **认证**：`info`（无凭据）→ `login`（错密码→`invalid_credentials`；连错→`rate_limited`；正密码→双令牌）→ `refresh` 换发 → 旧 refresh 复用→设备吊销 → 重登录。
2. **吊销**：`devices` 列表 → `revoke` 第二设备 → 其 `status` 得 `device_revoked`；`logout` 后设备从列表消失。
3. **权限边界**：默认 sid 调 `file.read` → Access denied（对照组）；仅 `quartz-app-api` 对象可达；篡改 JWT → `token_invalid`；无 access 调 `status` → 拒绝。
4. **数据面**：`status` 返回与 `127.0.0.1:8899`（及 Dashboard 渲染）同构；停 daemon → `data_unavailable`。
5. **发现与传输**：`/etc/umdns/quartz-app.json` 生成 + fp 与 `sha256sum /etc/uhttpd.crt` 一致；`curl --insecure` 直连 443 /ubus 全链通；证书 pin 校验脚本（dev/）通过。
6. **动作**：`service.restart openclash` 后 ≤2s 快照 `services.openclash.running` 变化；`system.reboot`（VM 内验证触发即返回）；越权 action → `forbidden_action`。

证据落 `dev/`（脚本 + 输出），截图沿用 Dashboard 的 Playwright 仅当涉及 UI（本 WP 无 UI）。

## 9. 决策记录（v1.0 定案：十项按推荐案实施）

| # | 决策 | 定案 | 备选与理由 |
| :--- | :--- | :--- | :--- |
| D1 | 运行时形态 | **rpcd exec 插件（Go）+ uhttpd 专用实例**（§2.2 案 1） | 独立 Go HTTP 服务：自造第二套传输/证书面且与「基于 rpcd」分歧，仅作事件推送期备选 |
| D2 | 凭据模型 | **JWT(15min) + refresh(30d 滑动/180d 绝对，旋转+复用检测)**；长效 API Key 后置 | 纯 API Key：无法短时收敛、撤销粒度粗；TDD 为「或」，v1 取 JWT 达标 |
| D3 | 暴露实例 | **专用 `app` 实例（443，TLS-only）** | 主实例加 443：少一个实例但管理面与应用面同口；残面（80）两案相同（§6.4） |
| D4 | 证书与信任 | **复用 uhttpd 自签证书，pin=DER SHA-256；配对时 TOFU + 管理员密码** | 配对码（6 位 SAS）v1.1；SPKI pin 过渡窗后置（续期议题） |
| D5 | 发现 | **umdns 服务文件（TXT: v/path/fp/rel/name）** | 纯手动地址：UX 退化；mDNS 缺 fp 时 App 标注「未验证指纹」 |
| D6 | 数据面通路 | **代理 daemon 回环 HTTP（1s 超时，错误 in-band）** | 直读 `/var/run/quartz/status.json`：绕开 daemon 对外通道声明、复刻 rpcd ACL 坑位 #25 的等价风险 |
| D7 | 控制面 v1 范围 | **service.{start,stop,restart}（白名单）+ system.reboot + docker 容器三动作** | 一并做核心更新/限速：依赖 WP-7/8 链路，易返工 |
| D8 | 令牌/吊销参数 | 上述默认值全部 **uci 可调**；`revoke_all` 换密钥 | — |
| D9 | 事件/推送 seam | **v1 不实现**；方法位形状冻结（未来 `events` 拉取或 daemon webhook，TDD §8 定案后择一） | 现在做 webhook：通道未定（WP-4 §9-D8 同款理由） |
| D10 | 防爆破/审计 | **全局 5 次/5min 锁定 + 指数退避（v1.0.1 修正：/ubus 不转发客户端 IP）；logd 审计短串** | 更严（per-IP）等机制出现再启用 |

## 10. W5.x 分解（拍板后）

| 序号 | 内容 | 依赖 |
| :--- | :--- | :--- |
| W5.0 | 本设计定案（决策记录落定） | 用户拍板 |
| W5.1 | 骨架 + 插件协议打通：Go 二进制 `list/call`、acl.d 生效实测（默认 sid 可见对象/不可见他对象）、exec 内嵌套 `session.login` 往返与并发验证 | W5.0 |
| W5.2 | 认证层：secret/注册表/限速 + login/refresh/logout/revoke + 审计 | W5.1 |
| W5.3 | 暴露层：uhttpd `app` 实例 + 证书生成 + uci-defaults + init（含 mDNS 宣告）+ 打包安装 + 运行时安装 reload 实测 | W5.1 |
| W5.4 | 数据面 `status`（daemon 代理）+ 控制面动作表 | W5.2 |
| W5.5 | 单测 + VM 集成验收（§8 六组）+ 证据归档 | W5.3、W5.4 |
| W5.6 | README + 同步项（§11）+ WORKPLAN 回写 | W5.5 |

## 11. 同步项与跨 WP 登记（拍板后执行）

| 项 | 动作 | 归属 |
| :--- | :--- | :--- |
| `quartz-defaults` | 追加 `+quartz-app-api`（追加式） | WP-5 执行；登记通知 |
| Builder `scripts/test-in-vm.sh` | 增 `APP_PORT`（宿主→443）转发（验收需要） | 登记 WP-1（与既有 PORT/SSH_PORT 同款） |
| WP-4 daemon | **零改动**（HTTP 上游已承诺）；如需对齐文档，仅补一句「WP-5 已按 §12 落地」 | 登记通知 |
| WP-3 Dashboard | 零改动（数据面互不依赖） | 登记通知 |
| WP-6 App | 接口清单以本文 §4 为准（info/login/refresh/status/action/devices/revoke + §6.2 pin 规则 + §5 动作集） | 登记通知 |
| 远程链路（tailscale/zerotier 防火墙策略） | 文档化「接口并入策略」两条路径（不自动改防火墙） | WP-5 文档 + WP-6 引导 |
| TDD | §3.4 无冲突（本设计即其落地）；§8「App 推送」待决保持开放 | 用户 + 架构决策 |

## 12. 变更记录

*v0.1 2026-10-12 —— 首稿（讨论稿）：定位/范围（§0）、事实核实（§1：rpcd/uhttpd 源码 + VM 实测四条）、架构与三案对比（§2）、认证模型（§3）、API 面八方法与错误码（§4）、控制面白名单（§5）、传输/证书/mDNS/残面（§6）、打包与集成（§7）、测试与验收（§8）、待拍板十项（§9）、W5.x 分解（§10）、同步项（§11）。*
*v1.0 2026-10-12 —— **用户拍板「就先按这个方案来」：§9 十项决策按推荐案定案**（D1 rpcd exec 插件 + uhttpd 443 专用实例 / D2 JWT+refresh 旋转 / D3 专用实例 / D4 TOFU+DER pin / D5 umdns / D6 代理 daemon HTTP / D7 动作三组 / D8 参数 uci 化 / D9 推送不做 / D10 限速+审计）；状态转「定案并开工」，按 §10 W5.x 实施。*
*v1.0.1 2026-10-12 —— 实施期机制修正：**限速键改为全局**（uhttpd `/ubus` 不向插件转发客户端 IP——源码核实仅注入 `ubus_rpc_session`，per-IP 不可实现；全局键对单管理员场景更强）；`login` 增加可选 `client_ip`（App 自报展示元数据）；§3.3/§4.1/§9-D10 同步。*

# quartz-app-api 设计规范

**版本**: v1.2 —— **定案**（2026-10-12 用户拍板「按推荐案实施」；v1.0.1–v1.0.3 为实施与验收期修正/补记；
**v1.1 = 软件包管理面扩展**，2026-10-13 用户确认登记、2026-10-14 实施并 VM 验收；
**v1.2 = ubus 转发项机制 + bandix 服务面**，2026-10-17 实施——WP-8 bridge 衔接项〈D5〉落地）
**日期**: 2026-10-12（v1.2 增补 2026-10-17）
**状态**: 定案并实施（W5.1–W5.5 + W5.7 软件包扩展完成，跟踪见 WORKPLAN §2/§3）
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
- **登录前置（v1.0.2，坑位 #28）**：要求根口令已设置——rpcd `session.login` 在 shadow 口令为空时**接受任意密码**（源码语义），未设口令的机器不允许签发长效凭据；`info.password_set` 供 App 事前提示。
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
├── state.json                  设备注册表（0600；原子写 .tmp+rename + flock）
│     {"devices":[{ "id":"a1b2…", "name":"iPhone 15", "created":1697…, "last_seen":…,
│                   "last_ip":"192.168.1.23", "refresh_sha256":"…", "refresh_exp":…,
│                   "rot_counter":3, "revoked":false }]}
└── history.json                软件包操作记录（v1.1；0600；环形 30 条；§4.5）
/var/run/quartz-app-api/        (0700；tmpfs)
└── ratelimit.json              限速/锁定状态（丢之无害）
```

## 4. API 面（ubus 对象 `quartz-app-api`）

### 4.1 方法表

| 方法 | 认证 | 参数 | 返回（成功信封） | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| `info` | 无 | `{}` | `{ok,api,name,model,release,kernel,password_set,features[]}` | 配对前展示路由器身份（mDNS 兜底）；`password_set=false` 时 App 应提示先设管理密码（v1.0.2） |
| `login` | 凭据 | `{username,password,device_name,client_ip?}` | `{ok,api,access_token,access_exp,refresh_token,refresh_exp,device_id,router{…,ts}}` | 用户名/密码=系统管理员（rpcd login 校验，§1.1）；限速+审计；`client_ip` 为 App 自报的展示元数据（不参与安全决策，v1.0.1） |
| `refresh` | refresh | `{device_id,refresh_token}` | 同 login（换发两枚令牌；旋转） | 复用检测→吊销（§3.1） |
| `logout` | refresh | `{device_id,refresh_token}` | `{ok,api}` | 删设备记录 |
| `status` | access | `{access_token}` | `{ok,api,snapshot:{…AGGREGATE-API v1 原文…}}` | 代理 daemon 回环 HTTP；语义同构（经 ubus JSON 重序列化，非字节级） |
| `action` | access | `{access_token,action,args{}}` | `{ok,api,action,result{}}` | 动作白名单见 §5 |
| `devices` | access | `{access_token}` | `{ok,api,devices:[{device_id,name,created,last_seen,current}]}` | 已配对设备清单 |
| `revoke` | access | `{access_token,device_id}` | `{ok,api}` | 踢出指定设备（含自身=登出） |
| `packages` | access | `{access_token,scope,query?,page?,per_page?}` | `{ok,api,scope,page,per_page,total,pages,items:[…]}` | 软件包管理面读（v1.1，§4.5）；`scope=installed\|upgradable\|available\|history` |
| `config.*` | access | 见 §5.2 | 见 §5.2 | 配置面（W6.10）：注册表读、暂存写、apply/confirm/rollback 安全网；`config.read/write/changes/revert/apply/confirm/rollback` |

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
| 插件默认总时长 | 10s | 远低于 rpcd exec 120s |
| `packages` 读（v1.1） | 20s | `available '*'` 全量索引约 3.5MB JSON（10k 包） |
| `packages.refresh`（v1.1） | 45s | `apk update`（含索引下载；R4S 级设备余量） |
| `packages.upgrade` / `packages.remove`（v1.1） | 50s | 含安装包下载；单包实测 <1s（本地源） |

**硬上限链（v1.1 核实）**：uhttpd `/ubus` 对每次调用的 dispatch 超时 =
`script_timeout × 1000ms`（`ubus.c: uloop_timeout_set`），app 实例配置 60 →
**单次 /ubus 调用（含插件全程）必须 <60s**；rpcd exec 默认 120s 不是瓶颈。
故上表 packages 各预算均须留在 60s 窗口内（当前 45s/50s + 外层余量已留）。

### 4.5 软件包管理面（v1.1；App「应用 tab · 软件包」子段与「操作记录」页数据面）

**登记与消费**：WP-6 UI-DESIGN §5 登记项（2026-10-13 用户确认）——读方法 `packages`
（分页）+ 动作 `packages.refresh/upgrade/remove`；安装浏览（可用源搜索安装）
**v1 后置**（不在本次范围）。数据源 = 路由器 **apk 体系**（apk-tools 3.x，
与 LuCI 软件包页同源命令 `apk query --format json`；VM 实测 3.0.5）。

| scope | 命令（同源 LuCI `package-manager-call`） | 条目字段 |
| :--- | :--- | :--- |
| `installed` | `apk query --fields … --format json --installed --from system '*'` | `version`=已装；`upgradable`+`target_version`（有新版时） |
| `upgradable` | `apk query --fields … --format json --upgradable '*'` | `version`=目标版本；`installed_version`=已装（供版本 diff） |
| `available` | `apk query --fields … --format json --available '<模式>'` | `version`=仓库版；`installed`/`installed_version`/`upgradable` 合并自已装集 |
| `history` | 本地 `history.json`（不存在于 apk） | 见下 |

- **字段集**（`--fields`，不含 `contents` 等膨胀字段）：
  `name,version,description,arch,license,origin,installed-size,file-size`；包 scope 条目统一超集
  `{name,version,installed_version?,target_version?,description,arch,license,origin,
  installed_size,file_size,installed,upgradable}`；`history` 条目为独立形态
  `{ts,action,target,ok,output,duration_ms}`（同一 `items` 数组承载）。
- **分页**：`page`（1 基，越界夹紧）`per_page`（默认 24，上限 100）；`total/pages` 随回；
  结果按 `name` 升序（确定性）。
- **搜索**：`query` = 包名**子串**（服务端包裹为 apk 模式 `*q*`，小写化）；字符集白名单
  `[a-z0-9._+@-]`，**用户输入不直通 apk 模式**（防通配/注入）；空 q = 全量首个分页。
- **动作**（通用 `action` 方法 + id 前缀路由 `packages.`，白名单式显式注册）：
  `packages.refresh`=`apk update`；`packages.upgrade`（`args.name` 空=全部）=
  `apk upgrade [name]`；`packages.remove`（`args.name` 必填）= `apk del <name>`
  （**危险动作**——apk 对 world 包不做保护、App 侧确认层负责把关，与 LuCI 行为一致）；
  包名字符集 `^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$`（绝不拼接 shell）。
- **操作记录（`history`）**：`/etc/quartz-app-api/history.json`（0600，flock + 原子写，
  环形 30 条，最新在前）——每次动作（成功/失败）记录
  `{ts,action,target,ok,output(≤512B 尾部),duration_ms}`；失败附 apk ERROR 行摘录
  （`classify`：优先 ERROR 行+缩进续行）。App「操作记录」页直出。
- **输出/错误**：动作成功 `result:{stdout,duration_ms}`；失败 `action_failed`（detail=
  摘录；超时明确标注 `timeout after Ns`）；读失败 `data_unavailable`（apk 不可用/索引
  损坏）；负例（scope 未知/搜索词非法/包名非法）`bad_request`/`forbidden_action`。
- **审计**：每次动作落 logd 短串（`packages action=… target=… ok|fail dur_ms=…`）。

**测试源**（VM 验收用，`dev/testrepo/`）：本地 apk 仓夹具（`serve.py` 提供**同 URL 可切换
stage**——stage1=仅旧版 / stage2=含新版，模拟"上游发布新版本"，避免 world 钉死约束：
`apk add 文件/等值` 会写 `name>hash`/`name=ver` 钉死条目致升级被抑制——VM 实测教训）。


## 5. 控制面（动作白名单）

机制：`action` 表 = **代码内注册表**（id → 校验器 + 执行器）；参数只按**结构校验 + 字符集白名单**，绝不拼接进 shell；执行走 `ubus call` CLI 或 `/etc/init.d/<name> <op>`（`<name>` 须 ∈ 已装 init 脚本 ∩ 允许集合）。

| action | 参数 | 机制 | v1 |
| :--- | :--- | :--- | :--- |
| `service.start` / `service.stop` / `service.restart` | `{name}` | `/etc/init.d/<name> <op>`；默认允许集：`openclash` `dockerd` `dnsmasq` `quartz-daemon` `tailscale` `zerotier` `bandix`（uci 可调）；`network`/`dropbear` 等自断链路项默认**不**入集 | ✅ |
| `system.reboot` | — | `ubus call system reboot` | ✅ |
| `docker.container.start/stop/restart` | `{id}` | `docker container <op> <id>`（字符集校验；docker 未装 → `action_failed: docker unavailable`） | ✅（存在即用） |
| `packages.refresh` | — | `apk update`（拉取仓库索引；§4.5） | ✅ v1.1 |
| `packages.upgrade` | `{name?}` | `apk upgrade [name]`（name 空 = 全部升级） | ✅ v1.1 |
| `packages.remove` | `{name}` | `apk del <name>`（**危险动作**；确认层在 App 侧） | ✅ v1.1 |
| `bandix.limit_default_set` | `{wan_rx_rate_limit,wan_tx_rate_limit}` | **ubus 转发项**（§5.1）→ `quartz.bandix limit_default_set`（Byte/s；数值收 `data` 对象） | ✅ 2026-10-17 |
| `bandix.whitelist_add` / `bandix.whitelist_delete` | `{mac}` | …`quartz.bandix` 同名方法（纯字符串 → 扁平参数） | ✅ 2026-10-17 |
| `bandix.whitelist_enabled` | `{enabled}` | …（0/1，`data` 对象） | ✅ 2026-10-17 |
| `bandix.hostname_set` | `{mac,hostname}` | …（hostname 空串 = 清除绑定） | ✅ 2026-10-17 |
| `bandix.limits` | — | **读**：限速/白名单/配额聚合（唯一读条目；§5.1） | ✅ 2026-10-17 |
| 核心/规则更新触发（TDD §4.4） | — | 归属 WP-7/WP-8 链路；**版本状态由 daemon `services` 报告**（已有），触发器后置 | ✗ 后置 |
| DDNS 配置（模式 B） | — | 经同一「ubus 转发项」加行接入（机制已落地，本轮不加条目） | ⬜ 后置 |

- **控制后可视化**：动作成功 → daemon 快照 ≤2s 反映新状态（WP-4 §12.4 承诺），App 无需专门回读。
- **能力可见性同源**：App 依据 `status.snapshot.services`/`docker.available` 决定显示哪些控制入口（数据与动作判定共用一份事实）。

### 5.1 「ubus 转发项」机制（v1.2 落地；WP-8 bridge 衔接项 D5）

**定位**：把第三人插件 Bridge 契约（TDD §5 模式 B）的**写面动作**经 app-api 暴露给 App。**仍无任意透传**——与 §5 主线同一纪律：

| 项 | 规则 |
| :--- | :--- |
| 条目 | **逐条显式注册**（`internal/ubusfwd` registry：id → 固定 object + method + 参数模式表）；未注册 id → `forbidden_action`，未声明参数键 → `forbidden_action`，缺必填/越界/字符集不符 → `bad_request` |
| 参数形态（Wrap） | 条目声明：**含数值字段的方法参数收进 `data` 对象**（bridge 坑位 #61：rpcd ucode 策略要求声明且类型精确匹配，而 blobmsg 按数值宽度选 int32/int64 → 单一类型提示必拒一半调用；纯字符串方法保持扁平） |
| 执行 | `ubus call <object> <method> <json>`（JSON 作为**单个 argv**，无 shell）；5s 超时；`ubus` 缺失/对象不在 → `data_unavailable`（"bridge unavailable"） |
| 错误映射 | bridge 信封 `{ok,api,data\|error}`：`ok=false` 时 `bad_request`→`bad_request`、`data_unavailable`→`data_unavailable`（message 带上游原因）、其余→`action_failed`；成功 `data:null` **不算失败**（bridge 写面常态） |
| 审计 | 每次转发落 logd 一行（`fwd action=… target=<obj>.<method> ok\|fail dur_ms=…`） |
| 读面 | **原则上不经本机制**——App 读 bandix 状态/流量走 daemon 快照（AGGREGATE-API §3.7，与 Dashboard 同字节）；唯一读条目 = `bandix.limits`（限速/白名单/配额聚合，无参）——该**配置状态不在快照内**，设置类对话框需取当前值 |
| 扩展 | 新条目 = registry 一行 + 单测；新对象（如 ddns bridge）= 同样只加行（前缀路由由 `Handles` 判定），机制不变 |
| 数值入参 | App 传 JSON number 亦可（dispatch 层整数值 → 十进制串，再交条目校验器）；小数丢弃 |

### 5.1.1 qmodem 蜂窝域条目（2026-10-18，W6.12 蜂窝轮）

**写入面 15 条**（`qmodem.*`）：拨号 `dial`/`hang`/`redial`、`set_usb_mode`、`set_lockband`、`set_network_prefer`、`send_at`、`send_sms`、`delete_sms`、`set_sms_storage`、`set_traffic_reset_schedule`、`clear_stats`/`save_stats`/`clear_dial_log`、`reboot`、`set_sim_slot`、`set_imei`。
**读面 16 条**（页面级读，见"读面例外"）：`at_cfg`（含预置命令，与网页端同源、可选 `lang`）、`sms_list` / `sms_storage_get`（**独立对象 `qmodem.sms`**——结构化消息，与网页端同源）、`dial_log`、`lockband`、`usb_mode`、`network_prefer`、`neighborcell`、`current_band`、`traffic_reset_schedule`、`sim_slot`、`sim_switch_capabilities`、`reboot_caps`、`disabled_features`、`tty_ports`。
**写面补 4 条**（短信对象）：`sms_send` / `sms_delete` / `sms_mark_read` / `sms_storage_set` / `sms_sync`（= 22 号条目组）。
**不注册**：`scan_*` / `remove_modem` / `set_5g_lan` / `set_neighborcell`（手动锁小区工具，参数多、风险高，v1 留网页端）。

**新增机制（本机制的三项扩展，均已落单测）**：

| 项 | 说明 |
| :--- | :--- |
| **上游 JSON 形态（shape）声明** | 每条目声明上游要哪一种形态：`flat` / `data`（bridge 数值）/ `sectionParams`（`config_section` 顶层 + 其余收 `params` 对象或数组）/ `sectionOnly` / `none`。qmodem 方法参数为三层结构，转发时按声明重建 |
| **两档参数校验** | ① **严格字段校验**：section（白名单，读 `uci show qmodem` 的 `modem-device`）/ at（字符集+长度）/ port（**白名单优先**，`get_tty_ports` 不可用时回退路径字符集）/ imei（15 位）/ phone / 索引列表 / 枚举（usb mode、storage、reset_type、slot、reboot method）。② **厂商方言参数的"结构校验"**：`set_lockband` 的 `{band_class, lock_band}`、`set_network_prefer` 的 `["4G","5G"]` 数组等形态由上游厂商脚本定义（**与其 rpcd 文档示例不一致——按源码实现为准**）→ 校验对象/数组形状 + 键名白名单 + 值类型与长度上限，语义交上游判定 |
| **sectionKey（模组参数名）** | 上游各对象对模组的参数名不同（`qmodem.*` 用 `config_section`、`qmodem.sms` 用 `modem_id`）→ 条目声明 `sectionKey`，App 统一以 `config_section` 传参、转发时按声明重建（`config_section` 不会泄漏到上游） |
| **上游应答两形态** | bridge 走信封 `{ok,data|error:{code,message}}`；qmodem 插件为**裸对象**（无 `ok`）→ 整对象作 data，其错误为 `{"error":"<字符串>"}` → 映射 `action_failed` |

**厂商方言参数以 JSON 字符串传入**：`action` 方法的 `args` 为扁平 `map[string]string`，嵌套参数（如 AT 页的 `params{at,port}`）由 App 以 **JSON 字符串**传给 `args.params`，app-api 解析并按子模式校验后重建（不引入嵌套透传）。

**读面例外（2026-10-18 规则修订）**：快照承载**概览级只读摘要**；**页面级读**（AT 端口与预置命令、短信列表、设置回显、能力声明）走本机制——先例 `bandix.limits`，条目仍逐条显式注册、无自由参数。

**luci 对象条目（2026-10-19，W6.10 路由器 tab）**：三条只读/动作条目——`luci.init_list`（无参，`getInitList`）、`luci.init_action`（`{name,action}`，`setInitAction`；`name` 走 `/etc/init.d/<name>` 字符集 + 禁止 `..`，action 枚举 enable/disable/start/stop/restart）、`luci.timezones`（无参，`getTimezones`）。`setInitAction` 成功回 `{}`（裸对象），失败回 `{"error":"…"}`，由既有「裸对象」分支处理。

### 5.2 配置面 `config.*`（2026-10-19，W6.10/W6.11 路由器 tab）

**定位**：网页端 Network/System 配置面的**受控暴露**。与 §5.1 同一纪律：**无任意透传**——配置文件、section、option 全部在 `internal/uciconf/registry.go` **按节完整注册**（类型/枚举/范围/字符集/敏感标记），未注册一律 `forbidden_action`；执行 = `ubus call uci …`（JSON 单个 argv，不拼 shell）。

| 方法 | 参数 | 返回（成功载荷） | 说明 |
| :--- | :--- | :--- | :--- |
| `config.read` | `{access_token,config,section?,option?}` | `{config,section,option,value}` | `value` 三形态：全量 `{section:{option:value}}` / 单节 `{option:value}` / 单选项标量；敏感项 `"__masked__"` |
| `config.write` | `{access_token,config,section,option,value}` | `{changes:{changes:[…]}}` | **暂存**（不落盘）；类型/枚举/范围/字符集校验 |
| `config.changes` | `{access_token,config}` | `{changes:{changes:[…]}}` | 查看当前会话暂存 |
| `config.revert` | `{access_token,config}` | `{reverted:<config>}` | 丢弃**暂存**（apply 之前有效） |
| `config.apply` | `{access_token,rollback?:bool=true,timeout?:int=90}` | `{applied:true,rollback,timeout}` | 提交；rollback=true 武装回滚窗口 |
| `config.confirm` | `{access_token}` | `{confirmed:true}` | 保留改动，取消回滚窗口 |
| `config.rollback` | `{access_token}` | `{rolled_back:true}` | **立即回滚已 apply 未 confirm 的改动**（`uci rollback`） |
| `config.add` | `{access_token,config,section_type,values{}}` | `{section,values}` | 新增已注册类型的 section（**暂存**；values 只收该节注册字段，`Required` 必填；返回 rpcd uci add 的 section 名） |
| `config.delete` | `{access_token,config,section}` | `{deleted:section}` | 删除 **Deletable** 节（如 `dhcp` 的 `host` 静态租约；**暂存**，需 apply） |

**会话模型（VM 实测）**：uci 写入/apply/confirm/rollback 必须携带**同一 `ubus_rpc_session`**；`session create` 是空 ACL，只有 `session login`（管理员凭据）的会话有全量 ACL。app-api 是 rpcd exec 插件、每次调用是新进程 → login 时开启保留会话（12h）并把 sid 持久化进设备记录（`store.Device.ConfigSID`），logout/revoke 销毁。

**安全网与 revert/rollback 的区别（2026-10-19 VM 实测，关键）**：`config.apply{rollback:true,timeout}` 后未确认会自动回滚；但**立即回滚不能调 `config.revert`**——`uci revert` 只丢弃暂存（apply 后暂存已空），已生效配置保留，且会解除回滚计时器（配置「漂」在生效态）。必须调 `config.rollback`（`uci rollback`，同会话）。App 侧确认条 = 90s 倒计时 + 「保留更改 / 立即回滚」。

**注册表试点范围**：`network.lan|wan`、`wireless @wifi-device|@wifi-iface`、`dhcp @dnsmasq|@dhcp`、`system @system`。W6.11 再扩防火墙/静态路由/管理权/计划任务/挂载/LED 与 `config.add/delete`（静态租约）。

**错误码**：`forbidden_action`（未注册配置/节/选项）、`bad_request`（类型/枚举/范围/字符集/掩码回写）、`data_unavailable`（配置会话不可用）、`action_failed`（uci 执行失败，detail 带上游串；网页端有未确认 apply 时也会落此码）。

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

**单元测试（host `go test ./...`）**：JWT 签发/校验/偏斜/回拨；refresh 旋转与复用检测；注册表原子写 + flock；限速状态机；动作表校验（名称字符集/白名单/注入负例）；信封与错误码序列化；**软件包面（v1.1）**：scope 合并语义（installed×upgradable、available×installed 标记）、分页夹紧/上限、搜索词白名单与模式包裹、动作命令构造（refresh/upgrade/单包/remove）、动作拒否负例、失败/超时→历史记录、history 环形与并发串行化。

**VM 集成验收**（QEMU + `test-in-vm.sh`，需为 443 增补宿主转发 `APP_PORT`，见 §10 同步项）：

1. **认证**：`info`（无凭据）→ `login`（错密码→`invalid_credentials`；连错→`rate_limited`；正密码→双令牌）→ `refresh` 换发 → 旧 refresh 复用→设备吊销 → 重登录。
2. **吊销**：`devices` 列表 → `revoke` 第二设备 → 其 `status` 得 `device_revoked`；`logout` 后设备从列表消失。
3. **权限边界**：默认 sid 调 `file.read` → Access denied（对照组）；仅 `quartz-app-api` 对象可达；篡改 JWT → `token_invalid`；无 access 调 `status` → 拒绝。
4. **数据面**：`status` 返回与 `127.0.0.1:8899`（及 Dashboard 渲染）同构；停 daemon → `data_unavailable`。
5. **发现与传输**：`/etc/umdns/quartz-app.json` 生成 + fp 与 `sha256sum /etc/uhttpd.crt` 一致；`curl --insecure` 直连 443 /ubus 全链通；证书 pin 校验脚本（dev/）通过。
6. **动作**：`service.restart openclash` 后 ≤2s 快照 `services.openclash.running` 变化；`system.reboot`（VM 内验证触发即返回）；越权 action → `forbidden_action`。
7. **软件包管理面（v1.1，`dev/vm-accept-packages.sh`，26 项）**：读四 scope（installed 分页/上限夹紧/未知 scope 拒否；available 子串搜索与非法字符拒否；upgradable 版本 diff 三字段；history 环形+最新在前）；动作真执行（refresh 计时、**全部升级（stage 切换模拟上游发新版→实装版本真变化）**、单包升级、卸载、注入/缺参拒否、不存在包 `action_failed`）；history.json 落盘 0600；ACL 面（packages 授权 + file.read 对照拒）。测试源 `dev/testrepo/serve.py`（同 URL 可切换 stage）。

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
| W5.7 | **软件包管理面扩展（v1.1）**：`packages` 读方法（四 scope 分页）+ 动作 `packages.refresh/upgrade/remove` + history.json（环形 30）+ 验收组 7（`dev/vm-accept-packages.sh`，26 项 PASS） | W5.6、用户确认（2026-10-13） |

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
*v1.0.2 2026-10-12 —— 实施期安全修正（VM 验收暴露，坑位 #28）：**登录要求根口令已设置**——rpcd 对 `/etc/config/rpcd` 的 `$p$root` 语义是"查 shadow 的 root 口令"，而 shadow 口令**为空时校验函数直接放行（任意密码通过）**；未设口令的机器被 WP-5 显式拒绝（避免为"任意密码"签发长效凭据）；`info` 增 `password_set` 字段供 App 事前提示；§3.1/§4.1 同步。另：uci-defaults 的 uhttpd 实例创建修正为 `uci set uhttpd.app=uhttpd`（`uci add uhttpd app` 第二参数是类型，生成匿名节——坑位 #27）。*
*v1.0.3 2026-10-13 —— **W5.1–W5.5 完成**：全部代码 + 打包落地并通过 **VM 集成验收 35/35 全绿**（QEMU 新镜像；六组：环境/ACL 边界/认证/旋转吊销登出/限速/数据面降级/控制面/reboot 持久化；证据 `dev/accept-run-20261013-1341.log`，可复跑 `dev/vm-accept.sh`，两次重跑一致）。验收期另修复 logout 信封不对称（auth 层统一返回裸载荷、reply 层统一包裹；回归测试 `main_test.go`）。剩余：推送 + feed 锁定 bump（跨 WP，待用户指令）、R4S 真机复测（与 WP-3 合并）。*
*v1.1 2026-10-14 —— **软件包管理面扩展（W5.7）实施并 VM 验收**（WP-6 UI-DESIGN §5 登记项的 WP-5 侧落地）：读方法 `packages`（`scope=installed|upgradable|available|history`，分页；数据源=apk 体系与 LuCI 软件包页同源命令 `apk query --format json`）+ 动作 `packages.refresh/upgrade/remove`（通用 action 白名单前缀路由）+ 结构化操作记录 `/etc/quartz-app-api/history.json`（环形 30，成功/失败+apk 错误摘录，供 App「操作记录」页）；acl.d 追加 `packages`；`info.features` 增 `packages`。新增 §4.5 与超时预算（§4.4：读 20s / refresh 45s / upgrade·remove 50s——**uhttpd /ubus 单次调用 60s 硬窗**源码核实）。**VM 验收 26/26 PASS**（`dev/vm-accept-packages.sh`，证据 `dev/accept-packages-run-*.log`），测试源 `dev/testrepo/`（serve.py 同 URL 可切换 stage 模拟上游发新版；VM 实测教训：`apk add 文件/等值` 写 world 钉死条目会抑制升级）。安装浏览（可用源搜索安装）v1 后置（不改本包方法面）。*
*v1.2 2026-10-17 —— **ubus 转发项机制落地（§5.1）+ bandix 服务面**（WP-8 衔接项 D5，用户「把没做的做了吧」）：①默认允许集加 `bandix`（`service.start/stop/restart`；一行 + 回归测试）；②新增 `internal/ubusfwd` 转发项机制——**无任意透传**：条目逐条显式注册（object+method+参数模式表），参数结构+字符集/数值域校验、JSON 单 argv 执行 `ubus call`、bridge 信封错误码映射（`data:null` 不算失败）、logd 审计；首批 5 条目 = bandix 写面（limit_default_set / whitelist_add / whitelist_delete / whitelist_enabled / hostname_set）；③`action` 方法参数接受 JSON number/bool（整数值转十进制串）；④`info.features` 增 `bandix`。读面不经本机制（App 读走 daemon 快照 §3.7）。单测 9 例（路由/校验/包装/错误映射/审计）。PKG_RELEASE 2。*
*v1.3 2026-10-19 —— **配置面 `config.*` 落地（§5.2，W6.10 路由器 tab 后端）**：`internal/uciconf` 注册表（network.lan|wan、wireless、dhcp、system）+ 六个方法（read/write/changes/revert/apply/confirm）+ 配置会话（login 保留 12h、sid 持久化、logout 销毁）；`methodList`/ACL 三处登记；PKG_RELEASE 8。VM 验收 17/17（`dev/vm-accept-config.sh`）。*
*v1.4 2026-10-19 —— **路由器 tab 后端补强（W6.9/W6.10 前端落地同批）**：①新增 `config.rollback`（VM 实测 `config.revert` 不能撤销已生效配置且会解除回滚计时器 → 立即回滚必须 `uci rollback`；PKG_RELEASE 9）；②`internal/ubusfwd` 增 `luci.init_list` / `luci.init_action` / `luci.timezones` 三条转发条目（启动项页与时区选择器数据源；`init_action.name` 严格字符集 + `..` 拒绝、action 枚举）；③修复 `main_test.go` 方法面断言仍写死 9 个方法的历史遗留（配置面加入后必然失败，已扩到 16 并显式列出）。VM 验收 15/15（`dev/vm-accept-router.sh`，含 rollback 往返与 luci 负例）；App e2e 23 项 ALL PASS。*
*v1.5 2026-10-19 —— **W6.11 静态租约切片**：①`config.add` / `config.delete`（暂存语义，同 apply/confirm 安全网；`config.add` 只收注册字段 + `Required` 必填，`config.delete` 仅允许 `Deletable` 节）；②`dhcp` 注册表增 `@host`（name/mac/ip/dns/leasetime/duid；mac 必填、格式校验；host 可删除）；③`methodList`/ACL 增两方法（18 方法面）；PKG_RELEASE 10；单测覆盖 add/delete/必填/白名单/非可删节；App 侧 DHCP 页静态租约增删（build 9）。*

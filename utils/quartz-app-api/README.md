# quartz-app-api

QuartzWrt 专属 App API（WP-5）：rpcd exec 插件 + uhttpd 443/TLS 专用实例；
JWT + refresh 认证；数据面代理 `quartz-daemon` 快照；控制面白名单动作；软件包管理面
（apk 查询/升级/卸载 + 操作记录）；umdns 发现宣告。

**设计规范**: [`docs/DESIGN.md`](docs/DESIGN.md) v1.1（定案）。

## 形态速查

```
App ──HTTPS 443──▶ uhttpd 'app' 实例 ──/ubus──▶ rpcd(ACL) ──▶ /usr/libexec/rpcd/quartz-app-api
                                                              ├─ 数据: GET 127.0.0.1:8899/api/v1/status
                                                              ├─ 动作: /etc/init.d/<白名单> | ubus | docker
                                                              └─ 软件包: apk query / apk update|upgrade|del
```

- ubus 对象名 = 文件名 `quartz-app-api`（rpcd 机制）；App 可达面仅此一对象九方法（acl.d `unauthenticated` 组）。
- 错误一律 in-band：`{"ok":false,"api":1,"error":{"code":…}}`（rpcd exec 插件 stdout 即结果）。
- 认证：`access_token`=JWT(HS256, 15min)；`refresh_token`=随机不透明（设备端仅存 SHA-256，旋转 + 复用检测→吊销）。
- 状态：`/etc/quartz-app-api/{secret,state.json,history.json}`（0600）；限速：`/var/run/quartz-app-api/ratelimit.json`（tmpfs）。
- 发现：`/etc/umdns/quartz-app.json` → `_quartz-app._tcp`（TXT：`v/path/name/fp/rel`，**fp=sha256(DER 证书)**）。

## 接口（九方法）

| 方法 | 认证 | 说明 |
| :--- | :--- | :--- |
| `info` | 无 | 路由器身份（配对前展示；`features` 含 `packages`） |
| `login` | 管理员密码 | `{username,password,device_name,client_ip?}` → 双令牌 |
| `refresh` | refresh | 旋转换发（旧令牌复用 → 吊销设备） |
| `logout` | refresh | 删除设备记录 |
| `status` | access | 快照（= daemon `/api/v1/status` 同数据模型） |
| `action` | access | 见下（含 `packages.refresh/upgrade/remove`） |
| `devices` | access | 已配对设备清单 |
| `revoke` | access | 吊销设备（含自身） |
| `packages` | access | 软件包读（scope=installed/upgradable/available/history，分页） |

### 动作（`action` 方法）

`service.{start,stop,restart}`（白名单）/ `system.reboot` / `docker.container.*` /
**`packages.refresh`**（`apk update`）/ **`packages.upgrade`**（`{name?}`，空=全部）/
**`packages.remove`**（`{name}`，危险动作，App 侧确认层）。

### 软件包读（`packages` 方法）

```
{"scope":"installed|upgradable|available|history", "query":"<子串>", "page":1, "per_page":24}
→ {ok, api, scope, page, per_page, total, pages, items:[{name,version,installed_version?,target_version?,
    description,arch,license,origin,installed_size,file_size,installed,upgradable}]}
```

- 数据源 = 路由器 apk 体系（`apk query --format json`，与 LuCI 软件包页同源）；
  升级检测 = `--upgradable`；搜索 = 按名子串（服务端包裹为 apk 模式，用户输入不直通）。
- `history` = 升级/卸载/刷新操作记录（成功与失败），近 30 条最新在前
  （`/etc/quartz-app-api/history.json`），供 App「操作记录」页。
- 超时预算：读 20s；`refresh` 45s；`upgrade/remove` 50s（uhttpd app 实例单次
  `/ubus` 调用窗口 60s 内——DESIGN §4.4）。

调用示例（对路由器 443）：

```sh
curl -sk https://192.168.1.1/ubus -d '{"jsonrpc":"2.0","id":1,"method":"call",
  "params":["00000000000000000000000000000000","quartz-app-api","login",
            {"username":"root","password":"…","device_name":"iPhone"}]}'
```

## 构建与测试

```sh
# 单测（宿主 Go）
cd src && go test ./...

# 单包编译（构建树）
make package/feeds/quartz/quartz-app-api/compile

# VM 验收（软件包管理面；测试源=dev/testrepo/serve.py，脚本自动拉起）
dev/vm-accept-packages.sh
```

## 配置（`/etc/config/quartz-app-api`）

`access_ttl` / `refresh_ttl` / `refresh_abs_ttl` / `login_fails` / `lockout` /
`daemon_url` / `mdns` / `port`；`list service_whitelist` 追加动作白名单项。

## 排障

- 改了插件二进制/新装包后：`/etc/init.d/rpcd reload`（SIGHUP 重扫；会话不丢）。
- `Access denied`：核对该方法的 acl.d 授权与 rpcd 版本（≥2026.10.04 行为）。
- 证书/pin：`sha256sum /etc/uhttpd.crt`；重刷固件会重新生成 → App 应提示重配对。
- mDNS 不可见：`ubus call umdns update`；确认 `/etc/umdns/quartz-app.json` 已生成（本包 init）。

# quartz-app-api

QuartzWrt 专属 App API（WP-5）：rpcd exec 插件 + uhttpd 443/TLS 专用实例；
JWT + refresh 认证；数据面代理 `quartz-daemon` 快照；控制面白名单动作；umdns 发现宣告。

**设计规范**: [`docs/DESIGN.md`](docs/DESIGN.md) v1.0.1（定案）。

## 形态速查

```
App ──HTTPS 443──▶ uhttpd 'app' 实例 ──/ubus──▶ rpcd(ACL) ──▶ /usr/libexec/rpcd/quartz-app-api
                                                              ├─ 数据: GET 127.0.0.1:8899/api/v1/status
                                                              └─ 动作: /etc/init.d/<白名单> | ubus | docker
```

- ubus 对象名 = 文件名 `quartz-app-api`（rpcd 机制）；App 可达面仅此一对象八方法（acl.d `unauthenticated` 组）。
- 错误一律 in-band：`{"ok":false,"api":1,"error":{"code":…}}`（rpcd exec 插件 stdout 即结果）。
- 认证：`access_token`=JWT(HS256, 15min)；`refresh_token`=随机不透明（设备端仅存 SHA-256，旋转 + 复用检测→吊销）。
- 状态：`/etc/quartz-app-api/{secret,state.json}`（0600）；限速：`/var/run/quartz-app-api/ratelimit.json`（tmpfs）。
- 发现：`/etc/umdns/quartz-app.json` → `_quartz-app._tcp`（TXT：`v/path/name/fp/rel`，**fp=sha256(DER 证书)**）。

## 接口（八方法）

| 方法 | 认证 | 说明 |
| :--- | :--- | :--- |
| `info` | 无 | 路由器身份（配对前展示） |
| `login` | 管理员密码 | `{username,password,device_name,client_ip?}` → 双令牌 |
| `refresh` | refresh | 旋转换发（旧令牌复用 → 吊销设备） |
| `logout` | refresh | 删除设备记录 |
| `status` | access | 快照（= daemon `/api/v1/status` 同数据模型） |
| `action` | access | `service.{start,stop,restart}`（白名单）/ `system.reboot` / `docker.container.*` |
| `devices` | access | 已配对设备清单 |
| `revoke` | access | 吊销设备（含自身） |

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
```

## 配置（`/etc/config/quartz-app-api`）

`access_ttl` / `refresh_ttl` / `refresh_abs_ttl` / `login_fails` / `lockout` /
`daemon_url` / `mdns` / `port`；`list service_whitelist` 追加动作白名单项。

## 排障

- 改了插件二进制/新装包后：`/etc/init.d/rpcd reload`（SIGHUP 重扫；会话不丢）。
- `Access denied`：核对该方法的 acl.d 授权与 rpcd 版本（≥2026.10.04 行为）。
- 证书/pin：`sha256sum /etc/uhttpd.crt`；重刷固件会重新生成 → App 应提示重配对。
- mDNS 不可见：`ubus call umdns update`；确认 `/etc/umdns/quartz-app.json` 已生成（本包 init）。

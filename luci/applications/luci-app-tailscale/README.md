# luci-app-tailscale（QuartzWrt 适配版）

Tailscale 的 LuCI 管理界面，为 QuartzWrt 适配再开发。

- **上游基线**：`asvow/luci-app-tailscale` v1.2.6（commit `534eb3f`，GPL-3.0-only；上游已停更）
- **适配架构（WP-10 定案）**：**daemon 层吃基座、功能层归本包**
  - daemon 由基座 `tailscale` 包（feeds/packages，活跃维护）提供；本包**不带** `/etc/init.d/tailscale` 与 `/etc/config/tailscale`，与基座**零文件重叠**；
  - `tailscale_helper`（幂等配置器）：`apply` 应用偏好（`tailscale set`）+ reconcile 胶水（MagicDNS dnsmasq 条目 / `network.tailscale` / 子网路由 / 防火墙 `tszone`+forwarding / exit-node 桥接）；`cleanup` 干净回退；
  - boot 钩子 `/etc/init.d/tailscale-quartz`（承接 enabled 语义与开机 apply）；WAN up 触发同样幂等 apply。
- **相对上游的改动**：见 `docs/DESIGN.md` §3（含安全收紧：移除自由 `flags` 直通；`authkey` 大小写断链修复；`ip`/CLI 的 ACL 白名单最小化）。
- **联动**：投放 `probes.d/tailscale.json` → `quartz-daemon` 快照/概念卡与 App 应用页自动出现 Tailscale 状态（零重编扩展机制）。

## 使用

1. `apk add luci-app-tailscale`（或随固件构建；**默认不预装**）；
2. LuCI → VPN → Tailscale：勾选 Enable → 保存并应用（助手会拉起 daemon 并显示登录状态）；
3. 点「Need to log in」在 Tailscale 完成授权（或填入 authkey 由助手自动登录）；
4. 远程访问路由器：在 QuartzWrt App「按地址添加」使用本机 tailnet 地址（`100.x.y.z` 或 MagicDNS 名，端口 443）。

## 许可

GPL-3.0-only。上游版权归 asvow 及贡献者；本包改动部分归 QuartzWrt 项目。

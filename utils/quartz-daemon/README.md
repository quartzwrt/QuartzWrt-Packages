# quartz-daemon

QuartzWrt 数据聚合守护进程（WP-4）：定时采集系统负载、网络吞吐、Docker 状态与
在线设备，输出**单一 JSON 快照**，供 Dashboard（rpcd 文件读取）与 App（经 WP-5
回环 HTTP）消费。

- **设计规范**: [`docs/DESIGN.md`](docs/DESIGN.md)（v1.0 定案）
- **数据契约**: [`../luci/applications/luci-app-dashboard/docs/AGGREGATE-API.md`](../luci/applications/luci-app-dashboard/docs/AGGREGATE-API.md)（冻结 v1——本进程是生产者，字段/语义变更须走契约流程）

## 输出通道

| 通道 | 位置 | 说明 |
| :--- | :--- | :--- |
| 文件（Dashboard 主通路） | `/var/run/quartz/status.json` | 每 `interval`（默认 2s）**原子写**（tmp+rename），权限 0644 |
| HTTP（WP-5/App 通路） | `GET http://127.0.0.1:8899/api/v1/status` | 与文件**严格同字节**；非 GET 405 / 未知路径 404 / 尚无快照 503；无认证（回环边界，对外认证归 WP-5） |

两条通道来自同一次序列化（单写者双缓冲），任何降级下均为合法 JSON。
`document.hidden` 语义、轮询间隔等消费方义务见契约 §5。

## 采集模型（快慢分离）

- **快周期**（每 `interval`）：纯 `/proc` + `/sys` 文件读 → 装配全量快照 → 一次 marshal →
  ① 原子写文件 ② `atomic.Value` 供 HTTP 直读（单写者，无并发竞争）
- **慢周期**（每 10s）：`ubus`（WAN 明细 / procd 服务表 / 板型兜底）、Docker unix socket、
  设备列表（`/proc/net/arp` ∪ 无线关联 ∪ DHCP 租约）、探针描述文件重扫——独立超时，
  失败只记 `errors[]`（组件级，恢复即清除），不阻塞快周期

各节数据源、口径与降级规则逐条见 DESIGN §3；上限自守（整文件 ≤32KB / clients ≤32 /
series ≤60 / unhealthy ≤8）见 §4。

## 配置（uci）

配置文件 `/etc/config/quartz-daemon`：

```
config daemon 'main'
	option interval '2'        # 采集周期（秒），范围 1-300
	option listen '127.0.0.1'  # HTTP 监听地址（回环）
	option port '8899'         # HTTP 端口
```

修改后 `uci commit quartz-daemon && /etc/init.d/quartz-daemon reload`（procd reload
trigger 重启进程生效）。缺失文件/缺项 = 内置默认值；非法值自动收敛。

## 探针描述文件（新软件包接入）—— DESIGN §13

服务的状态与版本由**声明式描述文件**驱动，新增软件包**不重编 daemon**：

- 目录：`/usr/share/quartz-daemon/probes.d/*.json`（包内置）+
  `/etc/quartz-daemon/probes.d/*.json`（本地追加，同 id 覆盖内置）
- 生效：慢周期重扫，运行期 `apk add` 后 ≤10s 自动出现在 `services` 节
- schema（v1，`id` 字符集 `^[a-z0-9_-]{1,32}$`）：

```json
{
  "id": "passwall",
  "title": "PassWall",
  "running": { "type": "procd", "name": "passwall" },
  "extras": {
    "core": { "type": "exec", "path": "/usr/bin/xray", "args": ["-version"],
              "regex": "Xray ([0-9.]+)", "cache": 300 }
  }
}
```

| 探针类型 | 语义 |
| :--- | :--- |
| `procd` | 已装 = `/etc/init.d/<name>` 存在；运行 = ubus service list 有 running 实例（三态：未装缺省 / 停止 `running:false` / 运行 `running:true`） |
| `pidof` | 仅运行信号（进程存在才出现；无独立"已装"探测，需三态请用 procd 型） |
| `exec` | 绝对路径 + 固定参数，`regex` 捕获组 1（无正则取首行）；结果缓存 `cache` 秒（默认 300）；路径不存在 → 字段省略 |

约束：仅 root 可写目录；无 shell、路径必须绝对；非法描述文件整条拒绝并记入 `errors[]`。
内置描述符：`openclash`（含 core 版本尽力探测，路径 `/etc/openclash/clash`）、`dockerd`。

## 构建与测试

```sh
# 单包交叉编译（构建树已就绪；WSL 须净化 PATH，见 WORKPLAN 坑位 #13）
make package/feeds/quartz/quartz-daemon/compile V=s -j16

# 宿主机单测（需 Go ≥ 1.22；本机 ~/.local/go）
cd src && go vet ./... && go test ./...
```

VM 集成验收（`QuartzWrt-Builder/scripts/test-in-vm.sh --detach` 起镜像后）：

```sh
python3 dev/vm-ssh.py '/etc/init.d/quartz-daemon status'          # 服务面
python3 dev/vm-ssh.py --fetch /var/run/quartz/status.json \
        --out /tmp/status.json && python3 dev/contract-check.py /tmp/status.json
python3 dev/vm-ssh.py 'printf "GET /api/v1/status HTTP/1.0\r\n\r\n" | nc 127.0.0.1 8899 | head -1'
```

Dashboard 真数据联调：见 `luci-app-dashboard/dev/vm-shot.py`（Playwright）。

## 已知边界

- **Docker**：按需安装（2026-10-12 移出预装）——出厂固件 `docker.available=false`、卡片隐藏是常态；
  已安装但 socket 不可用 → 整节 `null` + `errors`，下轮自动重试。采集走**无版本 API 路径**
  （`/containers/json`）——dockerd ≥29 默认最低 API 1.40，写死 `/v1.24` 会被拒（WORKPLAN 坑位 #24）。
- **WAN 缺省**是合法态：无默认路由（如未配置 WAN 的 VM）→ `up=false`、空字段，**不记 errors**；
  `up` 判定为"默认路由存在且承载接口非 down"（pppoe/wg 的 operstate=unknown 不误判）。
- **温度**：无 `thermal_zone`/`hwmon` 传感器 → `temp_c=null`（VM 即此形态）。
- **crash-loop 保护**：procd 默认 `respawn 3600 5 5`——1 小时内 5 次崩溃后**停止拉起**
  （防 fork 炸弹，属设计行为）；手动 `/etc/init.d/quartz-daemon start` 复位。
- **/var/run 符号链接**：`/var/run` → `/tmp/run`；rpcd ≥2026.10.04 对含符号链接路径做
  realpath 二次 ACL 校验，消费方 ACL 须同时授权 `/tmp/run/...`（WORKPLAN 坑位 #25）。

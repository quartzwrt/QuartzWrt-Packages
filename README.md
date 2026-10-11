# QuartzWrt-Packages (`quartz` feed)

QuartzWrt 自有核心与适配层。设计规范见 [QuartzWrt-Builder/docs/TDD.md](https://github.com/quartzwrt/QuartzWrt-Builder/blob/main/docs/TDD.md)。

## 职责

- 仅包含 QuartzWrt 专属研发的核心代码：主题、Dashboard、`quartz-daemon`、`quartz-app-api`、第三方包 Bridge 适配层。
- 第三方收集代码一律进 `QuartzWrt-ThirdParty`，不得混入本仓库（物理隔离原则）。

## 目录规范

| 目录 | 内容 |
| :--- | :--- |
| `luci/theme/` | 全局主题 |
| `luci/applications/` | Dashboard 等 SPA 应用 |
| `utils/` | `quartz-daemon`、`quartz-app-api` 等守护进程/服务 |
| `bridge/` | 第三方插件 API 适配层（模式 B） |
| `meta/` | `quartz-defaults` 元数据包 |

## 新增包流程

1. 按上述目录规范建包目录并编写 Makefile。
2. 包名一律带 `quartz-` 前缀，避免与基座或第三方包重名。
3. 需要随发行版默认携带的包，追加到 `meta/quartz-defaults/Makefile` 的 `DEPENDS`。
4. PR 经架构师 Review 后合并；CI 会以构建结果为准验证。

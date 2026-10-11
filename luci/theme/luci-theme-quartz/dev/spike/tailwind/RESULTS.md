# Tailwind 隔离 Spike — 实测结果（2026-10-12）

**结论先行**：生产配置 = **`qz:` 前缀 + `source(none)` 精确扫描 + 主题 `cascade.css` 入层（层序 `theme < base < qz-theme < components < utilities`）+ 少量 preflight 中和规则**。

## 四条机制性发现（v4.3.3 实测，非文档推断）

1. **v4 的前缀语法是 `qz:` 变体形式**（写 HTML 是 `class="qz:flex"`），与 v3 的 `qz-flex` 不同；且前缀只能配置在完整导入 `@import "tailwindcss" prefix(qz)` 上——granular 导入（`tailwindcss/utilities.css`）上的 `prefix()` 会被**静默忽略**。
2. **完整导入必然携带 preflight**（无法"只引 theme+utilities"再单独加前缀）。preflight 位于 `@layer base`。
3. **层序决定 utilities 能否生效**：主题若保持无层（unlayered），无层样式会压制一切层内样式——dashboard 的 utilities 会被主题的 `h2{font-size:20px}` 之类规则压制（实测探针停在 20px/600，本应 30px/700）。因此 cascade.css 必须入层。
4. **层序不能中和 preflight 的"单侧泄漏"**：preflight 里主题未声明过的属性没有任何冲突方，照样生效（实测 `img{display:block}` 把 alert 段落从 20.5px 撑到 55px）。需在主题中显式钉回 UA 默认值——已加入 cascade.css：
   `img, svg, video, canvas, iframe { display: inline; vertical-align: baseline }` 与 `hr { color: inherit }`。

## 测试矩阵（跑 `run-matrix.sh`，Edge 无头 + 计算样式/布局矩形快照对比）

| 行 | 配置 | 污染 diff | utilities 生效 |
| :--- | :--- | :--- | :--- |
| sanity | 无注入（装置稳定性自检） | 0 | — |
| control-unlayered | 对照组：无前缀 + 完整导入（含 preflight），主题无层 | **35** | ❌ |
| chosen-unlayered | 方案 A 但主题无层 | 0（中和规则生效） | ❌（被主题压制） |
| chosen-layered | **生产配置**：`qz:` 前缀 + 主题入层 | **0** | ✅（探针 30px/700） |

- control 行样例污染：`#maincontent` 被 `.container` 挤压 754→640px，h2/h3/p 全链宽度漂移（共 35 项）。
- chosen 行回归断言：输出仅含 `.qz\:*` 选择器（24 处），无 `\.container` 等无前缀选择器。

## 复现步骤

```sh
cd dev/spike/tailwind
./build.sh          # 下载 v4.3.3 CLI → 构建 chosen/control/无层对照 + 产物断言
./run-matrix.sh     # 需先在包根目录起 python3 -m http.server 8899（Edge 经 WSL 互操作截图/取 DOM）
```

原始结果 JSON 存于 `out/result-*.json`。跑矩阵务必使用全新浏览器 profile 且 URL 带缓存破坏参数（python http.server 无 Cache-Control，启发式缓存会喂旧文件——测试页已内置 `?cb=` 处理，脚本已内置 `rm -rf /tmp/...qz-edge-*`）。

## 对 Dashboard（WP-3）的落地要求

- Tailwind 构建入口（v4）：

  ```css
  @import "tailwindcss" prefix(qz) source(none);
  @source "./src/**/*.{html,js,vue,ts}";
  ```

- HTML 中类名一律 `qz:` 形式（如 `qz:flex`、`qz:text-xl`）。
- **token 对接**：在 `@theme` 中把颜色映射到 `var(--qz-*)`。注意 Tailwind 生成的 CSS 变量也带 `qz` 前缀（`--qz-font-sans`、`--qz-radius-sm`、`--qz-text-xl` …），与主题同名 token 存在命名重叠；层序上主题（`qz-theme`）先于 `utilities`，**主题值胜出**（这正是 token 单一来源想要的方向）；`--radius-sm`（Tailwind 0.25rem vs 主题 8px）等语义差异在 dashboard 侧 `@theme` 显式对齐。
- 依赖主题为入层版本（本主题自 2026-10-12 起即是，见 cascade.css 顶部层序注释）。

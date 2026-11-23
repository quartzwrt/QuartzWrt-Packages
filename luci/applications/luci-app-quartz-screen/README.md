# luci-app-quartz-screen

QuartzWrt 小屏控制面（WP-16）：`系统 → 屏幕显示`。

- **状态**：服务/屏幕/分辨率/当前页/亮度/夜间/数据源/错误；真实渲染预览
  （暗/浅可切）；测试图案；重启服务。
- **设置**：开关、亮度、主题、语言、方向、轮播间隔、页面增减/排序、页面
  内容开关、夜间时段调光/熄屏、便签标题与 4 行文本。

依赖 `quartz-screen`。预览直接调用守护进程同一渲染器（base64 PNG），
保证 LuCI 所见即实屏。设计见 `docs/DESIGN.md`。

## 构建

```sh
make package/luci-app-quartz-screen/compile V=s
```

## 验证

- `node --check htdocs/luci-static/resources/view/quartz-screen/*.js`
- 状态页冒烟：`python3 dev/status-smoke.py`（Playwright + 最小 LuCI 桩，
  检查状态字段/4 张预览图/按钮；截图 `dev/status-smoke.png`）。
- VM：`quartz-screen run --fb-file … --demo` 后用 Playwright 走通状态/预览/
  设置保存；真机验收见 `../../utils/quartz-screen/docs/DESIGN.md` §7。

# dev/vm-test —— VM 逐页主题测试（Playwright）

WP-2 逐页视觉验收工具：经 `scripts/test-in-vm.sh` 起的 QEMU 预览台（宿主 8080 → VM:80），
用 Playwright Chromium 登录 LuCI 后逐页全高截图并采集控制台/请求异常，供逐张目检。

## 依赖

```bash
python3 -m pip install --user --break-system-packages playwright pillow
python3 -m playwright install chromium
# 中文字体（免 root 安装到用户目录）：
apt-get download fonts-noto-cjk && dpkg -x fonts-noto-cjk*.deb /tmp/fonts \
  && mkdir -p ~/.local/share/fonts && cp -r /tmp/fonts/usr/share/fonts/opentype/noto ~/.local/share/fonts/ && fc-cache -f
# 彩色 emoji（dockerman 等第三方按钮使用；缺字体时显示豆腐块）：
apt-get download fonts-noto-color-emoji && dpkg -x fonts-noto-color-emoji*.deb /tmp/fonts2 \
  && cp /tmp/fonts2/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf ~/.local/share/fonts/ && fc-cache -f
```

## 用法

```bash
# 前提：VM 已启动
QuartzWrt-Builder/scripts/test-in-vm.sh --detach

python3 shoot.py --mode dark                     # 全量暗色 → ../shots/vm-test-dark/
python3 shoot.py --mode light --password quartz  # 全量浅色 → ../shots/vm-test-light/
python3 shoot.py --mode dark --only v18_system-quartz-theme   # 单页
python3 shoot.py --mode dark --from v30_network-interfaces    # 断点续跑（run-log.jsonl 去重）
python3 shoot.py --mode dark --flat-actions                   # 长图免"操作条中段悬停"假象（见下）
```

## 页面清单（pages.json）

59 项 = 菜单树 50 个 view/template 页（含 8 个 unsatisfied 直访页）+ OpenClash 6 页 + 登录页 + 404 + 退出页。
清单来源：`/cgi-bin/luci/admin/menu` 菜单树 JSON（与侧边栏一致）；新增页面时重新导出菜单树并更新。

## 输出

- `../shots/vm-test-<mode>/<id>.png`：逐页全高截图（viewport 1500×1000，`full_page=True`）
- `../shots/vm-test-<mode>/run-log.jsonl`：每页标题/高度/耗时/控制台错误/4xx 响应/视图文本长度

## 已知注意事项

- **sticky 应用条**：主题的 `.cbi-page-actions` 是 `position: sticky`（贴底 chrome，设计如此）。
  **全页截图会把它定格在"视口底部"位置**——长图里看起来"悬在页面中部"并盖住中段一小条内容，
  是截图拼接的假象（真实浏览始终贴视口底，视口截图可证），非渲染缺陷。**要出干净长图加
  `--flat-actions`**：截图前把该条置为 `position: static`，它回到文档流内的真实位置
  （= 滚到页底时看到的位置）。需要完整证据时另以 DOM 行数（run-log 的 view 长度）交叉验证。
- **实时图表白底**：负载/带宽图表为 LuCI 上游 `view/status/load.js` 内联 `background:#fff`，
  暗色下亦为白底（bootstrap 同）——上游行为，非主题缺陷。
- **未登录状态**：登录页（v01）在独立无会话 context 中截图；若 VM 已设 root 密码，
  其余页面需 `--password <pw>`（`scripts/test-in-vm.sh` 的容器版约定为 `quartz`）。
- 主题静态资源走 `root/www/` 原样安装；VM 内可 `ssh -p 2222 root@localhost "cat > /www/luci-static/quartz/cascade.css"`
  直接热更验证（本次逐页测试即用此法），但**提交前以源码为准**、勿依赖 VM 内改动。

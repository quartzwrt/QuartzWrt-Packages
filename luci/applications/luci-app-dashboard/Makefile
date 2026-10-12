#
# luci-app-dashboard — QuartzWrt 概览仪表盘（登录后默认首页）
#
# 设计规范: docs/DESIGN.md（v1.0 定案）
# 聚合契约: docs/AGGREGATE-API.md（冻结 v1，WP-4 quartz-daemon 实现）
#
# 说明:
#   - 本包只安装文件；前端产物（root/www/luci-static/resources/quartz-dashboard/）
#     由 frontend/ 源码构建后提交入库，固件构建与 CI 构建零 node 依赖（DESIGN.md §4）
#   - 资产放 root/www/（原样安装）：luci.mk 会用 csstidy 1.x 处理 htdocs/ 下 CSS
#     （不识别 @layer，实测会毁样式表）；同理源码目录不能叫 src/
#     （luci.mk 视其为 C 源码并要求 src/Makefile，实测报 "No rule to make target 'install'"）
#   - 占据 admin/dashboard 路由（order 1）作为登录落地页；与基座同名包互斥
#

include $(TOPDIR)/rules.mk

LUCI_TITLE:=QuartzWrt dashboard (default landing page)
# 说明：quartz-daemon（WP-4）落地前不设硬依赖——Dashboard 对"采集器未运行"有完整降级态
# （四态设计见 docs/DESIGN.md §2.2），无 daemon 固件可正常携带与渲染。
# WP-4 落地后在此追加 +quartz-daemon（共享依赖清单同步在 meta/quartz-defaults）。
LUCI_DEPENDS:=+luci-base

# i18n 命名去冲突：luci.mk 会用 LUCI_BASENAME 生成 luci-i18n-<basename>-<lang>，
# 默认从 luci-app-dashboard 剥出 "dashboard"，与基座 luci-mod-dashboard 的 i18n
# 包名/lmo 文件名（dashboard.zh_Hans.lmo）双重冲突（实测导致 kconfig 重名、
# quartz-defaults 依赖解析失败被 defconfig 关闭）。故显式指定 qdashboard：
# 包名 luci-i18n-qdashboard-<lang>，lmo 名随 po 文件名（po/*/qdashboard.po）。
LUCI_BASENAME:=qdashboard

PKG_LICENSE:=Apache-2.0

# 基座 luci feed 自带 luci-mod-dashboard（注册 admin/dashboard 同名路由，本固件未选装）；
# 声明互斥以防用户后装造成路由双定义（DESIGN.md §1.1）
CONFLICTS:=luci-mod-dashboard

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

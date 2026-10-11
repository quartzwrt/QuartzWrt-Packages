#
# luci-app-dashboard — QuartzWrt 概览仪表盘（登录后默认首页）
#
# 设计规范: docs/DESIGN.md（v1.0 定案）
# 聚合契约: docs/AGGREGATE-API.md（冻结 v1，WP-4 quartz-daemon 实现）
#
# 说明:
#   - 本包只安装文件；前端产物（htdocs/luci-static/resources/quartz-dashboard/）
#     由 src/ 源码构建后提交入库，固件构建与 CI 构建零 node 依赖（DESIGN.md §4）
#   - 占据 admin/dashboard 路由（order 1）作为登录落地页；与基座同名包互斥
#

include $(TOPDIR)/rules.mk

LUCI_TITLE:=QuartzWrt dashboard (default landing page)
LUCI_DEPENDS:=+luci-base +quartz-daemon

PKG_LICENSE:=Apache-2.0

# 基座 luci feed 自带 luci-mod-dashboard（注册 admin/dashboard 同名路由，本固件未选装）；
# 声明互斥以防用户后装造成路由双定义（DESIGN.md §1.1）
CONFLICTS:=luci-mod-dashboard

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

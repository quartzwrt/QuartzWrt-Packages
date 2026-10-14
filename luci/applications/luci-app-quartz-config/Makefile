#
# luci-app-quartz-config — QuartzWrt 主题配置页
#
# 与 luci-theme-quartz 配套的独立配置包：
# ① 外观选项存放于 /etc/config/quartz（section 'theme'），由主题模板在每次页面渲染时
#    读出并内联生效；主题在未安装本包时使用内置默认值，可独立工作。
# ② 壁纸（W2.9）：上传/替换/移除 /www/quartz-wallpaper/wallpaper.jpg（主题 theme.js
#    探测该固定路径）；root/www/quartz-wallpaper/.placeholder 只为随包创建投放目录
#    （用户文件不随升级覆盖），root/lib/upgrade/keep.d/quartz-wallpaper 让它在固件
#    sysupgrade 后保留。上传走 luci-base 依赖链上的 cgi-io（cgi-upload）。
#

include $(TOPDIR)/rules.mk

LUCI_TITLE:=QuartzWrt theme configuration
LUCI_DEPENDS:=+luci-base +luci-theme-quartz

PKG_LICENSE:=Apache-2.0

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature

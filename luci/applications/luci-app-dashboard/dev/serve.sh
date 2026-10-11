#!/bin/sh
# luci-app-dashboard 设计预览服务（WP-3）
# 根目录 = QuartzWrt-Packages/luci（预览页内资源用相对路径，换根不影响）
cd "$(dirname "$0")/../../.." || exit 1
echo "预览地址: http://127.0.0.1:8898/applications/luci-app-dashboard/dev/preview.html"
exec python3 -m http.server 8898

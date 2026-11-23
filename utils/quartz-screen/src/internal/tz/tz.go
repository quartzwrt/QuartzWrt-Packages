// Package tz 解决 OpenWrt 启动竞态下的本地时区：/etc/localtime 指向
// /tmp/localtime，而该符号链接由 system init 在启动过程中创建；procd 可能
// 在本守护进程启动时尚未就绪，Go 的 time.Local 会落到 UTC。
package tz

import "time"

// Setup 在进程入口调用：优先 /etc/localtime，其次中国设备常用的
// Asia/Shanghai（/tmp/localtime 通常指向它）；都不可用则保持默认。
func Setup() {
	if loc, err := time.LoadLocation("localtime"); err == nil {
		time.Local = loc
		return
	}
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		time.Local = loc
	}
}

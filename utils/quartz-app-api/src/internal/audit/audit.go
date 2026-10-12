// Package audit 审计日志（DESIGN §3.3）。
//
// rpcd exec 插件路径下 stderr 被 rpcd 吞掉（DESIGN §1.1），审计必须走
// logd：本包经 syslog(unixgram /dev/log) 投递，失败静默（审计不阻塞业务）。
// 不记录任何凭据材料（密码/令牌原文一律不入日志）。
package audit

import (
	"fmt"
	"log/syslog"
	"sync"
)

// Tag syslog 标签。
const Tag = "quartz-app-api"

// Sink 可注入的投递函数（测试用）；默认写 syslog。
var Sink = syslogSink

var (
	mu     sync.Mutex
	writer *syslog.Writer
	broken bool
)

func syslogSink(msg string) {
	mu.Lock()
	defer mu.Unlock()
	if broken {
		return
	}
	if writer == nil {
		w, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_AUTH, Tag)
		if err != nil {
			broken = true
			return
		}
		writer = w
	}
	_ = writer.Info(msg)
}

// Logf 记一条审计（短串、机器可读，格式见 DESIGN §3.3 示例）。
func Logf(format string, args ...any) {
	Sink(fmt.Sprintf(format, args...))
}

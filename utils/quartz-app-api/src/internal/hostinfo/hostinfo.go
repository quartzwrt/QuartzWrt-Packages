// Package hostinfo 采集路由器身份信息（info/login 响应中的 router 字段）。
//
// 数据源与口径对齐 quartz-daemon §3.1（同源）：
// hostname=os.Hostname；model=/tmp/sysinfo/model；release=/etc/quartz-release 的
// QUARTZ_DESCRIPTION → 兜底 /etc/openwrt_release 的 DISTRIB_DESCRIPTION；
// kernel=/proc/sys/kernel/osrelease。
package hostinfo

import (
	"os"
	"strings"
	"time"
)

// Info 返回身份信息（键与 API 文档一致）。
func Info() map[string]any {
	return map[string]any{
		"name":    hostname(),
		"model":   readTrim("/tmp/sysinfo/model"),
		"release": readRelease(),
		"kernel":  readTrim("/proc/sys/kernel/osrelease"),
		"ts":      time.Now().Unix(),
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// readRelease 发行版标识（quartz-defaults 的 /etc/quartz-release → openwrt_release 兜底）。
func readRelease() string {
	if v := readKVFile("/etc/quartz-release", "QUARTZ_DESCRIPTION"); v != "" {
		return v
	}
	return readKVFile("/etc/openwrt_release", "DISTRIB_DESCRIPTION")
}

// readKVFile 读 shell 风格 key='value' 文件中的指定键。
func readKVFile(path, key string) string {
	for _, line := range strings.Split(readFile(path), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k == key {
			return unquote(v)
		}
	}
	return ""
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func readTrim(p string) string {
	return strings.TrimSpace(readFile(p))
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

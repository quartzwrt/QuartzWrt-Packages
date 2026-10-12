package collect

import (
	"os"
	"strconv"
	"strings"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// hostFast 组装 host 节（契约 §3.1）。
func (c *Collector) hostFast(modelFallback string) snapshot.Host {
	h := snapshot.Host{}
	h.Hostname, _ = os.Hostname()
	h.Model = readTrim("/tmp/sysinfo/model")
	if h.Model == "" {
		h.Model = modelFallback
	}
	h.Release = readRelease()
	h.Kernel = readTrim("/proc/sys/kernel/osrelease")
	h.Uptime = readUptime()
	return h
}

// readRelease 发行版标识：优先 /etc/quartz-release（quartz-defaults 提供，
// DESIGN §9-D7），缺失时用 /etc/openwrt_release 的 DISTRIB_DESCRIPTION。
func readRelease() string {
	if v := readKVFile("/etc/quartz-release", "QUARTZ_DESCRIPTION"); v != "" {
		return v
	}
	return readKVFile("/etc/openwrt_release", "DISTRIB_DESCRIPTION")
}

// readKVFile 读 shell 风格 key='value' 文件中的指定键。
func readKVFile(path, key string) string {
	for _, line := range strings.Split(string(readFile(path)), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k == key {
			return unquote(v)
		}
	}
	return ""
}

// readUptime 运行时长（秒，/proc/uptime 首值）。
func readUptime() int64 {
	f := strings.Fields(string(readFile("/proc/uptime")))
	if len(f) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

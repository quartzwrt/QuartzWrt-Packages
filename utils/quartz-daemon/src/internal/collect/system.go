package collect

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// cpuTimes /proc/stat 聚合行的累计值。
type cpuTimes struct {
	total uint64
	idle  uint64
}

// systemFast 组装 system 节（契约 §3.2）。
func (c *Collector) systemFast() snapshot.System {
	var sys snapshot.System

	if f := strings.Fields(string(readFile("/proc/loadavg"))); len(f) >= 3 {
		for i := 0; i < 3; i++ {
			if v, err := strconv.ParseFloat(f[i], 64); err == nil {
				sys.Load[i] = v
			}
		}
	}

	cur := parseStatCPU(readFile("/proc/stat"))
	if c.hasPrevCPU {
		sys.CPUPct = cpuPct(c.prevCPU, cur)
	} else {
		sys.CPUPct = cpuPct(cpuTimes{}, cur) // 首轮：自开机以来的平均值
	}
	c.prevCPU, c.hasPrevCPU = cur, true

	mi := parseMeminfo(readFile("/proc/meminfo"))
	sys.Mem, sys.Swap = mi.mem, mi.swap
	sys.Rootfs = readRootfs("/")
	sys.TempC = readTemp()

	// 2026-10-19 加法：硬件摘要（系统卡底部信息密度；无 cpufreq 的平台省略频率）
	sys.CPUModel = readCPUModel()
	sys.Cores = runtime.NumCPU()
	sys.Procs = readProcs(string(readFile("/proc/loadavg")))
	sys.CPUMHz, sys.CPUMaxMHz = readCPUFreqFrom("/sys/devices/system/cpu")
	return sys
}

// readCPUModel 取 /proc/cpuinfo 的型号串（ARM64 常见 "model name"；
// 旧 ARM 用 "Processor" / "Hardware"；取首个非空值）。
func readCPUModel() string {
	return parseCPUModel(readFile("/proc/cpuinfo"))
}

func parseCPUModel(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "model name", "Hardware", "Processor":
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	return ""
}

// readProcs 从 /proc/loadavg 第 4 段（"1/218"）取进程总数。
func readProcs(loadavg string) int {
	f := strings.Fields(loadavg)
	if len(f) < 4 {
		return 0
	}
	if _, total, ok := strings.Cut(f[3], "/"); ok {
		if v, err := strconv.Atoi(total); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

// readCPUFreqFrom 汇总各核 cpufreq（kHz 文件 → MHz）：
// 当前频率取有值核心的均值，最大频率取全局最大值；无 cpufreq 时返回 0,0。
func readCPUFreqFrom(base string) (curMHz, maxMHz int) {
	dirs, _ := filepath.Glob(filepath.Join(base, "cpu[0-9]*", "cpufreq"))
	var sum, n int
	for _, d := range dirs {
		if v := readKHz(filepath.Join(d, "scaling_cur_freq")); v > 0 {
			sum += v
			n++
		}
		if v := readKHz(filepath.Join(d, "cpuinfo_max_freq")); v > maxMHz {
			maxMHz = v
		}
	}
	if n > 0 {
		curMHz = sum / n
	}
	return curMHz / 1000, maxMHz / 1000
}

func readKHz(path string) int {
	v, err := strconv.Atoi(readTrim(path))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// parseStatCPU 解析 /proc/stat 首行（聚合 cpu 行）。
func parseStatCPU(b []byte) cpuTimes {
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		f := strings.Fields(line)
		var t cpuTimes
		var vals []uint64
		for _, s := range f[1:] {
			v, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				break
			}
			vals = append(vals, v)
			t.total += v
		}
		switch {
		case len(vals) >= 5:
			t.idle = vals[3] + vals[4] // idle + iowait
		case len(vals) >= 4:
			t.idle = vals[3]
		}
		return t
	}
	return cpuTimes{}
}

// cpuPct 由相邻两次采样的 total/idle 差值计算占用百分比（0-100，1 位小数）。
func cpuPct(prev, cur cpuTimes) float64 {
	if cur.total < prev.total || cur.idle < prev.idle {
		return 0 // 计数器重置（热插拔/重启）
	}
	dt := cur.total - prev.total
	di := cur.idle - prev.idle
	if dt == 0 || di > dt {
		return 0
	}
	p := 100 * (1 - float64(di)/float64(dt))
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return round1(p)
}

type memInfo struct {
	mem  snapshot.Bytes
	swap snapshot.Bytes
}

// parseMeminfo 解析 /proc/meminfo（used = Total − MemAvailable；kB → bytes）。
func parseMeminfo(b []byte) memInfo {
	kv := map[string]int64{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		key := strings.TrimSuffix(f[0], ":")
		if v, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			kv[key] = v * 1024
		}
	}
	var mi memInfo
	total := kv["MemTotal"]
	avail := kv["MemAvailable"]
	if avail == 0 {
		avail = kv["MemFree"] // 老内核兜底
	}
	used := total - avail
	if used < 0 {
		used = 0
	}
	mi.mem = snapshot.Bytes{Total: total, Used: used, Pct: pct(used, total)}

	swTotal := kv["SwapTotal"]
	swUsed := swTotal - kv["SwapFree"]
	if swUsed < 0 {
		swUsed = 0
	}
	mi.swap = snapshot.Bytes{Total: swTotal, Used: swUsed, Pct: pct(swUsed, swTotal)}
	return mi
}

// readRootfs 读根文件系统用量（df 口径：total = Blocks×Bsize，
// used = total − Bavail×Bsize）。
func readRootfs(path string) snapshot.Bytes {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return snapshot.Bytes{}
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	avail := int64(st.Bavail) * bsize
	used := total - avail
	if used < 0 {
		used = 0
	}
	return snapshot.Bytes{Total: total, Used: used, Pct: pct(used, total)}
}

// 温度探测白名单（thermal_zone type / hwmon name 子串匹配）。
var (
	tempZoneTypes  = []string{"cpu", "soc", "x86_pkg_temp", "k10temp", "coretemp"}
	tempHwmonNames = []string{"coretemp", "k10temp", "cpu_thermal", "soc_thermal"}
)

// readTemp 探测 CPU 温度（°C，1 位小数）：
// thermal_zone 匹配集合取最大 → hwmon 白名单 → nil（VM 实测无传感器 → null）。
func readTemp() *float64 {
	return readTempFrom("/sys/class/thermal", "/sys/class/hwmon")
}

func readTempFrom(thermalDir, hwmonDir string) *float64 {
	var cands []float64
	zones, _ := filepath.Glob(filepath.Join(thermalDir, "thermal_zone*"))
	for _, z := range zones {
		typ := strings.ToLower(readTrim(filepath.Join(z, "type")))
		if !matchesAny(typ, tempZoneTypes) {
			continue
		}
		if v, ok := readMilliTemp(filepath.Join(z, "temp")); ok {
			cands = append(cands, v)
		}
	}
	if len(cands) == 0 {
		hwmons, _ := filepath.Glob(filepath.Join(hwmonDir, "hwmon*"))
		for _, h := range hwmons {
			name := strings.ToLower(readTrim(filepath.Join(h, "name")))
			if !matchesAny(name, tempHwmonNames) {
				continue
			}
			inputs, _ := filepath.Glob(filepath.Join(h, "temp*_input"))
			for _, in := range inputs {
				if v, ok := readMilliTemp(in); ok {
					cands = append(cands, v)
				}
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	m := cands[0]
	for _, v := range cands {
		if v > m {
			m = v
		}
	}
	return &m
}

// readMilliTemp 读毫摄氏度文件并折算 °C。
func readMilliTemp(path string) (float64, bool) {
	v, err := strconv.ParseInt(readTrim(path), 10, 64)
	if err != nil {
		return 0, false
	}
	return round1(float64(v) / 1000), true
}

func matchesAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

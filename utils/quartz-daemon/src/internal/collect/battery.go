package collect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// powerSupplyDir sysfs 电源节点根目录（pcat-mgr FUSE 挂载点）。
const powerSupplyDir = "/sys/class/power_supply"

// batteryFast 组装 battery 节（契约 §3.10）：读 pcat-mgr 暴露的
// pcat-battery / pcat-charger；节点不存在时 available=false（无电池/服务未运行）。
func (c *Collector) batteryFast() *snapshot.Battery {
	return readBatteryFrom(powerSupplyDir)
}

// readBatteryFrom 从给定 sysfs 根目录读取电池数据（base 便于单测注入）。
//
// pcat-mgr 的 FUSE 文件单位：voltage_now = µV，energy_now/energy_full = µWh，
// capacity = %，status = Full/Charging/Discharging。契约输出：mV / Wh。
func readBatteryFrom(base string) *snapshot.Battery {
	b := &snapshot.Battery{}
	batDir := filepath.Join(base, "pcat-battery")
	chgDir := filepath.Join(base, "pcat-charger")
	if !dirExists(batDir) || !dirExists(chgDir) {
		return b
	}

	// 三个核心文件缺一即视为不可用（FUSE 未挂载/服务异常）。
	capRaw := readFile(filepath.Join(batDir, "capacity"))
	statusRaw := readFile(filepath.Join(batDir, "status"))
	onlineRaw := readFile(filepath.Join(chgDir, "online"))
	if capRaw == nil || statusRaw == nil || onlineRaw == nil {
		return b
	}

	b.Available = true
	b.Present = readBoolFile(filepath.Join(batDir, "present"))
	b.ChargerOnline = parseBool(string(onlineRaw))
	b.Capacity = clampPercent(readIntFile(filepath.Join(batDir, "capacity")))
	b.Status = strings.TrimSpace(string(statusRaw))
	if b.Status == "" {
		b.Status = "Unknown"
	}
	b.VoltageMV = readIntFile(filepath.Join(batDir, "voltage_now")) / 1000
	b.EnergyNowWh = round1(float64(readIntFile(filepath.Join(batDir, "energy_now"))) / 1e6)
	b.EnergyFullWh = round1(float64(readIntFile(filepath.Join(batDir, "energy_full"))) / 1e6)
	b.Technology = readTrim(filepath.Join(batDir, "technology"))
	b.Manufacturer = readTrim(filepath.Join(batDir, "manufacturer"))
	return b
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func readIntFile(path string) int {
	v, err := strconv.Atoi(readTrim(path))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func readBoolFile(path string) bool {
	return parseBool(readTrim(path))
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "online":
		return true
	}
	return false
}

func clampPercent(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseStatCPU(t *testing.T) {
	got := parseStatCPU(readTestdata(t, "proc_stat.txt"))
	// 66004+52631+8210258+1221+12833 = 8342947
	if got.total != 8342947 {
		t.Fatalf("total = %d", got.total)
	}
	// idle + iowait = 8210258+1221
	if got.idle != 8211479 {
		t.Fatalf("idle = %d", got.idle)
	}
}

func TestCPUPct(t *testing.T) {
	prev := parseStatCPU(readTestdata(t, "proc_stat.txt"))
	cur := prev
	cur.idle += 800
	cur.total += 1000 // busy delta 200 / total delta 1000 → 20.0%
	if got := cpuPct(prev, cur); got != 20.0 {
		t.Fatalf("cpuPct = %v, want 20.0", got)
	}
	// 计数器重置 → 0
	if got := cpuPct(cur, prev); got != 0 {
		t.Fatalf("reset cpuPct = %v, want 0", got)
	}
}

func TestParseMeminfo(t *testing.T) {
	mi := parseMeminfo(readTestdata(t, "proc_meminfo.txt"))
	wantTotal := int64(1007832) * 1024
	if mi.mem.Total != wantTotal {
		t.Fatalf("mem total = %d, want %d", mi.mem.Total, wantTotal)
	}
	wantUsed := int64(1007832-846056) * 1024
	if mi.mem.Used != wantUsed {
		t.Fatalf("mem used = %d, want %d", mi.mem.Used, wantUsed)
	}
	if mi.swap.Total != 0 || mi.swap.Used != 0 || mi.swap.Pct != 0 {
		t.Fatalf("swap should be zero, got %+v", mi.swap)
	}
}

func TestReadRootfs(t *testing.T) {
	b := readRootfs("/")
	if b.Total <= 0 {
		t.Fatalf("rootfs total = %d", b.Total)
	}
	if b.Pct < 0 || b.Pct > 100 {
		t.Fatalf("rootfs pct = %v", b.Pct)
	}
}

func TestReadTempFrom(t *testing.T) {
	// thermal_zone 匹配（type 含 cpu）：milli°C → °C
	base := t.TempDir()
	z := filepath.Join(base, "thermal", "thermal_zone0")
	if err := os.MkdirAll(z, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(z, "type"), []byte("cpu-thermal\n"), 0o644)
	os.WriteFile(filepath.Join(z, "temp"), []byte("46000\n"), 0o644)
	got := readTempFrom(filepath.Join(base, "thermal"), filepath.Join(base, "hwmon"))
	if got == nil || *got != 46.0 {
		t.Fatalf("zone temp = %v, want 46.0", got)
	}

	// 无匹配 zone → hwmon 白名单兜底
	base2 := t.TempDir()
	hw := filepath.Join(base2, "hwmon", "hwmon0")
	if err := os.MkdirAll(hw, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(hw, "name"), []byte("coretemp\n"), 0o644)
	os.WriteFile(filepath.Join(hw, "temp1_input"), []byte("52000\n"), 0o644)
	got2 := readTempFrom(filepath.Join(base2, "thermal"), filepath.Join(base2, "hwmon"))
	if got2 == nil || *got2 != 52.0 {
		t.Fatalf("hwmon temp = %v, want 52.0", got2)
	}

	// 全无 → nil（VM 实机形态）
	if got3 := readTempFrom(t.TempDir(), t.TempDir()); got3 != nil {
		t.Fatalf("empty temp = %v, want nil", *got3)
	}
}

func TestReadKVFileAndRelease(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rel")
	os.WriteFile(p, []byte("QUARTZ_DESCRIPTION='QuartzWrt 0.1.0 (ImmortalWrt 25.12)'\n"), 0o644)
	if v := readKVFile(p, "QUARTZ_DESCRIPTION"); v != "QuartzWrt 0.1.0 (ImmortalWrt 25.12)" {
		t.Fatalf("kv = %q", v)
	}
	if v := readKVFile(readTestdataPath(t, "openwrt_release.txt"), "DISTRIB_DESCRIPTION"); v != "ImmortalWrt 25.12-SNAPSHOT r0+38247-ce35a9e4ef" {
		t.Fatalf("openwrt_release kv = %q", v)
	}
}

func readTestdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func TestReadUptime(t *testing.T) {
	if got := readUptime(); got <= 0 {
		t.Fatalf("uptime = %d", got)
	}
}

func TestParseCPUModel(t *testing.T) {
	arm := []byte("processor\t: 0\nBogoMIPS\t: 48.00\nmodel name\t: ARMv8 Processor rev 4 (v8l)\n")
	if got := parseCPUModel(arm); got != "ARMv8 Processor rev 4 (v8l)" {
		t.Fatalf("arm model = %q", got)
	}
	// Processor/Hardware 兜底（老 ARM 内核无 model name）
	old := []byte("Processor\t: ARMv7 Processor rev 1 (v7l)\nHardware\t: BCM2835\n")
	if got := parseCPUModel(old); got != "ARMv7 Processor rev 1 (v7l)" {
		t.Fatalf("old model = %q", got)
	}
	if got := parseCPUModel([]byte("nothing here\n")); got != "" {
		t.Fatalf("empty model = %q", got)
	}
}

func TestReadProcs(t *testing.T) {
	if got := readProcs("0.57 0.60 0.56 1/218 12335\n"); got != 218 {
		t.Fatalf("procs = %d, want 218", got)
	}
	if got := readProcs("garbage"); got != 0 {
		t.Fatalf("procs garbage = %d", got)
	}
}

func TestReadCPUFreqFrom(t *testing.T) {
	base := t.TempDir()
	for i, khz := range []string{"1200000", "600000"} {
		dir := filepath.Join(base, "cpu"+string(rune('0'+i)), "cpufreq")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "scaling_cur_freq"), []byte(khz+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "cpuinfo_max_freq"), []byte("1800000\n"), 0o644)
	}
	cur, max := readCPUFreqFrom(base)
	if cur != 900 || max != 1800 {
		t.Fatalf("freq = %d/%d MHz, want 900/1800", cur, max)
	}
	if cur, max := readCPUFreqFrom(t.TempDir()); cur != 0 || max != 0 {
		t.Fatalf("empty freq = %d/%d", cur, max)
	}
}

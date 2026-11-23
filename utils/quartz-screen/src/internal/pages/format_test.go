package pages

import (
	"testing"

	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

func TestFmtRate(t *testing.T) {
	// 快照契约是 Byte/s，显示单位是 bit/s（×8）。
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 bps"},
		{1, "8 bps"},
		{125, "1 Kbps"},
		{62500, "500 Kbps"},        // 500 Kbps
		{6250000, "50.0 Mbps"},     // 50 MB/s → 400 Mbps
		{50000000, "400 Mbps"},     // 50 MB/s → 400 Mbps
		{-5, "0 bps"},
	}
	for _, c := range cases {
		if got := fmtRate(c.in); got != c.want {
			t.Fatalf("fmtRate(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRateParts(t *testing.T) {
	// 主卡数值同样按 Byte/s 入参、bit/s 显示。
	cases := []struct {
		in       int64
		val, uni string
	}{
		{0, "0", "bps"},
		{125, "1", "Kbps"},
		{6250000, "50.0", "Mbps"},
		{50000000, "400", "Mbps"},
	}
	for _, c := range cases {
		v, u := rateParts(c.in)
		if v != c.val || u != c.uni {
			t.Fatalf("rateParts(%d) = %q/%q, want %q/%q", c.in, v, u, c.val, c.uni)
		}
	}
}

func TestOperatorInfo(t *testing.T) {
	cases := []struct {
		raw, name, icon string
	}{
		{"中国联通", "中国联通", "china-unicom"},
		{"46001", "中国联通", "china-unicom"},
		{"China Mobile", "中国移动", "china-mobile"},
		{"46011", "中国电信", "china-telecom"},
		{"中国广电", "中国广电", ""},
		{"", "—", ""},
	}
	for _, c := range cases {
		name, icon := operatorInfo("zh", c.raw)
		if name != c.name || icon != c.icon {
			t.Fatalf("operatorInfo(%q) = %q/%q, want %q/%q", c.raw, name, icon, c.name, c.icon)
		}
	}
	if name, icon := operatorInfo("en", "46001"); name != "China Unicom" || icon != "china-unicom" {
		t.Fatalf("en operatorInfo = %q/%q", name, icon)
	}
}

func TestWirelessLabels(t *testing.T) {
	if got := wifiSecurity("sae"); got != "WPA3-SAE" {
		t.Fatalf("sae = %q", got)
	}
	if got := wifiSecurity("none"); got != "开放" {
		t.Fatalf("none = %q", got)
	}
	if got := wifiStandard("HE80"); got != "802.11ax" {
		t.Fatalf("HE80 = %q", got)
	}
	if got := wifiStandard("VHT80"); got != "802.11ac" {
		t.Fatalf("VHT80 = %q", got)
	}
	if got := wifiStandard("HT40"); got != "802.11n" {
		t.Fatalf("HT40 = %q", got)
	}
	if got := simLabel("zh", "ready"); got != "就绪" {
		t.Fatalf("sim ready = %q", got)
	}
}

func TestShortIPv6(t *testing.T) {
	if got := shortIPv6("240e:3b8:1a2f:8c00::2"); got != "240e:…:8c00::2" {
		t.Fatalf("shortIPv6 = %q", got)
	}
	if got := shortIPv6("240e::2"); got != "240e::2" {
		t.Fatalf("short shortIPv6 = %q", got)
	}
}

func TestSizeAndUptime(t *testing.T) {
	if got := sizePair(1610612736, 4<<30); got != "1.5 / 4.0 GB" {
		t.Fatalf("sizePair = %q", got)
	}
	if v, u := uptimeParts(28*60+12, "zh"); v != "28" || u != "分钟" {
		t.Fatalf("uptimeParts = %q %q", v, u)
	}
	if v, u := uptimeParts(3*86400+14*3600, "en"); v != "3" || u != "d" {
		t.Fatalf("uptimeParts days = %q %q", v, u)
	}
}

func TestDataNormalization(t *testing.T) {
	if validOperator("????") || validOperator("---") || validOperator("") {
		t.Fatal("invalid operator accepted")
	}
	if !validOperator("46001") || !validOperator("中国联通") {
		t.Fatal("valid operator rejected")
	}
	modem := &snapshot.Modem{NetworkMode: "5G SA", CellInfo: &snapshot.CellInfo{Band: "78", DLBandwidth: "20M"}}
	if got := fmtBand(modem); got != "n78" {
		t.Fatalf("fmtBand = %q", got)
	}
	if got := fmtBandwidth("20M"); got != "20MHz" {
		t.Fatalf("fmtBandwidth = %q", got)
	}
	if got := fmtBandwidth("100MHz"); got != "100MHz" {
		t.Fatalf("fmtBandwidth Hz = %q", got)
	}
	if got := fmtBandwidth("100"); got != "100MHz" {
		t.Fatalf("fmtBandwidth numeric = %q", got)
	}
	if got := shortRelease("QuartzWrt 1.0 (ImmortalWrt 25.12)"); got != "QuartzWrt 1.0" {
		t.Fatalf("shortRelease = %q", got)
	}
	bad := -300.0
	if got := fmtTemp("zh", &bad, "c"); got != "—" {
		t.Fatalf("fmtTemp invalid = %q", got)
	}
	ok := 39.0
	if got := fmtTemp("zh", &ok, "c"); got != "39°C" {
		t.Fatalf("fmtTemp valid = %q", got)
	}
	if n, ic := operatorOverride("zh", "unicom"); n != "中国联通" || ic != "china-unicom" {
		t.Fatalf("operatorOverride = %q/%q", n, ic)
	}
	ci := &snapshot.CellInfo{MCC: "460", MNC: "01"}
	if n, ic := operatorInfo("zh", ci.MCC+ci.MNC); n != "中国联通" || ic != "china-unicom" {
		t.Fatalf("cell PLMN operator = %q/%q", n, ic)
	}
}

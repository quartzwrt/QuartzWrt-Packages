package collect

import (
	"fmt"
	"testing"
)

func TestParseARP(t *testing.T) {
	e := parseARP(readTestdata(t, "proc_arp.txt"))
	// 样张含两条：一条 Flags=0x2 完整、一条 0x0 被滤除
	if len(e) != 1 {
		t.Fatalf("entries = %d, want 1", len(e))
	}
	if e[0].MAC != "52:55:c0:a8:01:02" || e[0].IP != "192.168.1.2" || e[0].Device != "br-lan" {
		t.Fatalf("entry = %+v", e[0])
	}
}

func TestParseLeases(t *testing.T) {
	l := parseLeases(readTestdata(t, "dhcp_leases_sample.txt"))
	if got := l["aa:bb:cc:dd:ee:01"]; got.IP != "192.168.1.101" || got.Host != "NAS" {
		t.Fatalf("lease 1 = %+v", got)
	}
	if got := l["aa:bb:cc:dd:ee:02"]; got.Host != "" {
		t.Fatalf("wildcard host should be empty, got %q", got.Host)
	}
	if len(parseLeases(readTestdata(t, "dhcp_leases_empty.txt"))) != 0 {
		t.Fatal("empty lease file should parse to empty map")
	}
}

func TestIsLANSide(t *testing.T) {
	cases := []struct {
		dev, wan string
		want     bool
	}{
		{"br-lan", "eth0", true},
		{"eth0", "eth0", false}, // WAN 承载口
		{"docker0", "", false},  // 虚拟
		{"veth1234", "", false}, // 虚拟
		{"lo", "", false},       // 回环
		{"pppoe-wan", "pppoe-wan", false},
	}
	for _, c := range cases {
		if got := isLANSide(c.dev, c.wan); got != c.want {
			t.Fatalf("isLANSide(%q,%q) = %v, want %v", c.dev, c.wan, got, c.want)
		}
	}
}

func TestMergeClients(t *testing.T) {
	arp := []arpEntry{
		{IP: "192.168.1.10", MAC: "aa:bb:cc:dd:ee:01", Device: "br-lan"},
		{IP: "100.64.7.23", MAC: "ff:ff:ff:ff:ff:ff", Device: "eth0"}, // WAN 侧 → 滤除
	}
	leases := map[string]leaseInfo{
		"aa:bb:cc:dd:ee:01": {IP: "192.168.1.101", Host: "NAS"},
	}
	wifi := map[string]wifiClient{
		"bb:bb:bb:bb:bb:02": {Iface: "wlan0", InactiveMs: 5000, HasInact: true},
		"cc:cc:cc:cc:cc:03": {Iface: "wlan0"}, // 无 inactive：last_seen = now
	}
	got := mergeClients(arp, leases, wifi, "eth0", 1000)

	if got.Total != 3 {
		t.Fatalf("total = %d, want 3 (arp 1 + wifi 2)", got.Total)
	}
	byMAC := map[string]struct {
		ip, host, iface string
		last            int64
	}{}
	for _, c := range got.Items {
		byMAC[c.MAC] = struct {
			ip, host, iface string
			last            int64
		}{c.IP, c.Host, c.Iface, c.LastSeen}
	}
	// ARP 条目：IP 已有时不被租约覆盖；主机名取租约
	if v := byMAC["aa:bb:cc:dd:ee:01"]; v.ip != "192.168.1.10" || v.host != "NAS" || v.last != 1000 {
		t.Fatalf("arp entry = %+v", v)
	}
	// 无线含 inactive：last_seen = now − 5s
	if v := byMAC["bb:bb:bb:bb:bb:02"]; v.last != 995 || v.iface != "wlan0" {
		t.Fatalf("wifi entry = %+v", v)
	}
	// 无线无 inactive：last_seen = now（排最前）
	if v := byMAC["cc:cc:cc:cc:cc:03"]; v.last != 1000 {
		t.Fatalf("wifi no-inactive entry = %+v", v)
	}
	if got.Items[0].MAC != "aa:bb:cc:dd:ee:01" && got.Items[0].MAC != "cc:cc:cc:cc:cc:03" {
		t.Fatalf("ordering wrong: first = %s", got.Items[0].MAC)
	}
}

func TestMergeClientsCap32(t *testing.T) {
	var arp []arpEntry
	for i := 0; i < 40; i++ {
		mac := fmt.Sprintf("aa:bb:cc:dd:ee:%02x", i)
		arp = append(arp, arpEntry{IP: "192.168.1.10", MAC: mac, Device: "br-lan"})
	}
	got := mergeClients(arp, nil, nil, "", 100)
	if got.Total != 40 {
		t.Fatalf("total = %d, want 40", got.Total)
	}
	if len(got.Items) != 32 {
		t.Fatalf("items = %d, want capped 32", len(got.Items))
	}
}

func TestParseIWStationDump(t *testing.T) {
	m := parseIWStationDump(string(readTestdata(t, "iw_station_dump.txt")))
	if len(m) != 2 {
		t.Fatalf("stations = %d, want 2", len(m))
	}
	if v := m["a4:83:e7:1c:2d:9f"]; v != 1500 {
		t.Fatalf("inactive = %d, want 1500", v)
	}
	if v := m["3c:22:fb:9e:11:04"]; v != 65000 {
		t.Fatalf("inactive = %d, want 65000", v)
	}
}

func TestParseIwinfoAssoclist(t *testing.T) {
	s := "A4:83:E7:1C:2D:9F  -45dBm / -95dBm (SNR 50)  100 ms ago\nnot a mac line\n"
	macs := parseIwinfoAssoclist(s)
	if len(macs) != 1 || macs[0] != "a4:83:e7:1c:2d:9f" {
		t.Fatalf("macs = %v", macs)
	}
}

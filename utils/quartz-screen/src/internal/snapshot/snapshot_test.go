package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `{
  "v": 1,
  "ts": 1760000000,
  "interval": 2,
  "host": {"hostname":"NL68K","model":"XiGuaPi V3","release":"QuartzWrt 0.2.4","kernel":"6.12.112","uptime":306120},
  "system": {"load":[0.42,0.31,0.25],"cpu_pct":7.8,
    "mem":{"total":4294967296,"used":1600000000,"pct":37.3},
    "rootfs":{"total":8589934592,"used":1800000000,"pct":21.0},
    "temp_c":46.5},
  "net": {"wan":{"up":true,"ifname":"eth1","proto":"dhcp","ipv4":"192.168.1.2",
    "ipv6":"240e::2","rx_bps":12400000,"tx_bps":3100000},
    "series":{"step":2,"rx":[1,2,3],"tx":[4,5,6]}},
  "clients": {"total":23,"items":[
    {"mac":"aa:bb:cc:dd:ee:01","ip":"192.168.1.10","host":"NAS","iface":"br-lan","last_seen":1760000000}]},
  "cellular": {"available":true,"updated":1760000000,"modems":[
    {"id":"1_1","model":"RM500Q-GL","registered":true,"operator":"中国联通",
     "network_mode":"5G SA","network_type":"TDD NR5G",
     "cell_info":{"band":"n78","dl_bandwidth":"100MHz"},
     "signal":{"rsrp":-82,"sinr":21.4,"quality_pct":78,
       "series":{"step":10,"rsrp":[-80,-82]}},
     "dial":{"running":true,"ipv4":"10.64.1.2"},
     "traffic":{"available":true,"rx_bytes":12300000000,"tx_bytes":1200000000},
     "sim":{"status":"ready","isp":"46001"}}]},
  "errors": []
}`

func TestLoadSample(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, mod, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if mod == 0 {
		t.Fatal("mtime is zero")
	}
	if s.Host.Hostname != "NL68K" || s.System.CPU != 7.8 || s.System.TempC == nil || *s.System.TempC != 46.5 {
		t.Fatalf("host/system: %+v", s)
	}
	if s.Net.WAN.IPv4 != "192.168.1.2" || !s.Net.WAN.Up {
		t.Fatalf("net: %+v", s.Net.WAN)
	}
	if s.Clients == nil || s.Clients.Total != 23 || s.Clients.Items[0].Host != "NAS" {
		t.Fatalf("clients: %+v", s.Clients)
	}
	m := s.Cellular.HeroModem()
	if m == nil || m.Operator != "中国联通" || m.CellInfo == nil || m.CellInfo.Band != "n78" {
		t.Fatalf("modem: %+v", m)
	}
	if m.Signal == nil || m.Signal.RSRP == nil || *m.Signal.RSRP != -82 {
		t.Fatalf("signal: %+v", m.Signal)
	}
}

func TestHeroModemUnavailable(t *testing.T) {
	var c Cellular
	if c.HeroModem() != nil {
		t.Fatal("empty cellular should have no modem")
	}
	c.Available = true
	if c.HeroModem() != nil {
		t.Fatal("no modems should be nil")
	}
}

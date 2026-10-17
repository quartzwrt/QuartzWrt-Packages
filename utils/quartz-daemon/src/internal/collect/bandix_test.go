package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

func readBandixFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseBandixUCI(t *testing.T) {
	out := `bandix.general.iface='br-lan'
bandix.general.port='8686'
bandix.traffic.enabled='1'
bandix.traffic.offline_timeout='120'
bandix.connections.enabled='0'
bandix.dns.enabled='1'
`
	st := parseBandixUCI(out)
	if !st.installed || !st.traffic || st.connections || !st.dns {
		t.Fatalf("uci state = %+v", st)
	}
	if st.offlineTimeoutS != 120 {
		t.Fatalf("offline timeout = %d, want 120", st.offlineTimeoutS)
	}

	empty := parseBandixUCI("")
	if empty.installed || empty.traffic {
		t.Fatalf("empty uci should be uninstalled: %+v", empty)
	}
	if empty.offlineTimeoutS != bandixDefaultOfflineTimeout {
		t.Fatalf("default offline timeout = %d", empty.offlineTimeoutS)
	}

	bad := parseBandixUCI("bandix.traffic.offline_timeout='abc'\nbandix.traffic.enabled='1'")
	if bad.offlineTimeoutS != bandixDefaultOfflineTimeout || !bad.traffic {
		t.Fatalf("bad timeout should fall back: %+v", bad)
	}
}

// 正常路径：在线判据（含秒级时间戳兜底）、WAN 合计、top 排序/截断/pct、conn/dns 摘要。
func TestBuildBandixFull(t *testing.T) {
	now := time.UnixMilli(1_760_000_100_000) // 2025-10-09 附近，与 fixture last 对齐
	uci := bandixUCI{installed: true, traffic: true, connections: true, dns: true, offlineTimeoutS: 120}

	got, err := buildBandix(
		readBandixFixture(t, "bandix_status.json"),
		readBandixFixture(t, "bandix_connections.json"),
		readBandixFixture(t, "bandix_dns_stats.json"),
		uci, now)
	if err != nil {
		t.Fatalf("buildBandix: %v", err)
	}
	if !got.Available || got.Devices != 4 {
		t.Fatalf("available=%v devices=%d", got.Available, got.Devices)
	}
	// fixture：d1 last=now-30s(在线)、d2 last=now-200s(离线，>120s)、
	//         d3 秒级时间戳 now-60s(在线)、d4 last=0(离线) → online=2
	if got.Online != 2 {
		t.Fatalf("online = %d, want 2", got.Online)
	}
	// WAN 合计 = Σ w_rx_r / w_tx_r
	if got.WAN == nil || got.WAN.RxBps != 3000 || got.WAN.TxBps != 600 {
		t.Fatalf("wan = %+v, want rx=3000 tx=600", got.WAN)
	}
	// top：按累计降序、≤8（fixture 4 台 → 全量）；pct = 各自累计/总量
	if len(got.Top) != 4 {
		t.Fatalf("top len = %d, want 4", len(got.Top))
	}
	if got.Top[0].MAC != "aa:bb:cc:dd:ee:01" { // 累计 2000+0 最高
		t.Fatalf("top[0] = %+v", got.Top[0])
	}
	total := int64(2000 + 0 + 500 + 100 + 400 + 0 + 100 + 0)
	wantPct := round1(float64(2000) / float64(total) * 100)
	if got.Top[0].Pct != wantPct {
		t.Fatalf("top[0].pct = %v, want %v", got.Top[0].Pct, wantPct)
	}
	if got.Conn == nil || got.Conn.Total != 12 || got.Conn.TCP != 10 || got.Conn.UDP != 2 {
		t.Fatalf("conn = %+v", got.Conn)
	}
	if got.DNS == nil || got.DNS.Queries != 100 {
		t.Fatalf("dns = %+v", got.DNS)
	}

	// 序列化形状：available + 数字字段名与契约一致
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"available", "devices", "online", "wan", "top", "conn", "dns"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("marshal missing key %q: %s", k, b)
		}
	}
}

// top 截断：>8 台设备只保留前 8（按累计降序）。
func TestBuildBandixTopCap(t *testing.T) {
	var raw []byte
	raw = append(raw, []byte(`{"ok":true,"api":1,"data":{"d":[`)...)
	const hexes = "0102030405060708090a0b0c"
	for i := 0; i < 12; i++ {
		if i > 0 {
			raw = append(raw, ',')
		}
		raw = append(raw, []byte(`{"mac":"aa:bb:cc:dd:ee:`+hexes[i*2:i*2+2]+
			`","ip4":"192.168.1.1","host":"h","last":1760000100000,"t_rx_b":`+
			itoa(1000-i*10)+`,"t_tx_b":0,"w_rx_r":0,"w_tx_r":0}`)...)
	}
	raw = append(raw, []byte(`]}}`)...)

	got, err := buildBandix(raw, nil, nil,
		bandixUCI{installed: true, traffic: true, offlineTimeoutS: 600},
		time.UnixMilli(1_760_000_100_000))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Top) != snapshot.MaxBandixTop {
		t.Fatalf("top len = %d, want %d", len(got.Top), snapshot.MaxBandixTop)
	}
	if got.Top[0].RxB != 1000 || got.Top[7].RxB != 930 {
		t.Fatalf("top ordering wrong: %+v", got.Top)
	}
}

// 降级：上游信封 ok=false → error（collector 层会转成 null + errors 条目）。
func TestBuildBandixUpstreamError(t *testing.T) {
	raw := []byte(`{"ok":false,"api":1,"error":{"code":"data_unavailable","message":"Traffic monitoring is not enabled"}}`)
	_, err := buildBandix(raw, nil, nil, bandixUCI{traffic: true}, time.Now())
	if err == nil {
		t.Fatal("expected error for ok=false envelope")
	}
}

// 可选摘要降级：conn/dns 应答缺失或 ok=false → 字段省略（不阻塞本节）。
func TestBuildBandixOptionalSummaries(t *testing.T) {
	status := readBandixFixture(t, "bandix_status.json")
	badConn := []byte(`{"ok":false,"api":1,"error":{"code":"data_unavailable","message":"Connection monitoring is not enabled"}}`)

	got, err := buildBandix(status, badConn, nil,
		bandixUCI{installed: true, traffic: true, offlineTimeoutS: 120},
		time.UnixMilli(1_760_000_100_000))
	if err != nil {
		t.Fatal(err)
	}
	if got.Conn != nil || got.DNS != nil {
		t.Fatalf("expected omitted summaries, got conn=%+v dns=%+v", got.Conn, got.DNS)
	}
	b, _ := json.Marshal(got)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["conn"]; ok {
		t.Fatalf("conn should be omitted: %s", b)
	}
}

// available=false 序列化形状：仅 available 字段（供前端 visible 谓词判定）。
func TestBandixUnavailableShape(t *testing.T) {
	b, err := json.Marshal(&snapshot.Bandix{Available: false})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"available":false}` {
		t.Fatalf("unavailable shape = %s", b)
	}
}

// itoa 小工具（避免引入 strconv 到测试头部）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// 上游把"核心不可达"伪造成空列表（get_device_status 源码核实）——设备表为空视为
// 不可用（卡片隐藏），避免 "0/0" 误导；非空才正常出数。
func TestBandixEmptyDeviceListIsUnavailable(t *testing.T) {
	raw := []byte(`{"ok":true,"api":1,"data":{"d":[]}}`)
	out, err := buildBandix(raw, nil, nil, bandixUCI{installed: true, traffic: true, offlineTimeoutS: 600}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !out.Available || out.Devices != 0 {
		t.Fatalf("buildBandix 空表应可用但 0 台：%+v", out)
	}
}

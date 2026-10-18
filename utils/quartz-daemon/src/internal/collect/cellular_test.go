package collect

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// fixtureFetch 按 (method, section) 返回预置应答；缺失的键视为调用失败。
type fixtureFetch map[string]string

func (f fixtureFetch) fetch(method, section string) ([]byte, error) {
	key := section + "." + method
	if v, ok := f[key]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("not found")
}

// quectelFixture 一份完整的 Quectel 模组应答集（形状取自上游 generic.sh/quectel.sh）。
func quectelFixture(section string) fixtureFetch {
	f := fixtureFetch{}
	put := func(method, body string) { f[section+"."+method] = body }
	put("base_info", `{"modem_info":[
		{"key":"name","value":"RM500U-CN","class":"base"},
		{"key":"manufacturer","value":"Quectel","class":"base"},
		{"key":"revision","value":"RM500UCNCAR03A09M4G","class":"base"},
		{"key":"at_port","value":"/dev/ttyUSB3","class":"base"},
		{"key":"temperature","value":"42.5 °C","class":"base"},
		{"key":"voltage","value":"3131 mV","class":"base"},
		{"key":"connect_status","value":"Yes","class":"base"}]}`)
	put("network_info", `{"modem_info":[
		{"key":"ISP","value":"中国联通","class":"network"},
		{"key":"Network Type","value":"5G NSA","class":"network"},
		{"key":"CQI UL","value":"6","class":"network"},
		{"key":"CQI DL","value":"5","class":"network"},
		{"key":"AMBR UL","value":"100","class":"network"},
		{"key":"AMBR DL","value":"500","class":"network"},
		{"key":"IP Address","value":"10.42.7.23","class":"network"},
		{"key":"Rx Rate","value":"18.2 MB/s","class":"network"},
		{"key":"Tx Rate","value":"2.4 MB/s","class":"network"}]}`)
	put("cell_info", `{"modem_info":[
		{"key":"network_mode","value":"NR5G-SA Mode","class":"cell"},
		{"key":"Duplex Mode","value":"TDD","class":"cell"},
		{"key":"MCC","value":"460","class":"cell"},
		{"key":"MNC","value":"01","class":"cell"},
		{"key":"TAC","value":"78C300","class":"cell"},
		{"key":"Cell ID","value":"78C088002","class":"cell"},
		{"key":"Physical Cell ID","value":"161","class":"cell"},
		{"key":"ARFCN","value":"627264","class":"cell"},
		{"key":"Band","value":"78","class":"cell"},
		{"key":"UL Bandwidth","value":"400","class":"cell"},
		{"key":"DL Bandwidth","value":"400","class":"cell"},
		{"key":"SCS","value":"30","class":"cell"},
		{"key":"RSRP","value":"-78 dBm","min_value":"-140","max_value":"-44","unit":"dBm","type":"progress_bar","class":"cell"},
		{"key":"RSRQ","value":"-11.0 dB","min_value":"-19.5","max_value":"-3","unit":"dB","type":"progress_bar","class":"cell"},
		{"key":"SINR","value":"22 dB","min_value":"0","max_value":"30","unit":"dB","type":"progress_bar","class":"cell"},
		{"key":"RSSI","value":"-51 dBm","min_value":"-120","max_value":"-20","unit":"dBm","type":"progress_bar","class":"cell"},
		{"key":"Srxlev","value":"","class":"cell"}]}`)
	put("get_current_band", `{"current_band":{"status":"ok","vendor":"quectel","network_mode":"EN-DC","cells":[
		{"role":"pcc","rat":"LTE","band":"3","band_name":"LTE B3","channel":"1300","channel_type":"EARFCN","pci":"293","ul_bandwidth":"20","dl_bandwidth":"20","scs":""},
		{"role":"nsa","rat":"NR","band":"78","band_name":"NR n78","channel":"627264","channel_type":"NR-ARFCN","pci":"293","ul_bandwidth":"","dl_bandwidth":"100","scs":"30kHz"}]}}`)
	put("dial_status", `{"running":"true"}`)
	put("get_stats", `{"available":true,"updated_at":1760000000,"total_rx_bytes":46180000000,"total_tx_bytes":3832000000}`)
	put("get_traffic_reset_schedule", `{"enabled":true,"reset_type":"monthly","hour":0,"day":1,"minute":0,"line":null}`)
	put("sim_info", `{"modem_info":[{"key":"SIM Status","value":"ready"},{"key":"ISP","value":"46001"},{"key":"SIM Slot","value":"1"}]}`)
	put("get_sim_switch_capabilities", `{"supportSwitch":"0"}`)
	put("get_sim_slot", `{"slot":"1"}`)
	put("get_mode", `{"mode":"qmi"}`)
	put("get_network_prefer", `{"network_prefer":{"3G":"0","4G":"1","5G":"1"}}`)
	put("get_lockband", `{"band_class":"NR","lock_band":"41,78,79"}`)
	put("get_reboot_caps", `{"hard":true,"soft":true}`)
	return f
}

func TestBuildCellularQuectel(t *testing.T) {
	store := newCellularSeriesStore()
	now := time.Unix(1760000000, 0)
	out, errs := buildCellular([]string{"modem1"}, quectelFixture("modem1").fetch, store, now)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !out.Available || len(out.Modems) != 1 {
		t.Fatalf("want available with 1 modem, got %+v", out)
	}
	m := out.Modems[0]
	if m.ID != "modem1" || m.Model != "RM500U-CN" || m.Vendor != "quectel" {
		t.Fatalf("base info mismatch: %+v", m)
	}
	if m.Firmware != "RM500UCNCAR03A09M4G" {
		t.Fatalf("firmware: %q", m.Firmware)
	}
	if m.TempC == nil || *m.TempC != 42.5 {
		t.Fatalf("temp: %v", m.TempC)
	}
	if m.Operator != "中国联通" {
		t.Fatalf("operator: %q", m.Operator)
	}
	if m.ATPort != "/dev/ttyUSB3" || m.VoltageMV != 3131 || m.ConnectStatus != "Yes" {
		t.Fatalf("base extras: at_port=%q voltage=%d connect=%q", m.ATPort, m.VoltageMV, m.ConnectStatus)
	}
	if m.NetworkType != "5G NSA" || m.CQIUL != 6 || m.CQIDL != 5 || m.AMBRUL != 100 || m.AMBRDL != 500 {
		t.Fatalf("network extras: type=%q cqi=%d/%d ambr=%d/%d", m.NetworkType, m.CQIUL, m.CQIDL, m.AMBRUL, m.AMBRDL)
	}
	if m.CellInfo == nil || m.CellInfo.Duplex != "TDD" || m.CellInfo.MCC != "460" ||
		m.CellInfo.MNC != "01" || m.CellInfo.TAC != "78C300" || m.CellInfo.CellID != "78C088002" {
		t.Fatalf("cell info: %+v", m.CellInfo)
	}
	if m.SIM == nil || m.SIM.ISP != "46001" {
		t.Fatalf("sim isp: %+v", m.SIM)
	}
	// 归一化：EN-DC → 5G NSA
	if m.NetworkMode != "5G NSA" {
		t.Fatalf("network mode: %q", m.NetworkMode)
	}
	if !m.Registered {
		t.Fatal("want registered (band status=ok)")
	}
	// 信号：值 + 标尺归一的质量百分比
	if m.Signal == nil || m.Signal.RSRP == nil || *m.Signal.RSRP != -78 {
		t.Fatalf("rsrp: %+v", m.Signal)
	}
	if m.Signal.QualityPct == nil || *m.Signal.QualityPct != 64.6 {
		// (-78 - -140) / (-44 - -140) * 100 = 62/96 = 64.58 → 64.6
		t.Fatalf("quality pct: %v", m.Signal.QualityPct)
	}
	if m.Signal.Series == nil || len(m.Signal.Series.RSRP) != 1 || m.Signal.Series.Step != 10 {
		t.Fatalf("series: %+v", m.Signal.Series)
	}
	// 服务小区（band_name 优先，≤4 条）
	if len(m.ServingCells) != 2 || m.ServingCells[0].Band != "LTE B3" || m.ServingCells[1].Band != "NR n78" {
		t.Fatalf("serving cells: %+v", m.ServingCells)
	}
	// 拨号：字符串布尔 "true" + 速率解析（MB/s → Byte/s）
	if m.Dial == nil || !m.Dial.Running || m.Dial.IPv4 != "10.42.7.23" {
		t.Fatalf("dial: %+v", m.Dial)
	}
	if m.Dial.RxBps != 18200000 || m.Dial.TxBps != 2400000 {
		t.Fatalf("dial rates: %+v", m.Dial)
	}
	// 流量：available=true + 重置计划
	if m.Traffic == nil || !m.Traffic.Available || m.Traffic.RxBytes != 46180000000 {
		t.Fatalf("traffic: %+v", m.Traffic)
	}
	if m.Traffic.Reset == nil || m.Traffic.Reset.Type != "monthly" || m.Traffic.Reset.Day != 1 {
		t.Fatalf("traffic reset: %+v", m.Traffic.Reset)
	}
	// SIM：状态/槽位/不可切换（supportSwitch="0"）
	if m.SIM == nil || m.SIM.Status != "ready" || m.SIM.Slot != "1" || m.SIM.Switchable {
		t.Fatalf("sim: %+v", m.SIM)
	}
	// 设置回显（语义：USB 模式 / 网络偏好标志位归一 / 锁定频段条目）
	if m.Settings == nil || m.Settings.USBMode != "qmi" || m.Settings.Prefer != "4G+5G" {
		t.Fatalf("settings: %+v", m.Settings)
	}
	if len(m.Settings.Lockband) != 1 || m.Settings.Lockband[0] != "NR:41,78,79" {
		t.Fatalf("lockband: %+v", m.Settings.Lockband)
	}
	// 能力：stats=true / sim_switch=false（桩返回 0 不得当能力）
	if !m.Capabilities.Stats || m.Capabilities.SIMSwitch || !m.Capabilities.Temp {
		t.Fatalf("caps: %+v", m.Capabilities)
	}
	if !out.Capabilities.Stats || out.Capabilities.SIMSwitch {
		t.Fatalf("section caps: %+v", out.Capabilities)
	}
}

// generic 兜底实现：stats 恒 available:0、sim switch 恒 "0" —— 不得当成数据/能力。
func TestBuildCellularGenericStubs(t *testing.T) {
	f := fixtureFetch{
		"modem1.base_info":                   `{"modem_info":[{"key":"name","value":"Unknown Module"}]}`,
		"modem1.network_info":                `{"modem_info":[]}`,
		"modem1.cell_info":                   `{"modem_info":[]}`,
		"modem1.get_current_band":            `{"current_band":{"status":"unsupported","vendor":"generic","cells":[]}}`,
		"modem1.dial_status":                 `{"running":"false"}`,
		"modem1.get_stats":                   `{"available":false,"updated_at":0,"total_rx_bytes":0,"total_tx_bytes":0}`,
		"modem1.get_traffic_reset_schedule":  `{"enabled":false}`,
		"modem1.sim_info":                    `{"modem_info":[]}`,
		"modem1.get_sim_switch_capabilities": `{"supportSwitch":"0"}`,
		"modem1.get_sim_slot":                `{"slot":""}`,
		"modem1.get_mode":                    `{}`,
		"modem1.get_network_prefer":          `{}`,
		"modem1.get_lockband":                `{}`,
		"modem1.get_reboot_caps":             `{}`,
	}
	out, errs := buildCellular([]string{"modem1"}, f.fetch, newCellularSeriesStore(), time.Now())
	if len(out.Modems) != 1 {
		t.Fatalf("want 1 modem (base_info readable), got %d", len(out.Modems))
	}
	m := out.Modems[0]
	if m.Traffic == nil || m.Traffic.Available {
		t.Fatalf("stats stub must stay unavailable: %+v", m.Traffic)
	}
	if m.Capabilities.Stats {
		t.Fatal("stats capability must be false on stub")
	}
	if m.SIM != nil && m.SIM.Switchable {
		t.Fatal("sim_switch must be false on stub")
	}
	if m.Capabilities.CurrentBand {
		t.Fatal("current_band capability must be false when status=unsupported")
	}
	if len(errs) != 0 {
		t.Fatalf("missing methods are not errors here, got %v", errs)
	}
}

// 错误隔离：单个方法失败 → 子块省略 + 机器可读错误串；其他模组不受影响。
func TestBuildCellularErrorIsolation(t *testing.T) {
	f := quectelFixture("modem1")
	delete(f, "modem1.get_current_band") // 小区读取失败
	f2 := quectelFixture("modem2")
	merged := fixtureFetch{}
	for k, v := range f {
		merged[k] = v
	}
	for k, v := range f2 {
		merged[k] = v
	}
	out, errs := buildCellular([]string{"modem1", "modem2"}, merged.fetch, newCellularSeriesStore(), time.Now())
	if len(out.Modems) != 2 {
		t.Fatalf("both modems must survive: %d", len(out.Modems))
	}
	if out.Modems[0].ServingCells != nil {
		t.Fatalf("failed block must be omitted: %+v", out.Modems[0].ServingCells)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "modem1.get_current_band failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want modem1 read error, got %v", errs)
	}
	for _, e := range errs {
		if strings.Contains(e, "modem2") {
			t.Fatalf("modem2 must be clean: %v", errs)
		}
	}
}

// 模组已拔出（全部方法失败）→ 该模组不产出条目、不报错。
func TestBuildCellularDeadModem(t *testing.T) {
	out, errs := buildCellular([]string{"modem9"}, fixtureFetch{}.fetch, newCellularSeriesStore(), time.Now())
	if len(out.Modems) != 0 {
		t.Fatalf("dead modem must not appear: %+v", out.Modems)
	}
	if len(errs) != 0 {
		t.Fatalf("dead modem is not an error: %v", errs)
	}
}

// 序列：环形保持 ≤60 点且丢弃断档前的旧点。
func TestCellularSeriesRing(t *testing.T) {
	store := newCellularSeriesStore()
	for i := 0; i < snapshot.MaxCellularSeries+10; i++ {
		store.push("modem1", int64(-100+i))
	}
	got := store.series("modem1")
	if len(got) != snapshot.MaxCellularSeries {
		t.Fatalf("ring length: %d", len(got))
	}
	if got[len(got)-1] != int64(-100+snapshot.MaxCellularSeries+9) {
		t.Fatalf("ring tail: %d", got[len(got)-1])
	}
	store.drop("modem1")
	if store.series("modem1") != nil {
		t.Fatal("drop must clear the series")
	}
}

// 多模组：截断到上限（构造 5 个 section，应在 collectCellular 层被裁到 4）。
func TestCellularSectionNameValidation(t *testing.T) {
	for _, ok := range []string{"modem1", "modem_2", "M3"} {
		if !validModemSection(ok) {
			t.Fatalf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"", "modem-1", "modem 1", "a.b", "x;reboot", strings.Repeat("a", 33)} {
		if validModemSection(bad) {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

// 解析工具：带单位数值 / 速率 / 字符串布尔 / 频段列表。
func TestCellularParsers(t *testing.T) {
	if v, ok := numValue("42.5 °C"); !ok || v != 42.5 {
		t.Fatalf("numValue: %v %v", v, ok)
	}
	if v, ok := intValue("-78 dBm"); !ok || v != -78 {
		t.Fatalf("intValue: %v %v", v, ok)
	}
	if _, ok := numValue("n/a"); ok {
		t.Fatal("numValue must reject non-numeric")
	}
	if v, ok := rateBps("18.2 MB/s"); !ok || v != 18200000 {
		t.Fatalf("rateBps MB/s: %v %v", v, ok)
	}
	if v, ok := rateBps("150 Mbps"); !ok || v != 18750000 {
		t.Fatalf("rateBps Mbps: %v %v", v, ok)
	}
	if ok, has := parseSupportSwitch([]byte(`{"supportSwitch":"1"}`)); !has || !ok {
		t.Fatal("supportSwitch=1 must be true")
	}
	if ok, has := parseSupportSwitch([]byte(`{"supportSwitch":"0"}`)); !has || ok {
		t.Fatal("supportSwitch=0 must be false (and known)")
	}
	if _, has := parseSupportSwitch([]byte(`{}`)); has {
		t.Fatal("missing supportSwitch must be unknown")
	}
	// 频段锁定：Quectel 方言（band_class/lock_band）与其变体
	if got := parseLockband([]byte(`{"band_class":"NR","lock_band":"41,78,79"}`)); len(got) != 1 || got[0] != "NR:41,78,79" {
		t.Fatalf("lockband quectel: %v", got)
	}
	if got := parseLockband([]byte(`{"LTE":"1,3","NR":"unlock"}`)); len(got) != 1 || got[0] != "LTE:1,3" {
		t.Fatalf("lockband per-rat + unlock: %v", got)
	}
	if got := parseLockband([]byte(`{"bands":["3","78"]}`)); len(got) != 1 || got[0] != "3,78" {
		t.Fatalf("lockband list: %v", got)
	}
	// 网络偏好：标志位对象 → 紧凑串
	if got := parseNetworkPrefer([]byte(`{"network_prefer":{"3G":"0","4G":"1","5G":"1"}}`)); got != "4G+5G" {
		t.Fatalf("prefer flags: %q", got)
	}
	if got := parseNetworkPrefer([]byte(`{"prefer":"lte"}`)); got != "lte" {
		t.Fatalf("prefer plain: %q", got)
	}
	if got := parseSimSlot([]byte(`{"sim_slot":"2"}`)); got != "2" {
		t.Fatalf("sim slot: %q", got)
	}
}

// 序列形状：§3.9 的关键子结构必须能序列化为契约字段名。
func TestCellularMarshal(t *testing.T) {
	now := time.Unix(1760000000, 0)
	out, _ := buildCellular([]string{"modem1"}, quectelFixture("modem1").fetch, newCellularSeriesStore(), now)
	s := &snapshot.Snapshot{V: 1, Interval: 2, Errors: []string{}, Cellular: out}
	b, err := snapshot.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Cellular struct {
			Available bool `json:"available"`
			Modems    []struct {
				ID     string `json:"id"`
				Signal struct {
					RSRP    *int64   `json:"rsrp"`
					Quality *float64 `json:"quality_pct"`
					Series  struct {
						Step int     `json:"step"`
						RSRP []int64 `json:"rsrp"`
					} `json:"series"`
				} `json:"signal"`
				Dial struct {
					Running bool   `json:"running"`
					IPv4    string `json:"ipv4"`
					RxBps   int64  `json:"rx_bps"`
				} `json:"dial"`
				Capabilities struct {
					SIMSwitch bool `json:"sim_switch"`
				} `json:"capabilities"`
			} `json:"modems"`
		} `json:"cellular"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Cellular.Available || len(got.Cellular.Modems) != 1 {
		t.Fatalf("section shape: %+v", got.Cellular)
	}
	m := got.Cellular.Modems[0]
	if m.ID != "modem1" || m.Dial.IPv4 != "10.42.7.23" || m.Dial.RxBps != 18200000 {
		t.Fatalf("modem shape: %+v", m)
	}
	if m.Signal.Series.Step != 10 || len(m.Signal.Series.RSRP) != 1 {
		t.Fatalf("series shape: %+v", m.Signal.Series)
	}
}

// ---- 多厂商 fixture（key 词表跨厂商一致，但信号条目覆盖面不同）----

// fibocomFixture 只提供 RSSI/RSRQ（无 RSRP/SINR）——验证"部分信号"路径：
// signal 仍产出、quality_pct 与 series 因缺 RSRP 而省略，且**不**误报缺失。
func fibocomFixture(section string) fixtureFetch {
	f := fixtureFetch{}
	put := func(method, body string) { f[section+"."+method] = body }
	put("base_info", `{"modem_info":[
		{"key":"name","value":"FM150-AE"},
		{"key":"manufacturer","value":"Fibocom"},
		{"key":"revision","value":"FM150AE_01.00.06"},
		{"key":"temperature","value":"38 °C"}]}`)
	put("network_info", `{"modem_info":[
		{"key":"ISP","value":"China Mobile"},
		{"key":"IP Address","value":"10.9.1.7"},
		{"key":"Rx Rate","value":"12 MB/s"},
		{"key":"Tx Rate","value":"1.1 MB/s"}]}`)
	put("cell_info", `{"modem_info":[
		{"key":"RSRQ","value":"-13.0 dB","min_value":"-19.5","max_value":"-3","unit":"dB","type":"progress_bar"},
		{"key":"RSSI","value":"-67 dBm","min_value":"-120","max_value":"-20","unit":"dBm","type":"progress_bar"}]}`)
	put("get_current_band", `{"current_band":{"status":"ok","vendor":"fibocom","network_mode":"LTE","cells":[
		{"role":"pcc","rat":"LTE","band":"3","band_name":"LTE B3","channel":"1650","channel_type":"EARFCN","pci":"101","dl_bandwidth":"20","ul_bandwidth":"20"}]}}`)
	put("dial_status", `{"running":"true"}`)
	put("get_stats", `{"available":false,"updated_at":0,"total_rx_bytes":0,"total_tx_bytes":0}`)
	put("sim_info", `{"modem_info":[{"key":"SIM Status","value":"ready"}]}`)
	return f
}

// foxconnFixture 四条 bar 齐全 + 速率用 Mbps（比特单位）+ 模式写成 "LTE"。
func foxconnFixture(section string) fixtureFetch {
	f := fixtureFetch{}
	put := func(method, body string) { f[section+"."+method] = body }
	put("base_info", `{"modem_info":[{"key":"name","value":"T99W175"},{"key":"temperature","value":"51.0 °C"}]}`)
	put("network_info", `{"modem_info":[
		{"key":"ISP","value":"中国电信"},
		{"key":"Network Type","value":"LTE"},
		{"key":"Rx Rate","value":"150 Mbps"},
		{"key":"Tx Rate","value":"50 Mbps"}]}`)
	put("cell_info", `{"modem_info":[
		{"key":"RSRP","value":"-95 dBm","min_value":"-140","max_value":"-44","unit":"dBm","type":"progress_bar"},
		{"key":"RSRQ","value":"-15.0 dB","min_value":"-19.5","max_value":"-3","unit":"dB","type":"progress_bar"},
		{"key":"RSSI","value":"-72 dBm","min_value":"-120","max_value":"-20","unit":"dBm","type":"progress_bar"},
		{"key":"SINR","value":"9 dB","min_value":"0","max_value":"30","unit":"dB","type":"progress_bar"}]}`)
	put("dial_status", `{"running":"true"}`)
	return f
}

func TestBuildCellularFibocomPartialSignal(t *testing.T) {
	out, errs := buildCellular([]string{"modem1"}, fibocomFixture("modem1").fetch, newCellularSeriesStore(), time.Now())
	if len(errs) != 0 {
		t.Fatalf("partial signal must not raise errors: %v", errs)
	}
	m := out.Modems[0]
	if m.Model != "FM150-AE" || m.Vendor != "fibocom" {
		t.Fatalf("base: %+v", m)
	}
	if m.Signal == nil || m.Signal.RSSI == nil || *m.Signal.RSSI != -67 {
		t.Fatalf("rssi: %+v", m.Signal)
	}
	if m.Signal.RSRP != nil {
		t.Fatal("rsrp must be nil when vendor does not expose it")
	}
	if m.Signal.QualityPct != nil || m.Signal.Series != nil {
		t.Fatalf("quality/series must be omitted without RSRP: %+v", m.Signal)
	}
	if m.NetworkMode != "LTE" {
		t.Fatalf("mode: %q", m.NetworkMode)
	}
}

func TestBuildCellularFoxconnBitRates(t *testing.T) {
	out, _ := buildCellular([]string{"modem1"}, foxconnFixture("modem1").fetch, newCellularSeriesStore(), time.Now())
	m := out.Modems[0]
	if m.Dial == nil || m.Dial.RxBps != 18750000 || m.Dial.TxBps != 6250000 {
		// 150 Mbps = 150e6 bit/s = 18.75e6 Byte/s；50 Mbps = 6.25e6 Byte/s
		t.Fatalf("bit-rate conversion: %+v", m.Dial)
	}
	if m.Signal == nil || m.Signal.SINR == nil || *m.Signal.SINR != 9 {
		t.Fatalf("sinr: %+v", m.Signal)
	}
	if m.Signal.QualityPct == nil {
		t.Fatal("quality must be present with RSRP bar")
	}
}

// 形态容错：真布尔 / 数字 / 字符串三种上游形态都要能解析。
func TestCellularBoolShapes(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`true`, true}, {`false`, false},
		{`"true"`, true}, {`"false"`, false},
		{`1`, true}, {`0`, false},
		{`"1"`, true}, {`"0"`, false},
	}
	for _, c := range cases {
		got, ok := jsonBool(json.RawMessage(c.raw))
		if !ok || got != c.want {
			t.Fatalf("jsonBool(%s) = %v,%v want %v,true", c.raw, got, ok, c.want)
		}
	}
	if _, ok := jsonBool(json.RawMessage(`"maybe"`)); ok {
		t.Fatal("garbage must be unknown, not false")
	}
	// 布尔形态的 dial_status / supportSwitch 也要能用
	f := fixtureFetch{
		"modem1.base_info":                   `{"modem_info":[{"key":"name","value":"X"}]}`,
		"modem1.dial_status":                 `{"running":true}`,
		"modem1.get_sim_switch_capabilities": `{"supportSwitch":true}`,
	}
	out, _ := buildCellular([]string{"modem1"}, f.fetch, newCellularSeriesStore(), time.Now())
	m := out.Modems[0]
	if m.Dial == nil || !m.Dial.Running {
		t.Fatalf("bool-shaped running: %+v", m.Dial)
	}
	if !m.Capabilities.SIMSwitch {
		t.Fatal("bool-shaped supportSwitch must be true")
	}
}

// 关键字段缺失可见化：活跃模组（拨号中/已注册）信号块读不到 → 记一条可见提示。
func TestCellularActiveModemMissingCritical(t *testing.T) {
	f := fixtureFetch{
		"modem1.base_info":        `{"modem_info":[{"key":"name","value":"RM500U-CN"}]}`,
		"modem1.network_info":     `{"modem_info":[{"key":"ISP","value":"CMCC"}]}`,
		"modem1.cell_info":        `{"modem_info":[{"key":"SignalStrength","value":"great"}]}`, // key 漂移 → 认不出
		"modem1.dial_status":      `{"running":"true"}`,
		"modem1.get_current_band": `{"current_band":{"status":"ok","vendor":"x","network_mode":"LTE","cells":[{"role":"pcc","rat":"LTE","band":"3"}]}}`,
	}
	out, errs := buildCellular([]string{"modem1"}, f.fetch, newCellularSeriesStore(), time.Now())
	if len(out.Modems) != 1 {
		t.Fatalf("modem must survive: %d", len(out.Modems))
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "modem1.signal missing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("key drift must be visible: %v", errs)
	}
	// 非活跃模组（未注册/未拨号）无信号条目时不应刷这条提示
	f2 := fixtureFetch{
		"modem1.base_info":   `{"modem_info":[{"key":"name","value":"X"}]}`,
		"modem1.cell_info":   `{"modem_info":[]}`,
		"modem1.dial_status": `{"running":"false"}`,
	}
	_, errs2 := buildCellular([]string{"modem1"}, f2.fetch, newCellularSeriesStore(), time.Now())
	for _, e := range errs2 {
		if strings.Contains(e, "signal missing") {
			t.Fatalf("idle modem must not warn: %v", errs2)
		}
	}
}

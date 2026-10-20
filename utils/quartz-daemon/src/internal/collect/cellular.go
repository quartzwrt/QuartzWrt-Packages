package collect

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// cellular 节采集（契约 §3.9；W6.12 蜂窝轮，第二条慢车道，周期同 10s）。
//
// 数据面 = ubus 对象 `qmodem`（第三方插件 qmodem 的 rpcd 接口）——与网页端
// 同一数据源，字段语义逐项对齐。三条实现级事实（读上游源码核实，见
// QuartzWrt-App/docs/W6.12-CELLULAR-DESIGN.md §1）：
//
//   - `dial_status` 返回**字符串** "true"/"false"（不是 JSON 布尔）；
//   - generic 兜底实现恒回 `get_stats.available=0`、`supportSwitch="0"`
//     → 一律按 available/supportSwitch 门控，绝不把 0 值当数据展示；
//   - 读方法普遍带 10s `try_cache` → 本节 10s 采集正好命中缓存，不放大 AT 压力。
//
// 降级（契约 §3.9）：
//   - 无 modem-device section（未装 qmodem / 未配模组）→ {available:false}（tab 隐藏，无 errors）
//   - 全部模组均无可读数据（对象缺失/调用全失败）→ {available:false}
//   - 单模组子块失败 → 子块省略 + errors["cellular: <section>.<block>"]；不阻塞其他模组
//   - 敏感值（IMEI/IMSI/ICCID/短信内容）**不进快照**（页面级读按需取）

const (
	// cellularMaxModems 快照内模组上限（超出截断；与契约 §3.9 一致）。
	cellularMaxModems = snapshot.MaxCellularModems
	// cellularCallTimeout 单次 ubus 调用超时（真机慢模组余量）。
	cellularCallTimeout = 1 * time.Second
	// cellularCallGap 串行调用间隔：降低 AT 通道瞬时争用（真机实测可调）。
	cellularCallGap = 30 * time.Millisecond
	// cellularQualityFallbackMin/Max RSRP 质量条回退标尺（上游无 min/max 时）。
	cellularQualityFallbackMin = -140.0
	cellularQualityFallbackMax = -44.0
)

// cellularSeriesStore 每模组 RSRP 环形缓冲（仅慢 goroutine 访问 → 无锁）。
type cellularSeriesStore struct{ m map[string]*ring }

func newCellularSeriesStore() *cellularSeriesStore {
	return &cellularSeriesStore{m: map[string]*ring{}}
}

func (s *cellularSeriesStore) push(section string, v int64) []int64 {
	r := s.m[section]
	if r == nil {
		r = newRing(snapshot.MaxCellularSeries)
		s.m[section] = r
	}
	r.push(v)
	return r.slice()
}

// drop 清掉某模组的序列（读取失败时：避免曲线把断档后的点与旧点连起来）。
func (s *cellularSeriesStore) drop(section string) { delete(s.m, section) }

// cellularFetch 取数回调（真实实现走 ubus；单测注入 fixture）。
type cellularFetch func(method, section string) ([]byte, error)

// cellularUbusFetch 生产取数：`ubus call qmodem <method> {"config_section":"<section>"}`。
func cellularUbusFetch(method, section string) ([]byte, error) {
	args := fmt.Sprintf(`{"config_section":%q}`, section)
	return ubusCallTimeout(cellularCallTimeout, "qmodem", method, args)
}

// modemSections 取 qmodem 的 modem-device section 列表（`uci -q show qmodem`）。
// section 名按上游同款字符集校验（^[A-Za-z0-9_]+$）——防注入、防野 section。
func modemSections() []string {
	out, err := execTimeout(3*time.Second, "uci", "-q", "show", "qmodem")
	if err != nil {
		return nil
	}
	return parseModemSections(string(out))
}

// parseModemSections 从 `uci show qmodem` 输出提取**启用**的 modem-device section。
// qmodem 在 USB 重新枚举后会保留旧 path 的 section 并置 state=disabled（H5000M
// 重启实测 1_1/2_1 并存）；disabled 段进快照会让 App/Dashboard 出现两个模组
// （其中一个 dial.running=false）。无 state 字段的 section 保持旧行为（视为启用）。
func parseModemSections(raw string) []string {
	type secInfo struct {
		modem bool
		state string
	}
	infos := map[string]*secInfo{}
	for _, line := range strings.Split(raw, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) < 2 || parts[0] != "qmodem" || !validModemSection(parts[1]) {
			continue
		}
		info := infos[parts[1]]
		if info == nil {
			info = &secInfo{}
			infos[parts[1]] = info
		}
		switch {
		case len(parts) == 2 && unquote(val) == "modem-device":
			info.modem = true
		case len(parts) == 3 && parts[2] == "state":
			info.state = strings.ToLower(unquote(val))
		}
	}
	var secs []string
	for sec, info := range infos {
		if info.modem && info.state != "disabled" {
			secs = append(secs, sec)
		}
	}
	sort.Strings(secs)
	if len(secs) > cellularMaxModems {
		secs = secs[:cellularMaxModems]
	}
	return secs
}

// validModemSection section 名字符集校验（与上游 rpcd `case "$config_section"` 同款）。
func validModemSection(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// collectCellular 采集 cellular 节（慢刷调用；err 非 nil → 整节 null + errors）。
func collectCellular(now time.Time, series *cellularSeriesStore) (*snapshot.Cellular, error) {
	secs := modemSections()
	if len(secs) == 0 {
		// 未装 qmodem 或未配任何模组：与"未安装 dockerd"同款——条件隐藏，无 errors
		return &snapshot.Cellular{Available: false}, nil
	}
	out, errs := buildCellular(secs, cellularUbusFetch, series, now)
	out.Errors = errs
	if len(out.Modems) == 0 {
		// uci 有 section 但对象缺失/全部读取失败 → 视为不可用（不报错：
		// 网页端此时同样无可见数据，报错只会污染 errors 面板）
		return &snapshot.Cellular{Available: false}, nil
	}
	return out, nil
}

// buildCellular 由取数结果构造本节（纯函数，单测直插 fetch）。
// 返回的 errs 为该轮采集错误的机器可读短串（不排序，调用方决定去向）。
func buildCellular(secs []string, fetch cellularFetch, series *cellularSeriesStore, now time.Time) (*snapshot.Cellular, []string) {
	out := &snapshot.Cellular{Available: true, Updated: now.Unix(), Capabilities: &snapshot.CellularCaps{}}
	var errs []string
	for _, sec := range secs {
		m, mErrs := collectModem(sec, fetch, series, now)
		errs = append(errs, mErrs...)
		if m == nil {
			continue
		}
		out.Modems = append(out.Modems, *m)
	}
	for i := range out.Modems {
		c := out.Modems[i].Capabilities
		if c == nil {
			continue
		}
		out.Capabilities.AT = out.Capabilities.AT || c.AT
		out.Capabilities.SMS = out.Capabilities.SMS || c.SMS
		out.Capabilities.SIMSwitch = out.Capabilities.SIMSwitch || c.SIMSwitch
		out.Capabilities.Stats = out.Capabilities.Stats || c.Stats
		out.Capabilities.Temp = out.Capabilities.Temp || c.Temp
	}
	return out, errs
}

// collectModem 采集单个模组。返回 nil 表示该模组本轮无任何可读数据
// （不产生 error——例如 section 残留但模组已拔出）。
func collectModem(section string, fetch cellularFetch, series *cellularSeriesStore, now time.Time) (*snapshot.CellularModem, []string) {
	m := &snapshot.CellularModem{ID: section, Capabilities: &snapshot.CellularModemCaps{AT: true, SMS: true}}
	var pending []string // 子块读取失败（延迟外抛：模组整体无数据时不算错误）
	var readOK bool

	// 取数分两组（与消费方语义对齐）：
	//   callReq 必需方法——失败即记错误（核心信息缺失必须可见）
	//   callOpt 尽力方法——失败静默省略（设置回显/能力声明随厂商，缺了不影响主体）
	callReq := func(method string) []byte {
		time.Sleep(cellularCallGap)
		raw, err := fetch(method, section)
		if err != nil {
			pending = append(pending, fmt.Sprintf("cellular: %s.%s failed", section, method))
			return nil
		}
		return raw
	}
	callOpt := func(method string) []byte {
		time.Sleep(cellularCallGap)
		raw, err := fetch(method, section)
		if err != nil {
			return nil
		}
		return raw
	}

	// 基础信息（型号/厂商/固件/温度）
	if items := parseModemInfo(callReq("base_info")); len(items) > 0 {
		readOK = true
		m.Model = itemValue(items, "name", "model", "module")
		m.Vendor = strings.ToLower(itemValue(items, "manufacturer", "vendor"))
		m.Firmware = itemValue(items, "revision", "firmware", "version")
		if v, ok := numValue(itemValue(items, "temperature")); ok {
			m.TempC = &v
			m.Capabilities.Temp = true
		}
		m.ATPort = itemValue(items, "at_port", "at port")
		m.ConnectStatus = itemValue(items, "connect_status", "connect status")
		if v, ok := numValue(itemValue(items, "voltage")); ok {
			m.VoltageMV = int(v)
		}
	}
	// 网络信息（运营商 / 网络类型 / 速率 / 地址）
	if items := parseModemInfo(callReq("network_info")); len(items) > 0 {
		readOK = true
		m.Operator = itemValue(items, "isp", "operator", "network operator")
		m.NetworkType = itemValue(items, "network type", "network_type")
		if v, ok := intValue(itemValue(items, "cqi ul", "cqi_ul")); ok {
			m.CQIUL = int(v)
		}
		if v, ok := intValue(itemValue(items, "cqi dl", "cqi_dl")); ok {
			m.CQIDL = int(v)
		}
		if v, ok := intValue(itemValue(items, "ambr ul", "ambr_ul")); ok {
			m.AMBRUL = int(v)
		}
		if v, ok := intValue(itemValue(items, "ambr dl", "ambr_dl")); ok {
			m.AMBRDL = int(v)
		}
		if v := itemValue(items, "network type", "network_mode", "network mode"); v != "" {
			m.NetworkMode = normalizeNetworkMode(v)
		}
		d := &snapshot.CellularDial{}
		d.IPv4 = itemValue(items, "ip address", "ipv4", "ip addr", "ip")
		if v, ok := rateBps(itemValue(items, "rx rate", "download rate")); ok {
			d.RxBps = v
		}
		if v, ok := rateBps(itemValue(items, "tx rate", "upload rate")); ok {
			d.TxBps = v
		}
		if d.IPv4 != "" || d.RxBps > 0 || d.TxBps > 0 {
			m.Dial = d
		}
	}
	// 小区信息（信号指标 progress_bar：值 + min/max）
	if items := parseModemInfo(callReq("cell_info")); len(items) > 0 {
		readOK = true
		sig := &snapshot.CellularSignal{}
		var rMin, rMax float64
		var hasRange bool
		if it := findItem(items, "rsrp"); it != nil {
			if v, ok := intValue(it.Value); ok {
				sig.RSRP = &v
			}
			if mn, ok1 := numValue(it.Min); ok1 {
				if mx, ok2 := numValue(it.Max); ok2 && mx > mn {
					rMin, rMax, hasRange = mn, mx, true
				}
			}
		}
		if it := findItem(items, "rsrq"); it != nil {
			if v, ok := numValue(it.Value); ok {
				sig.RSRQ = &v
			}
		}
		if it := findItem(items, "sinr"); it != nil {
			if v, ok := numValue(it.Value); ok {
				sig.SINR = &v
			}
		}
		if it := findItem(items, "rssi"); it != nil {
			if v, ok := intValue(it.Value); ok {
				sig.RSSI = &v
			}
		}
		if sig.RSRP != nil {
			if hasRange {
				sig.QualityPct = ptrFloat(cellQualityPct(float64(*sig.RSRP), rMin, rMax))
			} else {
				sig.QualityPct = ptrFloat(cellQualityPct(float64(*sig.RSRP), cellularQualityFallbackMin, cellularQualityFallbackMax))
			}
			sig.Series = &snapshot.CellularSeries{Step: int(SlowInterval / time.Second), RSRP: series.push(section, *sig.RSRP)}
		}
		if sig.RSRP != nil || sig.RSRQ != nil || sig.SINR != nil || sig.RSSI != nil {
			m.Signal = sig
		}
		ci := &snapshot.CellularCellInfo{
			NetworkMode: itemValue(items, "network_mode", "network mode"),
			Duplex:      itemValue(items, "duplex mode", "duplex"),
			MCC:         itemValue(items, "mcc"),
			MNC:         itemValue(items, "mnc"),
			TAC:         itemValue(items, "tac"),
			CellID:      itemValue(items, "cell id", "cell_id"),
			PCI:         itemValue(items, "physical cell id", "pci"),
			ARFCN:       itemValue(items, "arfcn"),
			Band:        itemValue(items, "band"),
			ULBandwidth: itemValue(items, "ul bandwidth", "ul_bandwidth"),
			DLBandwidth: itemValue(items, "dl bandwidth", "dl_bandwidth"),
			SCS:         itemValue(items, "scs"),
		}
		if ci.NetworkMode != "" || ci.Duplex != "" || ci.MCC != "" || ci.CellID != "" {
			m.CellInfo = ci
		}
	}
	// 服务小区（get_current_band：结构化 cells）
	if raw := callReq("get_current_band"); raw != nil {
		if cells, mode, status, ok := parseCurrentBand(raw); ok {
			readOK = true
			m.Capabilities.CurrentBand = status != "unsupported"
			if len(cells) > 0 {
				m.ServingCells = cells
				m.Registered = status == "ok"
			}
			if mode != "" {
				m.NetworkMode = mode
			}
			// qmodem 首页 Cell Information 的 PCI/ARFCN/带宽等优先取 get_current_band；
			// cell_info 缺项时用服务小区补齐（两处同源，字段名不同）。
			if len(cells) > 0 {
				if m.CellInfo == nil {
					m.CellInfo = &snapshot.CellularCellInfo{}
				}
				c := cells[0]
				if m.CellInfo.PCI == "" {
					m.CellInfo.PCI = c.PCI
				}
				if m.CellInfo.ARFCN == "" {
					m.CellInfo.ARFCN = c.Channel
				}
				if m.CellInfo.Band == "" {
					m.CellInfo.Band = c.Band
				}
				if m.CellInfo.ULBandwidth == "" {
					m.CellInfo.ULBandwidth = c.ULBandwidth
				}
				if m.CellInfo.DLBandwidth == "" {
					m.CellInfo.DLBandwidth = c.DLBandwidth
				}
				if m.CellInfo.SCS == "" {
					m.CellInfo.SCS = c.SCS
				}
			}
		}
	}
	// 拨号状态（上游为**字符串** "true"/"false"；同时收真布尔，防上游改形）
	if raw := callReq("dial_status"); raw != nil {
		var v struct {
			Running json.RawMessage `json:"running"`
		}
		if json.Unmarshal(raw, &v) == nil {
			if running, ok := jsonBool(v.Running); ok {
				readOK = true
				if m.Dial == nil {
					m.Dial = &snapshot.CellularDial{}
				}
				m.Dial.Running = running
			}
		}
	}
	// 流量统计（非 Quectel 恒 available=false —— 按 available 门控，不展示 0）
	if raw := callReq("get_stats"); raw != nil {
		var v struct {
			Available json.RawMessage `json:"available"`
			UpdatedAt int64           `json:"updated_at"`
			RxBytes   int64           `json:"total_rx_bytes"`
			TxBytes   int64           `json:"total_tx_bytes"`
		}
		if json.Unmarshal(raw, &v) == nil {
			readOK = true
			avail, _ := jsonBool(v.Available) // 缺失/异形 → false（不展示 0 值）
			tr := &snapshot.CellularTraffic{Available: avail, UpdatedAt: v.UpdatedAt}
			if avail {
				tr.RxBytes, tr.TxBytes = v.RxBytes, v.TxBytes
				m.Capabilities.Stats = true
			}
			if m.Traffic == nil {
				m.Traffic = tr
			} else {
				m.Traffic.Available, m.Traffic.UpdatedAt, m.Traffic.RxBytes, m.Traffic.TxBytes =
					tr.Available, tr.UpdatedAt, tr.RxBytes, tr.TxBytes
			}
		}
	}
	// 流量重置计划
	if raw := callOpt("get_traffic_reset_schedule"); raw != nil {
		var v struct {
			Enabled   bool   `json:"enabled"`
			ResetType string `json:"reset_type"`
			Hour      int    `json:"hour"`
			Day       int    `json:"day"`
		}
		if json.Unmarshal(raw, &v) == nil {
			readOK = true
			if m.Traffic == nil {
				m.Traffic = &snapshot.CellularTraffic{}
			}
			m.Traffic.Reset = &snapshot.CellularReset{Enabled: v.Enabled, Type: v.ResetType, Hour: v.Hour, Day: v.Day}
		}
	}
	// SIM 摘要
	sim := &snapshot.CellularSIM{}
	if items := parseModemInfo(callReq("sim_info")); len(items) > 0 {
		readOK = true
		sim.Status = itemValue(items, "sim status", "status")
		sim.Slot = itemValue(items, "sim slot", "slot")
		sim.ISP = itemValue(items, "isp", "internet service provider")
		if m.Operator == "" {
			m.Operator = sim.ISP
		}
	}
	if raw := callOpt("get_sim_switch_capabilities"); raw != nil {
		if sw, ok := parseSupportSwitch(raw); ok {
			readOK = true
			sim.Switchable = sw
			m.Capabilities.SIMSwitch = sw
		}
	}
	if raw := callOpt("get_sim_slot"); raw != nil {
		if slot := parseSimSlot(raw); slot != "" {
			readOK = true
			sim.Slot = slot
		}
	}
	if sim.Status != "" || sim.Slot != "" || sim.ISP != "" {
		m.SIM = sim
	}
	// 设置回显（尽力：形状随厂商，失败即省略）
	if raw := callOpt("get_mode"); raw != nil {
		// 注意：这是 **USB 拨号模式**（qmi/ecm/mbim/rndis/ncm），不是网络模式
		if v := stringField(raw, "mode"); v != "" {
			m.Settings = ensureSettings(m.Settings)
			m.Settings.USBMode = v
		}
	}
	if raw := callOpt("get_network_prefer"); raw != nil {
		if v := parseNetworkPrefer(raw); v != "" {
			m.Settings = ensureSettings(m.Settings)
			m.Settings.Prefer = v
		}
	}
	if raw := callOpt("get_lockband"); raw != nil {
		if bands := parseLockband(raw); len(bands) > 0 {
			m.Settings = ensureSettings(m.Settings)
			m.Settings.Lockband = bands
		}
	}
	if raw := callOpt("get_reboot_caps"); raw != nil {
		if s := string(raw); strings.Contains(s, "hard") || strings.Contains(s, "soft") {
			readOK = true
		}
	}
	if !readOK {
		// 模组已拔出/section 残留：无数据即无条目、也无错误（与 bandix 空表同款取向）
		series.drop(section)
		return nil, nil
	}
	errs := pending
	// 关键字段缺失可见化：上游改 key/改形状时，静默省略会表现为「卡片少一块」——
	// 对**看起来活跃**的模组（已注册 / 拨号中 / 有服务小区）显式记一条可见提示。
	active := m.Registered || (m.Dial != nil && m.Dial.Running) || len(m.ServingCells) > 0
	if active {
		if m.Signal == nil {
			errs = append(errs, fmt.Sprintf("cellular: %s.signal missing (upstream key drift?)", section))
		}
		if m.Dial == nil {
			errs = append(errs, fmt.Sprintf("cellular: %s.dial missing (upstream key drift?)", section))
		}
	}
	if m.Registered == false && m.Signal != nil && m.Capabilities.CurrentBand == false {
		// 无 band 能力时以"拨号在跑"作为兜底注册判据（尽力而为，消费方以字段为准）
		if m.Dial != nil && m.Dial.Running {
			m.Registered = true
		}
	}
	return m, errs
}

// ---- 解析工具 ----

// modemInfoItem 上游 `modem_info` 条目（generic.sh 的 add_*_info_entry 生产者）。
type modemInfoItem struct {
	Class    string `json:"class"`
	Key      string `json:"key"`
	FullName string `json:"full_name"`
	Value    string `json:"value"`
	Type     string `json:"type"`
	Min      string `json:"min_value"`
	Max      string `json:"max_value"`
	Unit     string `json:"unit"`
}

// parseModemInfo 解析 `{modem_info:[…]}`（形状不符 → nil）。
func parseModemInfo(raw []byte) []modemInfoItem {
	if raw == nil {
		return nil
	}
	var v struct {
		ModemInfo []modemInfoItem `json:"modem_info"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v.ModemInfo
}

// findItem 按 key 别名（大小写不敏感）取首个条目。
func findItem(items []modemInfoItem, aliases ...string) *modemInfoItem {
	for i := range items {
		for _, a := range aliases {
			if strings.EqualFold(strings.TrimSpace(items[i].Key), a) {
				return &items[i]
			}
		}
	}
	return nil
}

// itemValue 取 key 别名对应的值（去首尾空白）。
func itemValue(items []modemInfoItem, aliases ...string) string {
	if it := findItem(items, aliases...); it != nil {
		return strings.TrimSpace(it.Value)
	}
	return ""
}

// numericPrefix 解析开头的数字段，返回（值, 数字段结束位置, 是否成功）。
func numericPrefix(s string) (float64, int, bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s[:end], "."), 64)
	if err != nil {
		return 0, 0, false
	}
	return v, end, true
}

// numValue 解析带单位的数值（"42.5 °C" / "-11.0 dB" / "-78 dBm" → 数值）。
func numValue(s string) (float64, bool) {
	v, _, ok := numericPrefix(s)
	return v, ok
}

// intValue 解析整数指标（"‑78 dBm" → -78；RSSI/RSRP 为整数域）。
func intValue(s string) (int64, bool) {
	v, ok := numValue(s)
	if !ok {
		return 0, false
	}
	return int64(v), true
}

// rateBps 解析速率字符串为 Byte/s（"18.2 MB/s" / "1.0 mbps" → bytes/s）。
func rateBps(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, end, ok := numericPrefix(s)
	if !ok {
		return 0, false
	}
	unit := strings.TrimSpace(strings.TrimSpace(s)[end:]) // 例："MB/s" / "Mbps" / "KB/s"
	// bit 与 byte 由大小写区分：MB/s=字节/秒，Mbps=比特/秒（单位里只有小写 b）
	bits := strings.Contains(unit, "b") && !strings.Contains(unit, "B")
	u := strings.ToLower(unit)
	mult := 1.0
	switch {
	case strings.Contains(u, "g"):
		mult = 1e9
	case strings.Contains(u, "m"):
		mult = 1e6
	case strings.Contains(u, "k"):
		mult = 1e3
	}
	if bits {
		mult *= 0.125 // bit/s → Byte/s
	}
	return int64(v * mult), true
}

// parseCurrentBand 解析 `get_current_band`（文档化结构；status/vendor/network_mode/cells）。
func parseCurrentBand(raw []byte) ([]snapshot.CellularCell, string, string, bool) {
	var v struct {
		CurrentBand struct {
			Status      string `json:"status"`
			NetworkMode string `json:"network_mode"`
			Cells       []struct {
				Role        string `json:"role"`
				RAT         string `json:"rat"`
				Band        string `json:"band"`
				BandName    string `json:"band_name"`
				Channel     string `json:"channel"`
				ChannelType string `json:"channel_type"`
				PCI         string `json:"pci"`
				ULBandwidth string `json:"ul_bandwidth"`
				DLBandwidth string `json:"dl_bandwidth"`
				SCS         string `json:"scs"`
			} `json:"cells"`
		} `json:"current_band"`
	}
	if json.Unmarshal(raw, &v) != nil || v.CurrentBand.Status == "" {
		return nil, "", "", false
	}
	cb := v.CurrentBand
	cells := make([]snapshot.CellularCell, 0, len(cb.Cells))
	for _, c := range cb.Cells {
		band := c.BandName
		if band == "" {
			band = c.Band
		}
		cells = append(cells, snapshot.CellularCell{
			Role: c.Role, RAT: c.RAT, Band: band, Channel: c.Channel,
			ChannelType: c.ChannelType, PCI: c.PCI,
			DLBandwidth: c.DLBandwidth, ULBandwidth: c.ULBandwidth, SCS: c.SCS,
		})
	}
	if len(cells) > snapshot.MaxServingCells {
		cells = cells[:snapshot.MaxServingCells]
	}
	return cells, normalizeNetworkMode(cb.NetworkMode), cb.Status, true
}

// parseSupportSwitch 解析 `get_sim_switch_capabilities`（上游为字符串 "0"/"1"，
// 同时收真布尔/数字，防上游改形）。
func parseSupportSwitch(raw []byte) (bool, bool) {
	var v struct {
		SupportSwitch json.RawMessage `json:"supportSwitch"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.SupportSwitch) == 0 {
		return false, false
	}
	return jsonBool(v.SupportSwitch)
}

// parseSimSlot 解析 `get_sim_slot`（形状随厂商：{"slot":"1"} / {"sim_slot":"1"} / 纯串）。
func parseSimSlot(raw []byte) string {
	if v := stringField(raw, "slot", "sim_slot", "current_slot"); v != "" {
		return v
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

// parseLockband 解析 `get_lockband`。上游形状随厂商/版本而异（读源码核实）：
//   - Quectel 方言：`{"band_class":"NR","lock_band":"41,78,79"}`（set 侧同形）
//   - 部分厂商：`{"LTE":"41,78","NR":"41"}` / `{"bands":"1,3,78"}` / `{"bands":[...]}`
//
// 归一为可读条目（`去重、≤4`）：例如 `["LTE:1,3","NR:41,78,79"]`；`unlock` 视为未锁定。
func parseLockband(raw []byte) []string {
	var v struct {
		BandClass string          `json:"band_class"`
		LockBand  string          `json:"lock_band"`
		Bands     json.RawMessage `json:"bands"`
		LTE       string          `json:"lte"`
		NR        string          `json:"nr"`
		NRBand    string          `json:"NR_Band"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	add := func(class, bands string) {
		bands = strings.TrimSpace(bands)
		if bands == "" || strings.EqualFold(bands, "unlock") || strings.EqualFold(bands, "0") {
			return
		}
		item := bands
		if class != "" {
			item = strings.ToUpper(class) + ":" + bands
		}
		for _, o := range out {
			if o == item {
				return
			}
		}
		if len(out) < 4 {
			out = append(out, item)
		}
	}
	if v.BandClass != "" || v.LockBand != "" {
		add(v.BandClass, v.LockBand)
	}
	if len(v.Bands) > 0 {
		var list []string
		if json.Unmarshal(v.Bands, &list) == nil {
			add("", strings.Join(list, ","))
		} else {
			var str string
			if json.Unmarshal(v.Bands, &str) == nil {
				add("", str)
			}
		}
	}
	add("LTE", v.LTE)
	add("NR", firstNonEmpty(v.NR, v.NRBand))
	return out
}

// parseNetworkPrefer 解析 `get_network_prefer`：上游返回标志位对象
// （`{"network_prefer":{"3G":"0","4G":"1","5G":"1"}}`，值为字符串 0/1），
// 归一为紧凑串（"4G+5G"）；同时兼容 `{"prefer":"lte"}` 形态。
func parseNetworkPrefer(raw []byte) string {
	var v struct {
		NetworkPrefer map[string]json.RawMessage `json:"network_prefer"`
		Prefer        string                     `json:"prefer"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	if v.Prefer != "" {
		return v.Prefer
	}
	order := []string{"2G", "3G", "4G", "5G"}
	var on []string
	for _, k := range order {
		raw, ok := v.NetworkPrefer[k]
		if !ok {
			continue
		}
		if b, ok := jsonBool(raw); ok && b {
			on = append(on, k)
		}
	}
	return strings.Join(on, "+")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitBands 拆分 "1,3,78" / "B3 n78" 形态并去空。
func splitBands(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '+' || r == '/'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// jsonBool 解析"字符串 / 数字 / 真布尔"三形态的布尔值（上游混用：
// dial_status 用 "true"/"false"，supportSwitch 用 "0"/"1"，jshn 输出为真布尔）。
// 第二个返回值表示该字段是否存在且可解析。
func jsonBool(raw json.RawMessage) (bool, bool) {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return false, false
	}
	switch t {
	case "true", "1", `"true"`, `"1"`, `"yes"`, `"on"`:
		return true, true
	case "false", "0", `"false"`, `"0"`, `"no"`, `"off"`:
		return false, true
	}
	return false, false
}

// stringField 取 JSON 对象里的首个非空字符串字段。
func stringField(raw []byte, keys ...string) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// normalizeNetworkMode 归一网络模式显示（上游大小写/形态不一）。
func normalizeNetworkMode(s string) string {
	v := strings.TrimSpace(s)
	switch strings.ToUpper(v) {
	case "", "UNKNOWN":
		return ""
	case "NR5G-SA", "NR SA", "5G SA":
		return "5G SA"
	case "EN-DC", "NR5G-NSA", "5G NSA", "LTE+NR":
		return "5G NSA"
	case "LTE", "4G":
		return "LTE"
	case "WCDMA", "3G":
		return "WCDMA"
	case "GSM", "2G":
		return "GSM"
	}
	return v
}

// cellQualityPct 信号质量百分比（用上游标的 min/max 归一，clamp 0–100）。
func cellQualityPct(v, min, max float64) float64 {
	if max <= min {
		return 0
	}
	p := (v - min) / (max - min) * 100
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return round1(p)
}

func ptrFloat(v float64) *float64 { return &v }

func ensureSettings(s *snapshot.CellularSettings) *snapshot.CellularSettings {
	if s == nil {
		return &snapshot.CellularSettings{}
	}
	return s
}

// cellularSeriesFor 供单测/诊断：查询某 section 的当前序列（无则 nil）。
func (s *cellularSeriesStore) series(section string) []int64 {
	if r := s.m[section]; r != nil {
		return r.slice()
	}
	return nil
}

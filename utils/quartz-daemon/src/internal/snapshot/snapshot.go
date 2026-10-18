// Package snapshot 定义聚合快照数据结构（AGGREGATE-API.md 冻结 v1 的镜像）
// 与上线自守（≤32KB / clients ≤32 / series ≤60 / unhealthy ≤8）。
//
// 契约纪律：字段/语义变更须走 AGGREGATE-API.md 变更流程（加法优先，
// 破坏性升 v）；本文件的 json tag 即契约字段名，不得私改。
package snapshot

import (
	"encoding/json"
	"sort"
)

const (
	// ContractVersion 契约版本（AGGREGATE-API.md §2 v 字段）。
	ContractVersion = 1
	// MaxBytes 整文件上限（契约 §1 硬约束）。
	MaxBytes = 32 * 1024
	// MaxClients clients.items 上限。
	MaxClients = 32
	// MaxSeries net.series 每数组上限。
	MaxSeries = 60
	// MaxUnhealthy docker.unhealthy 上限。
	MaxUnhealthy = 8
	// MaxDockerContainers docker.containers 上限。
	MaxDockerContainers = 8
	// MaxBandixTop bandix.top 上限（契约 §3.7）。
	MaxBandixTop = 8
	// MaxCellularModems cellular.modems 上限（契约 §3.9）。
	MaxCellularModems = 4
	// MaxCellularSeries cellular 每模组 signal.series 上限。
	MaxCellularSeries = 60
	// MaxServingCells cellular 每模组 serving_cells 上限。
	MaxServingCells = 4
)

// Snapshot 是快照顶层结构（契约 §2）。
type Snapshot struct {
	V         int            `json:"v"`
	Ts        int64          `json:"ts"`
	Interval  int            `json:"interval"`
	CollectMs *int64         `json:"collect_ms"`
	Host      Host           `json:"host"`
	System    System         `json:"system"`
	Net       Net            `json:"net"`
	Docker    *Docker        `json:"docker"`
	Clients   *Clients       `json:"clients"`
	Services  map[string]any `json:"services"`
	Bandix    *Bandix        `json:"bandix,omitempty"`
	Cellular  *Cellular      `json:"cellular,omitempty"`
	Errors    []string       `json:"errors"`
}

// Host 主机信息（契约 §3.1）。
type Host struct {
	Hostname string `json:"hostname"`
	Model    string `json:"model"`
	Release  string `json:"release"`
	Kernel   string `json:"kernel"`
	Uptime   int64  `json:"uptime"`
}

// Bytes 容量三元组（契约 §3.2；total=0 表示未启用，如 swap）。
type Bytes struct {
	Total int64   `json:"total"`
	Used  int64   `json:"used"`
	Pct   float64 `json:"pct"`
}

// System 系统资源（契约 §3.2）。
type System struct {
	Load   [3]float64 `json:"load"`
	CPUPct float64    `json:"cpu_pct"`
	Mem    Bytes      `json:"mem"`
	Swap   Bytes      `json:"swap"`
	Rootfs Bytes      `json:"rootfs"`
	TempC  *float64   `json:"temp_c"`
}

// WAN 广域网接口状态（契约 §3.3）。
type WAN struct {
	Up     bool   `json:"up"`
	Ifname string `json:"ifname"`
	Proto  string `json:"proto"`
	IPv4   string `json:"ipv4"`
	// IPv6 首个公网 GUA（跳过 fe80/ULA/temporary/deprecated；2026-10-19 加法）。
	IPv6  string `json:"ipv6,omitempty"`
	RxBps int64  `json:"rx_bps"`
	TxBps int64  `json:"tx_bps"`
}

// Series 吞吐环形缓冲（契约 §3.3；step 秒/点，各数组 ≤60 点）。
type Series struct {
	Step int     `json:"step"`
	Rx   []int64 `json:"rx"`
	Tx   []int64 `json:"tx"`
}

// Net 网络节（契约 §3.3）。
type Net struct {
	WAN    WAN    `json:"wan"`
	Series Series `json:"series"`
}

// Docker Docker 节（契约 §3.4；整节 null = 不可用；Available=false = 未安装）。
type Docker struct {
	Available bool     `json:"available"`
	Running   int      `json:"running"`
	Total     int      `json:"total"`
	Images    int      `json:"images"`
	Unhealthy []string `json:"unhealthy"`
	// 2026-10-19 加法（Dashboard Docker 卡信息密度；全部可选）：
	Version    string            `json:"version,omitempty"`    // Docker ServerVersion
	Driver     string            `json:"driver,omitempty"`     // 存储驱动（overlayfs/vfs…）
	RootDir    string            `json:"root_dir,omitempty"`   // Docker Root Dir
	CPUs       int               `json:"cpus,omitempty"`       // 节点 CPU 数
	MemTotal   int64             `json:"mem_total,omitempty"`  // 节点内存（字节）
	Networks   int               `json:"networks,omitempty"`   // 网络数
	Volumes    int               `json:"volumes,omitempty"`    // 卷数
	Containers []DockerContainer `json:"containers,omitempty"` // 容器摘要（≤8）
}

// DockerContainer 容器摘要（Dashboard 列表；不含敏感 env/labels）。
type DockerContainer struct {
	Name   string `json:"name"`
	Image  string `json:"image,omitempty"`
	State  string `json:"state,omitempty"`
	Status string `json:"status,omitempty"`
}

// Client 在线设备条目（契约 §3.5）。
type Client struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Host     string `json:"host"`
	Iface    string `json:"iface"`
	LastSeen int64  `json:"last_seen"`
}

// Clients 在线设备节（契约 §3.5；items ≤32、按 last_seen 降序）。
type Clients struct {
	Total int      `json:"total"`
	Items []Client `json:"items"`
}

// SortClients 按 last_seen 降序、并列按 MAC 升序稳定排序（契约 §3.5）。
func SortClients(items []Client) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastSeen != items[j].LastSeen {
			return items[i].LastSeen > items[j].LastSeen
		}
		return items[i].MAC < items[j].MAC
	})
}

// Bandix 流量监控节（契约 §3.7；WP-8 bridge 链路慢刷 10s，经 ubus 契约
// `quartz.bandix` 取数）。available=false = 未装/未开 traffic 模块 → 卡片隐藏；
// 整节缺失（null）= 上游暂不可用（errors 记一条）。
type Bandix struct {
	Available bool          `json:"available"`
	Devices   int           `json:"devices,omitempty"`
	Online    int           `json:"online,omitempty"`
	WAN       *BandixWAN    `json:"wan,omitempty"`
	Limits    *BandixLimits `json:"limits,omitempty"`
	Top       []BandixTop   `json:"top,omitempty"`
	Conn      *BandixConn   `json:"conn,omitempty"`
	DNS       *BandixDNS    `json:"dns,omitempty"`
}

// BandixWAN 设备侧 WAN 速率合计（Byte/s；= Σ w_rx_r / w_tx_r，与网页同式）。
//
// 注：内置卡片与 App 不再展示（与"网络"卡的接口级吞吐重复，2026-10-17 用户指令）；
// 字段保留供消费方自定义展示/诊断。
type BandixWAN struct {
	RxBps int64 `json:"rx_bps"`
	TxBps int64 `json:"tx_bps"`
}

// BandixLimits 总体限速策略摘要（契约 §3.7；来源 `quartz.bandix limits` 聚合）。
type BandixLimits struct {
	DefaultRxBps     int64 `json:"default_rx_bps"`
	DefaultTxBps     int64 `json:"default_tx_bps"`
	WhitelistEnabled bool  `json:"whitelist_enabled"`
	WhitelistCount   int   `json:"whitelist_count,omitempty"`
	ScheduleCount    int   `json:"schedule_count,omitempty"`
	QuotaCount       int   `json:"quota_count,omitempty"`
}

// BandixTop 流量 Top 设备（按累计 t_rx_b+t_tx_b 降序，pct = 占全部设备累计流量比）。
// rx_r/tx_r = 设备侧 WAN 速率（Byte/s，= w_rx_r/w_tx_r，与网页设备表 WAN 列同源）；
// wl/sch/q = 该设备的限速策略标记（白名单 / 时段限速规则条数 / 有无配额）。
type BandixTop struct {
	MAC  string  `json:"mac"`
	Host string  `json:"host"`
	IP   string  `json:"ip"`
	RxB  int64   `json:"rx_b"`
	TxB  int64   `json:"tx_b"`
	Pct  float64 `json:"pct"`
	RxR  int64   `json:"rx_r"`
	TxR  int64   `json:"tx_r"`
	WL   bool    `json:"wl,omitempty"`
	Sch  int     `json:"sch,omitempty"`
	Q    bool    `json:"q,omitempty"`
}

// BandixConn 连接摘要（契约 §3.7；connections 模块未启用/不可用时省略）。
type BandixConn struct {
	Total int `json:"total"`
	TCP   int `json:"tcp"`
	UDP   int `json:"udp"`
}

// BandixDNS DNS 摘要（契约 §3.7；dns 模块未启用/不可用时省略）。
type BandixDNS struct {
	Queries int64 `json:"queries"`
}

// Cellular 蜂窝节（契约 §3.9；慢刷 10s，经 ubus 对象 `qmodem` 读方法。
// 第三方插件 qmodem 的 rpcd 接口——与网页端同一数据源）。
//
// available=false = 未装 qmodem / 无 modem-device section / 全部模组读取失败
// → 蜂窝 tab 与三处卡片一起隐藏（与 bandix 条件卡同款语义）。
type Cellular struct {
	Available    bool            `json:"available"`
	Updated      int64           `json:"updated,omitempty"`
	Capabilities *CellularCaps   `json:"capabilities,omitempty"`
	Modems       []CellularModem `json:"modems,omitempty"`
	Errors       []string        `json:"errors,omitempty"`
}

// CellularCaps 端级能力（任一模组具备即为真；用于 tab 内的功能入口显隐）。
type CellularCaps struct {
	AT        bool `json:"at"`
	SMS       bool `json:"sms"`
	SIMSwitch bool `json:"sim_switch"`
	Stats     bool `json:"stats"`
	Temp      bool `json:"temp"`
}

// CellularModem 单个模组（uci section = qmodem.<id>，type=modem-device）。
type CellularModem struct {
	ID          string   `json:"id"`
	Model       string   `json:"model,omitempty"`
	Vendor      string   `json:"vendor,omitempty"`
	Firmware    string   `json:"firmware,omitempty"`
	TempC       *float64 `json:"temp_c,omitempty"`
	Registered  bool     `json:"registered"`
	Operator    string   `json:"operator,omitempty"`
	NetworkMode string   `json:"network_mode,omitempty"`
	// 2026-10-19 加法（对齐 qmodem 首页 Base/Network Information；全部可选）：
	ATPort        string             `json:"at_port,omitempty"`
	VoltageMV     int                `json:"voltage_mv,omitempty"`
	ConnectStatus string             `json:"connect_status,omitempty"`
	NetworkType   string             `json:"network_type,omitempty"`
	CQIUL         int                `json:"cqi_ul,omitempty"`
	CQIDL         int                `json:"cqi_dl,omitempty"`
	AMBRUL        int                `json:"ambr_ul,omitempty"`
	AMBRDL        int                `json:"ambr_dl,omitempty"`
	CellInfo      *CellularCellInfo  `json:"cell_info,omitempty"`
	Signal        *CellularSignal    `json:"signal,omitempty"`
	ServingCells  []CellularCell     `json:"serving_cells,omitempty"`
	Dial          *CellularDial      `json:"dial,omitempty"`
	Traffic       *CellularTraffic   `json:"traffic,omitempty"`
	SIM           *CellularSIM       `json:"sim,omitempty"`
	Settings      *CellularSettings  `json:"settings,omitempty"`
	Capabilities  *CellularModemCaps `json:"capabilities,omitempty"`
}

// CellularCellInfo qmodem 首页 Cell Information 段（单服务小区；MCC/MNC/TAC/
// Cell ID 等结构字段来自 cell_info；PCI/ARFCN/带宽/SCS 优先取 get_current_band）。
type CellularCellInfo struct {
	NetworkMode string `json:"network_mode,omitempty"`
	Duplex      string `json:"duplex,omitempty"`
	MCC         string `json:"mcc,omitempty"`
	MNC         string `json:"mnc,omitempty"`
	TAC         string `json:"tac,omitempty"`
	CellID      string `json:"cell_id,omitempty"`
	PCI         string `json:"pci,omitempty"`
	ARFCN       string `json:"arfcn,omitempty"`
	Band        string `json:"band,omitempty"`
	ULBandwidth string `json:"ul_bandwidth,omitempty"`
	DLBandwidth string `json:"dl_bandwidth,omitempty"`
	SCS         string `json:"scs,omitempty"`
}

// CellularSignal 信号指标（RSRP/RSRQ/SINR/RSSI 取上游 progress_bar 条目的值，
// 单位与标尺与网页端一致；quality_pct 用同一条目的 min/max 归一）。
type CellularSignal struct {
	RSRP       *int64          `json:"rsrp,omitempty"`
	RSRQ       *float64        `json:"rsrq,omitempty"`
	SINR       *float64        `json:"sinr,omitempty"`
	RSSI       *int64          `json:"rssi,omitempty"`
	QualityPct *float64        `json:"quality_pct,omitempty"`
	Series     *CellularSeries `json:"series,omitempty"`
}

// CellularSeries RSRP 环形缓冲（daemon 侧累积；step 秒/点，≤60 点）。
type CellularSeries struct {
	Step int     `json:"step"`
	RSRP []int64 `json:"rsrp"`
}

// CellularCell 服务小区（源自 get_current_band.cells，归一化字段名）。
type CellularCell struct {
	Role        string `json:"role,omitempty"`
	RAT         string `json:"rat,omitempty"`
	Band        string `json:"band,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	PCI         string `json:"pci,omitempty"`
	DLBandwidth string `json:"dl_bandwidth,omitempty"`
	ULBandwidth string `json:"ul_bandwidth,omitempty"`
	SCS         string `json:"scs,omitempty"`
}

// CellularDial 拨号状态（running 由上游字符串布尔 "true"/"false" 归一）。
type CellularDial struct {
	Running bool   `json:"running"`
	IPv4    string `json:"ipv4,omitempty"`
	RxBps   int64  `json:"rx_bps,omitempty"`
	TxBps   int64  `json:"tx_bps,omitempty"`
}

// CellularTraffic 流量统计（available=false = 本模组不支持，如非 Quectel）。
type CellularTraffic struct {
	Available bool           `json:"available"`
	UpdatedAt int64          `json:"updated_at,omitempty"`
	RxBytes   int64          `json:"rx_bytes,omitempty"`
	TxBytes   int64          `json:"tx_bytes,omitempty"`
	Reset     *CellularReset `json:"reset,omitempty"`
}

// CellularReset 流量重置计划。
type CellularReset struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type,omitempty"`
	Hour    int    `json:"hour,omitempty"`
	Day     int    `json:"day,omitempty"`
}

// CellularSIM SIM 摘要（存储容量与未读短信属页面级读，不进快照）。
type CellularSIM struct {
	Status     string `json:"status,omitempty"`
	Slot       string `json:"slot,omitempty"`
	ISP        string `json:"isp,omitempty"` // 运营商 PLMN（qmodem 首页 SIM Information）
	Switchable bool   `json:"switchable"`
}

// CellularSettings 当前设置回显（尽力；上游形状随厂商，取不到则省略）。
//
// 语义（读上游源码核实，2026-10-18）：USBMode = `get_mode` 的 **USB 拨号模式**
// （qmi/ecm/mbim/rndis/ncm，AT+QCFG="usbnet"）——**不是** 5G/4G 网络模式；网络偏好
// 在 `prefer`（`get_network_prefer` 的 3G/4G/5G 标志位归一，如 "4G+5G"）。
type CellularSettings struct {
	USBMode  string   `json:"usb_mode,omitempty"`
	Prefer   string   `json:"prefer,omitempty"`
	Lockband []string `json:"lockband,omitempty"`
}

// CellularModemCaps 单模组能力（灰显依据；不支持项由消费方标注原因）。
type CellularModemCaps struct {
	AT          bool `json:"at"`
	SMS         bool `json:"sms"`
	SIMSwitch   bool `json:"sim_switch"`
	Stats       bool `json:"stats"`
	CurrentBand bool `json:"current_band"`
	Temp        bool `json:"temp"`
}

// trimCellular 收缩 cellular 节（超限阶梯用；seriesMax ≤0 表示只裁模组数）。
func trimCellular(c *Cellular, seriesMax, modemsMax int) {
	if c == nil {
		return
	}
	if modemsMax > 0 && len(c.Modems) > modemsMax {
		c.Modems = c.Modems[:modemsMax]
	}
	for i := range c.Modems {
		m := &c.Modems[i]
		if m.Signal == nil || m.Signal.Series == nil {
			continue
		}
		if seriesMax > 0 && len(m.Signal.Series.RSRP) > seriesMax {
			m.Signal.Series.RSRP = m.Signal.Series.RSRP[len(m.Signal.Series.RSRP)-seriesMax:]
		}
	}
}

// Marshal 序列化并执行上限自守（契约 §6.3）。
//
// 阶梯：正常 → 超限时先收 clients→16、series→30 → 仍超则丢弃可选节
// （clients / series），保证必备节（host/system/net）完整送达。
func Marshal(s *Snapshot) ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(b) <= MaxBytes {
		return b, nil
	}
	if s.Clients != nil && len(s.Clients.Items) > 16 {
		s.Clients.Items = s.Clients.Items[:16]
	}
	if len(s.Net.Series.Rx) > 30 {
		s.Net.Series.Rx = s.Net.Series.Rx[len(s.Net.Series.Rx)-30:]
	}
	if len(s.Net.Series.Tx) > 30 {
		s.Net.Series.Tx = s.Net.Series.Tx[len(s.Net.Series.Tx)-30:]
	}
	trimCellular(s.Cellular, 30, 2)
	b, err = json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(b) <= MaxBytes {
		return b, nil
	}
	// 防御性兜底：丢可选节（罕见路径；正常数据量级在 ~3KB）
	s.Clients = nil
	s.Cellular = nil
	s.Net.Series = Series{Step: s.Interval}
	b, err = json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return b, nil
}

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
	RxBps  int64  `json:"rx_bps"`
	TxBps  int64  `json:"tx_bps"`
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
	b, err = json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(b) <= MaxBytes {
		return b, nil
	}
	// 防御性兜底：丢可选节（罕见路径；正常数据量级在 ~3KB）
	s.Clients = nil
	s.Net.Series = Series{Step: s.Interval}
	b, err = json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return b, nil
}

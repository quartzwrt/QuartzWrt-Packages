// Package snapshot 读取 quartz-daemon 的聚合快照（AGGREGATE-API v1 的子集）。
package snapshot

import (
	"encoding/json"
	"os"
)

// Snapshot 是 quartz-screen 消费的快照字段（与 AGGREGATE-API.md §3 对齐）。
type Snapshot struct {
	V    int   `json:"v"`
	Ts   int64 `json:"ts"`
	Host struct {
		Hostname string `json:"hostname"`
		Model    string `json:"model"`
		Release  string `json:"release"`
		Kernel   string `json:"kernel"`
		Uptime   int64  `json:"uptime"`
	} `json:"host"`
	System struct {
		Load [3]float64 `json:"load"`
		CPU  float64    `json:"cpu_pct"`
		Mem  struct {
			Total int64   `json:"total"`
			Used  int64   `json:"used"`
			Pct   float64 `json:"pct"`
		} `json:"mem"`
		Rootfs struct {
			Total int64   `json:"total"`
			Used  int64   `json:"used"`
			Pct   float64 `json:"pct"`
		} `json:"rootfs"`
		TempC    *float64 `json:"temp_c"`
		CPUModel string   `json:"cpu_model,omitempty"`
		Cores    int      `json:"cores,omitempty"`
		CPUMHz   int      `json:"cpu_mhz,omitempty"`
		Procs    int      `json:"procs,omitempty"`
	} `json:"system"`
	Net struct {
		WAN struct {
			Up     bool   `json:"up"`
			Ifname string `json:"ifname"`
			Proto  string `json:"proto"`
			IPv4   string `json:"ipv4"`
			IPv6   string `json:"ipv6"`
			RxBps  int64  `json:"rx_bps"`
			TxBps  int64  `json:"tx_bps"`
		} `json:"wan"`
		Series struct {
			Step int     `json:"step"`
			Rx   []int64 `json:"rx"`
			Tx   []int64 `json:"tx"`
		} `json:"series"`
	} `json:"net"`
	Clients  *Clients  `json:"clients"`
	Cellular *Cellular `json:"cellular"`
	Bandix   *Bandix   `json:"bandix,omitempty"`
}

// Bandix 是聚合快照里 screen 需要的流量 Top 子集（契约 §3.7）。
type Bandix struct {
	Available bool        `json:"available"`
	Top       []BandixTop `json:"top,omitempty"`
}

// BandixTop 单个设备累计流量排名（pct = 占全部设备累计流量比；
// RxR/TxR = 设备侧 WAN 速率，Byte/s）。
type BandixTop struct {
	Host string  `json:"host,omitempty"`
	IP   string  `json:"ip,omitempty"`
	MAC  string  `json:"mac,omitempty"`
	Pct  float64 `json:"pct"`
	RxR  int64   `json:"rx_r,omitempty"`
	TxR  int64   `json:"tx_r,omitempty"`
}

// Client 在线设备条目（契约 §3.5）。
type Client struct {
	MAC   string `json:"mac"`
	IP    string `json:"ip"`
	Host  string `json:"host"`
	Iface string `json:"iface"`
}

// Clients 在线设备节。
type Clients struct {
	Total int      `json:"total"`
	Items []Client `json:"items"`
}

// Cellular 蜂窝节（契约 §3.9）。
type Cellular struct {
	Available bool     `json:"available"`
	Updated   int64    `json:"updated,omitempty"`
	Modems    []Modem  `json:"modems,omitempty"`
	Errors    []string `json:"errors,omitempty"`
}

// Modem 单个模组。
type Modem struct {
	ID            string    `json:"id"`
	Model         string    `json:"model,omitempty"`
	Vendor        string    `json:"vendor,omitempty"`
	TempC         *float64  `json:"temp_c,omitempty"`
	Registered    bool      `json:"registered"`
	Operator      string    `json:"operator,omitempty"`
	NetworkMode   string    `json:"network_mode,omitempty"`
	NetworkType   string    `json:"network_type,omitempty"`
	ConnectStatus string    `json:"connect_status,omitempty"`
	CellInfo      *CellInfo `json:"cell_info,omitempty"`
	Signal        *Signal   `json:"signal,omitempty"`
	Dial          *Dial     `json:"dial,omitempty"`
	Traffic       *Traffic  `json:"traffic,omitempty"`
	SIM           *SIM      `json:"sim,omitempty"`
}

// CellInfo 服务小区摘要。
type CellInfo struct {
	Band        string `json:"band,omitempty"`
	DLBandwidth string `json:"dl_bandwidth,omitempty"`
	NetworkMode string `json:"network_mode,omitempty"`
	MCC         string `json:"mcc,omitempty"`
	MNC         string `json:"mnc,omitempty"`
	PCI         string `json:"pci,omitempty"`
	ARFCN       string `json:"arfcn,omitempty"`
}

// Signal 信号指标。
type Signal struct {
	RSRP       *int64        `json:"rsrp,omitempty"`
	RSRQ       *float64      `json:"rsrq,omitempty"`
	SINR       *float64      `json:"sinr,omitempty"`
	QualityPct *float64      `json:"quality_pct,omitempty"`
	Series     *SignalSeries `json:"series,omitempty"`
}

// SignalSeries RSRP 环形缓冲。
type SignalSeries struct {
	Step int     `json:"step"`
	RSRP []int64 `json:"rsrp"`
}

// Dial 拨号状态。
type Dial struct {
	Running bool   `json:"running"`
	IPv4    string `json:"ipv4,omitempty"`
	RxBps   int64  `json:"rx_bps,omitempty"`
	TxBps   int64  `json:"tx_bps,omitempty"`
}

// Traffic 流量统计。
type Traffic struct {
	Available bool  `json:"available"`
	RxBytes   int64 `json:"rx_bytes,omitempty"`
	TxBytes   int64 `json:"tx_bytes,omitempty"`
}

// SIM SIM 摘要。
type SIM struct {
	Status string `json:"status,omitempty"`
	ISP    string `json:"isp,omitempty"`
}

// Wireless 是从 /etc/config/wireless 取到的静态信息（非快照）。
type Wireless struct {
	Present    bool
	SSID       string
	Band       string
	Channel    string
	HTMode     string
	Radio      string
	Iface      string
	Encryption string
	Mode       string
	Network    string
	Hidden     bool
	WMM        bool
}

// Load 读取快照文件；返回 mtime（Unix 秒）便于增量刷新。
func Load(path string) (*Snapshot, int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, st.ModTime().Unix(), err
	}
	return &s, st.ModTime().Unix(), nil
}

// HeroModem 返回第一个可用模组。
func (c *Cellular) HeroModem() *Modem {
	if c == nil || !c.Available || len(c.Modems) == 0 {
		return nil
	}
	for i := range c.Modems {
		if c.Modems[i].Registered || c.Modems[i].ConnectStatus != "" {
			return &c.Modems[i]
		}
	}
	return &c.Modems[0]
}

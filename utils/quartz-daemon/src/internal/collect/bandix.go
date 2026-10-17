package collect

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// bandix 节采集（契约 §3.7；WP-8 bridge 链路）。
//
// 数据面经 Bridge 契约 `quartz.bandix`（bridge DESIGN §6.2：唯一集成面，
// daemon 不直连核心 8686 HTTP API）。慢刷 10s。语义与网页逐字段同源：
//   - online = last（最后在线时刻）在 bandix.traffic.offline_timeout 内
//     —— 与上游状态页 isDeviceOnline() 同一条判据（含秒级时间戳兜底）
//   - wan    = Σ w_rx_r / w_tx_r（设备侧 WAN 速率合计，与上游 "WAN Traffic" 卡同式）
//   - top    = 按累计 t_rx_b+t_tx_b 降序；pct = 该设备累计 / 全部设备累计 ×100
//
// 降级（契约 §3.7 + §6.2）：
//   - 未装 bridge 包 / 未开 traffic 模块 → {available:false}（卡片隐藏，无 errors）
//   - traffic 已开但上游不可用 → 整节 null + errors["bandix: …"]（下轮重试）
//   - connections / dns 模块未启用 → conn/dns 省略（null 语义）

const (
	// bandixBridgeUC bridge 包契约文件（存在性判定"已装"，避免多一次 ubus 探测）。
	bandixBridgeUC = "/usr/share/rpcd/ucode/quartz-bandix-bridge.uc"
	// bandixDefaultOfflineTimeout 离线超时默认值（秒；上游前端无配置时的同款兜底）。
	bandixDefaultOfflineTimeout = 600
)

// bandixUCI 关心的开关与参数（一次 `uci -q show bandix` 取全）。
type bandixUCI struct {
	installed       bool // 有 bandix.general.* 或 traffic 开关 → 配置存在
	traffic         bool
	connections     bool
	dns             bool
	offlineTimeoutS int
}

// parseBandixUCI 解析 `uci -q show bandix` 的规范化输出。
func parseBandixUCI(out string) bandixUCI {
	st := bandixUCI{offlineTimeoutS: bandixDefaultOfflineTimeout}
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" {
			continue
		}
		val = unquote(val)
		switch key {
		case "bandix.general.iface", "bandix.general.data_dir", "bandix.general.port":
			st.installed = true
		case "bandix.traffic.enabled":
			st.installed = true
			st.traffic = val == "1"
		case "bandix.connections.enabled":
			st.connections = val == "1"
		case "bandix.dns.enabled":
			st.dns = val == "1"
		case "bandix.traffic.offline_timeout":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				st.offlineTimeoutS = n
			}
		}
	}
	return st
}

// bandixEnvelope Bridge 契约信封（{ok,api,data|error}）。
type bandixEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// collectBandix 采集 bandix 节（慢刷调用；失败返回 error → 整节 null + errors）。
func collectBandix(now time.Time) (*snapshot.Bandix, error) {
	if _, err := os.Stat(bandixBridgeUC); err != nil {
		return &snapshot.Bandix{Available: false}, nil
	}
	uciOut, _ := execTimeout(3*time.Second, "uci", "-q", "show", "bandix")
	uci := parseBandixUCI(string(uciOut))
	if !uci.traffic {
		// 未装（uci 无配置）或 traffic 关闭 → 与 "未安装 dockerd" 同款：卡片隐藏
		return &snapshot.Bandix{Available: false}, nil
	}

	statusRaw, err := ubusCall("quartz.bandix", "status")
	if err != nil {
		return nil, fmt.Errorf("bandix: bridge unreachable")
	}
	// connections / dns 为可选摘要：模块未启用时不调用；调用失败不阻塞本节
	var connRaw, dnsRaw []byte
	if uci.connections {
		connRaw, _ = ubusCall("quartz.bandix", "connections")
	}
	if uci.dns {
		dnsRaw, _ = ubusCall("quartz.bandix", "dns_stats")
	}
	return buildBandix(statusRaw, connRaw, dnsRaw, uci, now)
}

// buildBandix 由上游应答构造本节（纯函数，单测直插）。
func buildBandix(statusRaw, connRaw, dnsRaw []byte, uci bandixUCI, now time.Time) (*snapshot.Bandix, error) {
	var env bandixEnvelope
	if err := json.Unmarshal(statusRaw, &env); err != nil {
		return nil, fmt.Errorf("bandix: status parse error")
	}
	if !env.OK {
		msg := "upstream error"
		if env.Error != nil && env.Error.Message != "" {
			msg = env.Error.Message
		}
		return nil, fmt.Errorf("bandix: %s", msg)
	}
	var st struct {
		D []struct {
			MAC  string `json:"mac"`
			IP4  string `json:"ip4"`
			Host string `json:"host"`
			Last int64  `json:"last"`
			TRxB int64  `json:"t_rx_b"`
			TTxB int64  `json:"t_tx_b"`
			WRxR int64  `json:"w_rx_r"`
			WTxR int64  `json:"w_tx_r"`
		} `json:"d"`
	}
	if err := json.Unmarshal(env.Data, &st); err != nil {
		return nil, fmt.Errorf("bandix: status data parse error")
	}

	out := &snapshot.Bandix{
		Available: true,
		Devices:   len(st.D),
		WAN:       &snapshot.BandixWAN{},
		Top:       []snapshot.BandixTop{},
	}
	thresholdMs := int64(uci.offlineTimeoutS) * 1000
	nowMs := now.UnixMilli()
	var totalB int64
	for _, d := range st.D {
		lastMs := d.Last
		if lastMs > 0 && lastMs < 1_000_000_000_000 {
			lastMs *= 1000 // 秒级时间戳兜底（上游前端同款兼容）
		}
		if lastMs > 0 && nowMs-lastMs <= thresholdMs {
			out.Online++
		}
		out.WAN.RxBps += d.WRxR
		out.WAN.TxBps += d.WTxR
		out.Top = append(out.Top, snapshot.BandixTop{
			MAC: d.MAC, Host: d.Host, IP: d.IP4,
			RxB: d.TRxB, TxB: d.TTxB,
		})
		totalB += d.TRxB + d.TTxB
	}

	sort.SliceStable(out.Top, func(i, j int) bool {
		return out.Top[i].RxB+out.Top[i].TxB > out.Top[j].RxB+out.Top[j].TxB
	})
	if len(out.Top) > snapshot.MaxBandixTop {
		out.Top = out.Top[:snapshot.MaxBandixTop]
	}
	if totalB > 0 {
		for i := range out.Top {
			out.Top[i].Pct = round1(float64(out.Top[i].RxB+out.Top[i].TxB) / float64(totalB) * 100)
		}
	}

	if connRaw != nil {
		if c := parseBandixConn(connRaw); c != nil {
			out.Conn = c
		}
	}
	if dnsRaw != nil {
		if v, ok := parseBandixDNS(dnsRaw); ok {
			out.DNS = &snapshot.BandixDNS{Queries: v}
		}
	}
	return out, nil
}

// parseBandixConn 解析 `connections` 应答（信封 → data.g 摘要）；不可用时 nil。
func parseBandixConn(raw []byte) *snapshot.BandixConn {
	var env bandixEnvelope
	if json.Unmarshal(raw, &env) != nil || !env.OK {
		return nil
	}
	var d struct {
		G struct {
			Total int `json:"total"`
			TCP   int `json:"tcp"`
			UDP   int `json:"udp"`
		} `json:"g"`
	}
	if json.Unmarshal(env.Data, &d) != nil {
		return nil
	}
	return &snapshot.BandixConn{Total: d.G.Total, TCP: d.G.TCP, UDP: d.G.UDP}
}

// parseBandixDNS 解析 `dns_stats` 应答（信封 → data.total_queries）。
func parseBandixDNS(raw []byte) (int64, bool) {
	var env bandixEnvelope
	if json.Unmarshal(raw, &env) != nil || !env.OK {
		return 0, false
	}
	var d struct {
		TotalQueries int64 `json:"total_queries"`
	}
	if json.Unmarshal(env.Data, &d) != nil {
		return 0, false
	}
	return d.TotalQueries, true
}

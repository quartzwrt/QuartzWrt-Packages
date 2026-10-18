package collect

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// netCounters 接口计数器采样点（用于 delta 计算）。
type netCounters struct {
	ifname string
	rx, tx uint64
	ts     time.Time
}

// netFast 组装 net 节（契约 §3.3）；proto 来自慢刷缓存。
func (c *Collector) netFast(now time.Time, proto string) snapshot.Net {
	var n snapshot.Net
	n.WAN.Proto = proto

	ifname := parseDefaultRoute(readFile("/proc/net/route"))
	n.WAN.Ifname = ifname
	if ifname != "" {
		oper := readTrim("/sys/class/net/" + ifname + "/operstate")
		// pppoe/wireguard 等隧道接口 operstate 常为 "unknown"：仅 "down" 视为离线
		n.WAN.Up = oper != "down"
		n.WAN.IPv4 = ifaceIPv4(ifname)
		n.WAN.IPv6 = ifaceIPv6GUA(ifname)

		rx, tx := readCounters(ifname)
		var rxb, txb int64
		if c.hasNetPrev && c.netPrev.ifname == ifname {
			dt := now.Sub(c.netPrev.ts).Seconds()
			if dt > 0 {
				rxb = deltaBps(c.netPrev.rx, rx, dt)
				txb = deltaBps(c.netPrev.tx, tx, dt)
			}
		}
		c.netPrev = netCounters{ifname: ifname, rx: rx, tx: tx, ts: now}
		c.hasNetPrev = true
		n.WAN.RxBps, n.WAN.TxBps = rxb, txb
	} else {
		c.hasNetPrev = false // 无默认路由：不累计 delta（恢复后按新基点重算）
	}

	c.ringRx.push(n.WAN.RxBps)
	c.ringTx.push(n.WAN.TxBps)
	n.Series = snapshot.Series{Step: c.cfg.Interval, Rx: c.ringRx.slice(), Tx: c.ringTx.slice()}
	return n
}

// parseDefaultRoute 解析 /proc/net/route 的默认路由承载接口
// （Destination=00000000 且 Flag up；多条取 Metric 最小）。
func parseDefaultRoute(b []byte) string {
	best := ""
	bestMetric := -1
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue // 表头
		}
		f := strings.Fields(line)
		if len(f) < 8 || f[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&0x1 == 0 {
			continue
		}
		metric, _ := strconv.ParseUint(f[6], 16, 32)
		if best == "" || int(metric) < bestMetric {
			best, bestMetric = f[0], int(metric)
		}
	}
	return best
}

// readCounters 读接口累计字节数（/sys/class/net/<if>/statistics）。
func readCounters(ifname string) (uint64, uint64) {
	rx, _ := strconv.ParseUint(readTrim("/sys/class/net/"+ifname+"/statistics/rx_bytes"), 10, 64)
	tx, _ := strconv.ParseUint(readTrim("/sys/class/net/"+ifname+"/statistics/tx_bytes"), 10, 64)
	return rx, tx
}

// deltaBps 计算速率（字节/秒）；计数器回绕/接口重建（cur < prev）→ 0。
func deltaBps(prev, cur uint64, dt float64) int64 {
	if cur < prev || dt <= 0 {
		return 0
	}
	return int64(float64(cur-prev) / dt)
}

// ifaceIPv6GUA 取接口首个公网 GUA（跳过 fe80/link-local、ULA、temporary/deprecated）。
// 优先 `ip -6 -o addr … scope global`（能识别 temporary/deprecated 标志），
// 失败时回退 Go 原生地址枚举（无法识别 temporary，仅保证非 ULA/非链路本地）。
func ifaceIPv6GUA(name string) string {
	if out, err := execTimeout(2*time.Second, "ip", "-6", "-o", "addr", "show", "dev", name, "scope", "global"); err == nil {
		if v := parseIPv6GUA(string(out)); v != "" {
			return v
		}
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.To4() != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	return ""
}

// parseIPv6GUA 从 `ip -6 -o addr` 输出挑选首个公网 GUA；跳过 temporary/deprecated 行。
func parseIPv6GUA(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "temporary") || strings.Contains(line, "deprecated") {
			continue
		}
		f := strings.Fields(line)
		for i, tok := range f {
			if tok != "inet6" || i+1 >= len(f) {
				continue
			}
			addr := strings.SplitN(f[i+1], "/", 2)[0]
			ip := net.ParseIP(addr)
			if ip == nil || ip.To4() != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

// ifaceIPv4 取接口首个 IPv4 地址（Go 原生，不 exec）。
func ifaceIPv4(name string) string {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

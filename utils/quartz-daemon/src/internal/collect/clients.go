package collect

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

// jsonRaw 与 jsonUnmarshal 是 encoding/json 的薄封装（集中一处便于替换）。
type jsonRaw = json.RawMessage

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

type arpEntry struct {
	IP, MAC, Device string
}

// parseARP 解析 /proc/net/arp（仅保留 Flags=0x2 完整条目）。
func parseARP(b []byte) []arpEntry {
	var out []arpEntry
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue // 表头
		}
		f := strings.Fields(line)
		if len(f) < 6 || f[2] != "0x2" {
			continue
		}
		out = append(out, arpEntry{IP: f[0], MAC: strings.ToLower(f[3]), Device: f[5]})
	}
	return out
}

type leaseInfo struct{ IP, Host string }

// parseLeases 解析 dnsmasq 租约（/tmp/dhcp.leases）：
// `<expiry> <mac> <ip> <hostname> <clientid>`；hostname 为 "*" 记空串。
func parseLeases(b []byte) map[string]leaseInfo {
	out := map[string]leaseInfo{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		host := f[3]
		if host == "*" {
			host = ""
		}
		out[strings.ToLower(f[1])] = leaseInfo{IP: f[2], Host: host}
	}
	return out
}

type wifiClient struct {
	Iface      string
	InactiveMs int64
	HasInact   bool
}

// collectClients 组合 ARP ∪ 无线关联（在线判定；DESIGN §3.5），
// 租约补主机名/IP；last_seen = 最后确认在线时刻（无线用 inactive 修正）。
func collectClients(wanIface string) (*snapshot.Clients, error) {
	arpRaw := readFile("/proc/net/arp")
	if len(arpRaw) == 0 {
		return nil, fmt.Errorf("clients: /proc/net/arp unavailable")
	}
	wifi := collectWireless()
	leases := parseLeases(readFile("/tmp/dhcp.leases"))
	return mergeClients(parseARP(arpRaw), leases, wifi, wanIface, time.Now().Unix()), nil
}

// mergeClients 合并三源（独立函数便于单测）：
// 在线 = ARP（LAN 侧）∪ 无线关联；租约补主机名/IP；items ≤32 按 last_seen 降序。
func mergeClients(arp []arpEntry, leases map[string]leaseInfo, wifi map[string]wifiClient, wanIface string, now int64) *snapshot.Clients {
	m := map[string]*snapshot.Client{}

	for _, e := range arp {
		if !isLANSide(e.Device, wanIface) {
			continue
		}
		m[e.MAC] = &snapshot.Client{MAC: e.MAC, IP: e.IP, Iface: e.Device, LastSeen: now}
	}
	for mac, w := range wifi {
		c := m[mac]
		if c == nil {
			c = &snapshot.Client{MAC: mac, Iface: w.Iface, LastSeen: now}
			m[mac] = c
		}
		if w.HasInact {
			if ls := now - w.InactiveMs/1000; ls < c.LastSeen {
				c.LastSeen = ls
			}
		}
	}
	for mac, c := range m {
		if l, ok := leases[mac]; ok {
			if c.IP == "" {
				c.IP = l.IP
			}
			c.Host = l.Host
		}
	}

	items := make([]snapshot.Client, 0, len(m))
	for _, c := range m {
		items = append(items, *c)
	}
	total := len(items)
	snapshot.SortClients(items)
	if len(items) > snapshot.MaxClients {
		items = items[:snapshot.MaxClients]
	}
	return &snapshot.Clients{Total: total, Items: items}
}

// isLANSide 判定 ARP 条目属于 LAN 侧设备（排除 WAN 承载口与虚拟接口）。
func isLANSide(dev, wanIface string) bool {
	if dev == "" || dev == "lo" {
		return false
	}
	if wanIface != "" && dev == wanIface {
		return false
	}
	for _, p := range []string{"docker", "veth", "virbr", "tun", "wg", "tailscale"} {
		if strings.HasPrefix(dev, p) {
			return false
		}
	}
	return true
}

// collectWireless 无线关联（可选增强）：无无线接口或无 iw/iwinfo 时返回空。
// 优先 `iw dev <if> station dump`（含 inactive 毫秒），否则 `iwinfo <if> assoclist`。
func collectWireless() map[string]wifiClient {
	out := map[string]wifiClient{}
	ifaces := wirelessIfaces()
	if len(ifaces) == 0 {
		return out
	}
	if _, err := exec.LookPath("iw"); err == nil {
		for _, ifc := range ifaces {
			b, err := execTimeout(2*time.Second, "iw", "dev", ifc, "station", "dump")
			if err != nil {
				continue
			}
			for mac, inact := range parseIWStationDump(string(b)) {
				out[mac] = wifiClient{Iface: ifc, InactiveMs: inact, HasInact: true}
			}
		}
		return out
	}
	if _, err := exec.LookPath("iwinfo"); err == nil {
		for _, ifc := range ifaces {
			b, err := execTimeout(2*time.Second, "iwinfo", ifc, "assoclist")
			if err != nil {
				continue
			}
			for _, mac := range parseIwinfoAssoclist(string(b)) {
				out[mac] = wifiClient{Iface: ifc}
			}
		}
	}
	return out
}

func wirelessIfaces() []string {
	dirs, _ := filepath.Glob("/sys/class/net/*/wireless")
	var out []string
	for _, d := range dirs {
		out = append(out, filepath.Base(filepath.Dir(d)))
	}
	sort.Strings(out)
	return out
}

var macRe = regexp.MustCompile(`^([0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5})`)

// parseIWStationDump 解析 `iw dev <if> station dump`：MAC → inactive 毫秒。
func parseIWStationDump(s string) map[string]int64 {
	out := map[string]int64{}
	cur := ""
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "Station ") {
			f := strings.Fields(t)
			if len(f) >= 2 {
				cur = strings.ToLower(f[1])
				out[cur] = 0
			}
			continue
		}
		if cur != "" && strings.HasPrefix(t, "inactive time:") {
			f := strings.Fields(t)
			if len(f) >= 3 {
				if ms, err := strconv.ParseInt(f[2], 10, 64); err == nil {
					out[cur] = ms
				}
			}
		}
	}
	return out
}

// parseIwinfoAssoclist 解析 `iwinfo <if> assoclist`：逐行取首个 MAC。
func parseIwinfoAssoclist(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if m := macRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, strings.ToLower(m[1]))
		}
	}
	return out
}

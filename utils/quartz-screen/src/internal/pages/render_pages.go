package pages

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/render"
	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

// R2 版式常量（320x172，顶栏 22，内容从 28 起）。
const (
	cardW    = 308
	tallY    = 28
	tallH    = 92
	shortY   = 28
	shortH   = 58
	infoY    = 89
	infoH    = 40
	tallY1   = 136
	tallY2   = 150
	shortY1  = 143
	shortY2  = 157
	contentX = 8
	contentR = 312
)

func renderOverview(c *render.Canvas, ctx *Context) {
	th, s, lang := ctx.Theme, ctx.Snapshot, ctx.Lang
	cpu, mem := 0.0, 0.0
	cpuV, memV, tempV, tempU := "—", "—", "—", ""
	cpuS, memS, tempS := "—", "—", "—"
	ok := s != nil && !ctx.Stale
	if s != nil {
		cpu, mem = s.System.CPU, s.System.Mem.Pct
		if s.System.CPUMHz > 0 {
			cpuS = fmtMHz(s.System.CPUMHz)
		}
		if s.System.Mem.Total > 0 {
			memS = sizePair(s.System.Mem.Used, s.System.Mem.Total)
		}
		tempV, tempU = tempParts(s.System.TempC, ctx.Config.TempUnit)
		if s.System.Load[0] > 0 {
			tempS = tr(lang, "负载 ", "load ") + fmt.Sprintf("%.2f", s.System.Load[0])
		}
	}
	if ok {
		cpuV, memV = fmt.Sprintf("%.0f", cpu), fmt.Sprintf("%.0f", mem)
	} else {
		cpuS, memS, tempS = "—", "—", "—"
	}
	tallMetric(c, th, 6, tr(lang, "处理器", "CPU"), cpuV, pctUnit(ok), cpuS, metricColor(th, cpu), barOf(ok, cpu), metricColor(th, cpu))
	tallMetric(c, th, 110, tr(lang, "内存", "RAM"), memV, pctUnit(ok), memS, th.Text, barOf(ok, mem), th.Accent)
	tallMetric(c, th, 214, tr(lang, "温度", "TEMP"), tempV, tempU, tempS, th.Text, -1, 0)

	var up, clients int64
	var proto, ipv4 string
	var rx, tx int64
	if s != nil {
		up = s.Host.Uptime
		ipv4 = s.Net.WAN.IPv4
		rx, tx = s.Net.WAN.RxBps, s.Net.WAN.TxBps
		proto = strings.ToUpper(s.Net.WAN.Proto)
		if s.Clients != nil {
			clients = int64(s.Clients.Total)
		}
	}
	var parts []string
	if ok {
		parts = append(parts, tr(lang, "已运行 ", "up ")+fmtUptime(lang, up))
		if ctx.Config.ShowClients {
			parts = append(parts, fmt.Sprintf("%s %d", tr(lang, "在线", "online"), clients))
		}
		if s.Net.WAN.Up {
			parts = append(parts, "WAN "+dashIfEmpty(proto))
		} else {
			parts = append(parts, "WAN "+tr(lang, "未连接", "down"))
		}
	} else {
		parts = append(parts, tr(lang, "数据源离线", "offline"))
	}
	line1 := strings.Join(parts, " · ")
	if ok {
		drawStrip(c, th, tallY1, tallY2, line1, dashIfEmpty(ipv4), rx, tx, true)
	} else {
		drawStrip(c, th, tallY1, tallY2, line1, "—", 0, 0, false)
	}
}

func renderNetwork(c *render.Canvas, ctx *Context) {
	th, s, lang := ctx.Theme, ctx.Snapshot, ctx.Lang
	var rx, tx int64
	var ipv4, ipv6, proto, ifname string
	var clients int
	var seriesRx, seriesTx []int64
	if s != nil {
		rx, tx = s.Net.WAN.RxBps, s.Net.WAN.TxBps
		ipv4, ipv6, proto, ifname = s.Net.WAN.IPv4, s.Net.WAN.IPv6, s.Net.WAN.Proto, s.Net.WAN.Ifname
		seriesRx, seriesTx = s.Net.Series.Rx, s.Net.Series.Tx
		if s.Clients != nil {
			clients = s.Clients.Total
		}
	}
	rv, ru := "—", ""
	uv, uu := "—", ""
	clientsV := "—"
	if s != nil {
		rv, ru = rateParts(rx)
		uv, uu = rateParts(tx)
		if s.Clients != nil {
			clientsV = fmt.Sprintf("%d", clients)
		}
	}
	shortMetric(c, th, 6, tr(lang, "下行", "DOWN"), rv, ru, th.Accent)
	shortMetric(c, th, 110, tr(lang, "上行", "UP"), uv, uu, th.OK)
	shortMetric(c, th, 214, tr(lang, "客户端", "Clients"), clientsV, "", th.Text)

	hasTop := s != nil && s.Bandix != nil && len(s.Bandix.Top) > 0
	if hasTop {
		drawTopList(c, th, s.Bandix.Top)
	} else {
		drawWanCard(c, th, lang, ifname, proto, ipv4, ipv6)
	}

	line1 := ""
	if ctx.Config.ShowIPv6 && ipv6 != "" {
		line1 = "IPv6 " + shortIPv6(ipv6)
	} else if proto != "" {
		line1 = "WAN " + strings.ToUpper(proto)
	}
	if ctx.Config.ShowPeaks && len(seriesRx) > 0 {
		line1 = joinParts(line1, tr(lang, "峰值 ", "peak ")+fmtRate(peak(seriesRx))+" / "+fmtRate(peak(seriesTx)))
	}
	if s == nil {
		line1 = tr(lang, "数据源离线", "offline")
	}
	drawStrip(c, th, shortY1, shortY2, line1, dashIfEmpty(ipv4), rx, tx, s != nil)
}

func drawTopList(c *render.Canvas, th *render.Theme, top []snapshot.BandixTop) {
	card(c, th, 6, infoY, cardW, infoH)
	if len(top) > 3 {
		top = top[:3]
	}
	for i, r := range top {
		y := 101 + i*11
		name := r.Host
		if name == "" {
			name = r.IP
		}
		if name == "" {
			name = r.MAC
		}
		c.Text(face("ui9"), 13, y, fitText(face("ui9"), dashIfEmpty(name), 64), th.Text)
		p := math.Max(0, math.Min(100, r.Pct))
		c.FillRect(82, y-4, 133, 3, th.Well)
		if p > 0 {
			c.FillRect(82, y-4, int(133*p/100), 3, th.Accent)
		}
		c.TextRight(face("ui9"), 246, y, fmt.Sprintf("%.0f%%", p), th.Text2)
		c.TextRight(face("ui9"), 307, y, fitText(face("ui9"), fmtRate(r.RxR+r.TxR), 56), th.Text2)
	}
}

func drawWanCard(c *render.Canvas, th *render.Theme, lang, ifname, proto, ipv4, ipv6 string) {
	card(c, th, 6, infoY, cardW, infoH)
	line1 := "WAN " + dashIfEmpty(ifname)
	if proto != "" {
		line1 += " · " + strings.ToUpper(proto)
	} else {
		line1 += " · " + tr(lang, "未配置", "unconfigured")
	}
	line2 := "IPv4 " + dashIfEmpty(ipv4)
	if ipv6 != "" {
		line2 = "IPv6 " + shortIPv6(ipv6)
	}
	c.Text(face("ui9"), 13, 101, fitText(face("ui9"), line1, 292), th.Text2)
	c.Text(face("ui9"), 13, 113, fitText(face("ui9"), line2, 292), th.Text2)
}

func renderCellular(c *render.Canvas, ctx *Context) {
	th, s, lang := ctx.Theme, ctx.Snapshot, ctx.Lang
	var m *snapshot.Modem
	if s != nil {
		m = s.Cellular.HeroModem()
	}
	if m == nil {
		return
	}
	rsrp, sinr, quality := "—", "—", "—"
	rsrpU, sinrU, qualityU := "", "", ""
	rsrpC, sinrC, qualityC := th.Text, th.Text, th.Text
	if m.Signal != nil {
		if m.Signal.RSRP != nil {
			rsrp, rsrpU = fmt.Sprintf("%d", *m.Signal.RSRP), "dBm"
		}
		if m.Signal.SINR != nil {
			sinr = fmt.Sprintf("%.0f", *m.Signal.SINR)
		}
		if m.Signal.QualityPct != nil {
			q := *m.Signal.QualityPct
			quality, qualityU = fmt.Sprintf("%.0f", q), "%"
			qualityC = qualityColor(th, q)
		}
	}
	shortMetric(c, th, 6, "RSRP", rsrp, rsrpU, rsrpC)
	shortMetric(c, th, 110, "SINR", sinr, sinrU, sinrC)
	shortMetric(c, th, 214, tr(lang, "质量", "Quality"), quality, qualityU, qualityC)

	ci := m.CellInfo
	band, bw, pci, arfcn := "—", "—", "—", "—"
	if ci != nil {
		band, bw, pci, arfcn = fmtBand(m), fmtBandwidth(ci.DLBandwidth), dashIfEmpty(ci.PCI), dashIfEmpty(ci.ARFCN)
	}
	card(c, th, 6, infoY, cardW, infoH)
	drawKV(c, th, 13, 101, tr(lang, "频段", "Band"), band, 92)
	drawKV(c, th, 110, 101, tr(lang, "带宽", "BW"), bw, 92)
	drawKV(c, th, 207, 101, "PCI", pci, 92)
	drawKV(c, th, 13, 115, tr(lang, "温度", "TEMP"), fmtTemp(lang, m.TempC, ctx.Config.TempUnit), 92)
	drawKV(c, th, 110, 115, "ARFCN", arfcn, 92)
	sim := "—"
	if m.SIM != nil {
		sim = simLabel(lang, m.SIM.Status)
	}
	drawKV(c, th, 207, 115, "SIM", sim, 92)

	opName, icon := pickOperator(lang, m)
	if icon == "" && m.CellInfo != nil && m.CellInfo.MCC != "" && m.CellInfo.MNC != "" {
		if n, ic := operatorInfo(lang, m.CellInfo.MCC+m.CellInfo.MNC); ic != "" {
			opName, icon = n, ic
		}
	}
	if ov := ctx.Config.Operator; ov != "" && ov != "auto" {
		opName, icon = operatorOverride(lang, ov)
	}
	x := contentX
	if icon != "" && c.DrawIcon(icon, x, 131) {
		x += 22
	}
	nameW := c.Text(face("uib13"), x, 143, fitText(face("uib13"), dashIfEmpty(opName), 110), th.Text)
	mode := m.NetworkMode
	if mode == "" {
		mode = m.NetworkType
	}
	if mode != "" {
		c.Text(face("ui11"), nameW+6, 143, fitText(face("ui11"), mode, 312-nameW-6), th.Text2)
	}
	ip := "—"
	if m.Dial != nil && m.Dial.IPv4 != "" {
		ip = m.Dial.IPv4
	} else if s != nil && s.Net.WAN.IPv4 != "" {
		ip = s.Net.WAN.IPv4
	}
	c.Text(face("mono11"), contentX, shortY2, fitText(face("mono11"), ip, 140), th.Text)
	traffic := ""
	if m.Traffic != nil && m.Traffic.Available {
		traffic = "↓ " + fmtBytes(m.Traffic.RxBytes) + " / ↑ " + fmtBytes(m.Traffic.TxBytes)
	} else if m.ConnectStatus != "" {
		traffic = m.ConnectStatus
	}
	if traffic != "" {
		c.TextRight(face("ui9"), contentR, shortY2, fitText(face("ui9"), traffic, 150), th.Text2)
	}
}

func renderWireless(c *render.Canvas, ctx *Context) {
	th, s, lang, w := ctx.Theme, ctx.Snapshot, ctx.Lang, ctx.Wireless
	clients := 0
	ok := s != nil && s.Clients != nil
	if ok {
		clients = s.Clients.Total
	}
	cnt := "—"
	if ok {
		cnt = fmt.Sprintf("%d", clients)
	}
	shortMetric(c, th, 6, tr(lang, "在线设备", "Online"), cnt, "", th.Text)
	shortMetric(c, th, 110, tr(lang, "频段", "Band"), strings.ToUpper(dashIfEmpty(w.Band)), "", th.Accent)
	chU := ""
	if w.Channel != "" {
		chU = strings.ToUpper(w.HTMode)
	}
	shortMetric(c, th, 214, tr(lang, "信道", "Ch"), dashIfEmpty(w.Channel), chU, th.Text)

	c.Text(face("uib16"), contentX, 105, fitText(face("uib16"), dashIfEmpty(w.SSID), cardW-16), th.Text)
	card(c, th, 6, 113, cardW, infoH)
	mode := strings.ToUpper(w.Mode)
	if mode == "" {
		mode = "AP"
	}
	line1 := joinParts(wifiSecurity(w.Encryption), mode, dashIfEmpty(w.Radio), "ch"+dashIfEmpty(w.Channel)+" "+strings.ToUpper(w.HTMode))
	line2 := tr(lang, "在线 ", "online ") + "—"
	if ok {
		line2 = fmt.Sprintf("%s %d", tr(lang, "在线", "online"), clients)
		var names []string
		for i, cl := range s.Clients.Items {
			if i >= 2 {
				break
			}
			name := cl.Host
			if name == "" {
				name = cl.IP
			}
			if name == "" {
				name = cl.MAC
			}
			if cl.Host != "" && cl.IP != "" {
				name = cl.Host + " " + cl.IP
			}
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			line2 += " · " + strings.Join(names, " · ")
		}
	}
	c.Text(face("ui9"), 13, 127, fitText(face("ui9"), line1, 292), th.Text2)
	c.Text(face("ui9"), 13, 140, fitText(face("ui9"), line2, 292), th.Text2)

	metaL := joinParts(mode, wifiStandard(w.HTMode), "WMM "+onOff(lang, w.WMM))
	metaR := tr(lang, "隐藏 SSID ", "Hidden SSID ") + yesNo(lang, w.Hidden)
	c.Text(face("ui9"), contentX, 165, fitText(face("ui9"), metaL, 200), th.Text2)
	c.TextRight(face("ui9"), contentR, 165, metaR, th.Text2)
}

func renderSystem(c *render.Canvas, ctx *Context) {
	th, s, lang := ctx.Theme, ctx.Snapshot, ctx.Lang
	upV, upU, upS := "—", "", "—"
	loadV, loadS := "—", "—"
	rootV, rootU, rootS := "—", "", "—"
	ok := s != nil
	if ok {
		upV, upU = uptimeParts(s.Host.Uptime, lang)
		upS = tr(lang, "启动 ", "boot ") + bootClock(ctx.Now, s.Host.Uptime)
		loadV = fmt.Sprintf("%.2f", s.System.Load[0])
		loadS = fmt.Sprintf("%.2f / %.2f", s.System.Load[1], s.System.Load[2])
		rootV, rootU = fmt.Sprintf("%.0f", s.System.Rootfs.Pct), "%"
		rootS = sizePair(s.System.Rootfs.Used, s.System.Rootfs.Total)
	}
	tallMetric(c, th, 6, tr(lang, "运行", "Uptime"), upV, upU, upS, th.Text, -1, 0)
	tallMetric(c, th, 110, tr(lang, "负载", "Load"), loadV, "", loadS, th.Text, -1, 0)
	tallMetric(c, th, 214, tr(lang, "根分区", "Rootfs"), rootV, rootU, rootS, th.OK, -1, 0)

	line1, line2 := "—", "—"
	if ok {
		line1 = joinParts(dashIfEmpty(s.Host.Model), dashIfEmpty(shortRelease(s.Host.Release)))
		cpu := cpuShort(lang, s)
		freq := fmtMHz(s.System.CPUMHz)
		mem := fmt.Sprintf("%s %s", tr(lang, "内存", "mem"), pct(s.System.Mem.Pct))
		temp := fmtTemp(lang, s.System.TempC, ctx.Config.TempUnit)
		procs := fmt.Sprintf("%d %s", s.System.Procs, tr(lang, "进程", "procs"))
		line2 = joinParts("Linux "+dashIfEmpty(s.Host.Kernel), strings.TrimSpace(cpu+" "+freq), mem, temp, procs)
	}
	drawTextStrip(c, th, 136, 150, line1, line2)
}

func renderClock(c *render.Canvas, ctx *Context) {
	th, lang := ctx.Theme, ctx.Lang
	txt := ctx.Now.Format("15:04")
	if !ctx.Config.Clock24 {
		txt = ctx.Now.Format("3:04 PM")
	}
	c.TextCenter(face("clock64"), 160, 104, txt, th.Text)
	var date string
	if lang == "en" {
		date = ctx.Now.Format("Mon, Jan 2")
	} else {
		week := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[int(ctx.Now.Weekday())]
		date = fmt.Sprintf("%d月%d日 %s", int(ctx.Now.Month()), ctx.Now.Day(), week)
	}
	c.TextCenter(face("uib16"), 160, 126, date, th.Text2)
	if ctx.Snapshot != nil {
		c.TextCenter(face("ui11"), 160, 146, tr(lang, "已运行 ", "up ")+fmtUptime(lang, ctx.Snapshot.Host.Uptime), th.Text2)
	}
}

func renderNote(c *render.Canvas, ctx *Context) {
	th, lang := ctx.Theme, ctx.Lang
	title := ctx.Config.Note.Title
	if title == "" {
		title = tr(lang, "便签", "Note")
	}
	c.Text(face("uib16"), 8, 56, fitText(face("uib16"), title, 300), th.Text)
	y := 80
	empty := true
	for _, line := range ctx.Config.Note.Lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		empty = false
		c.Text(face("ui13"), 8, y, fitText(face("ui13"), line, 300), th.Text2)
		y += 18
	}
	if empty {
		c.Text(face("ui13"), 8, 80, tr(lang, "（未设置便签内容）", "(no note configured)"), th.Text3)
	}
}

// ---- 组件 ----

func face(name string) *render.Face { return render.FaceByName(name) }

func tallMetric(c *render.Canvas, th *render.Theme, x int, label, value, unit, sub string, color uint32, bar float64, barColor uint32) {
	card(c, th, x, tallY, 98, tallH)
	c.Text(face("ui11"), x+8, tallY+17, fitText(face("ui11"), label, 82), th.Text2)
	drawValue(c, th, face("uib28"), face("ui11"), x+8, tallY+47, value, unit, color, 82)
	if sub != "" {
		c.Text(face("ui9"), x+8, tallY+80, fitText(face("ui9"), sub, 82), th.Text2)
	}
	if bar >= 0 {
		progress(c, th, x+8, tallY+85, 82, bar, barColor)
	}
}

func shortMetric(c *render.Canvas, th *render.Theme, x int, label, value, unit string, color uint32) {
	card(c, th, x, shortY, 98, shortH)
	c.Text(face("ui11"), x+8, shortY+17, fitText(face("ui11"), label, 82), th.Text2)
	drawValue(c, th, face("uib22"), face("ui11"), x+8, shortY+42, value, unit, color, 82)
}

func drawValue(c *render.Canvas, th *render.Theme, vf, uf *render.Face, x, baseline int, value, unit string, color uint32, maxW int) {
	if value == "" {
		value = "—"
	}
	uw := 0
	if unit != "" {
		uw = uf.TextWidth(unit) + 2
	}
	if vf.TextWidth(value)+uw > maxW {
		value = fitText(vf, value, maxW-uw)
		if value == "" {
			value = "—"
		}
	}
	c.Text(vf, x, baseline, value, color)
	if unit != "" {
		c.Text(uf, x+vf.TextWidth(value)+2, baseline, unit, th.Text2)
	}
}

func drawKV(c *render.Canvas, th *render.Theme, x, baseline int, label, value string, maxW int) {
	lf, vf := face("ui9"), face("ui11")
	lw := lf.TextWidth(label)
	c.Text(lf, x, baseline, label, th.Text2)
	c.Text(vf, x+lw+4, baseline, fitText(vf, dashIfEmpty(value), maxW-lw-4), th.Text)
}

func drawStrip(c *render.Canvas, th *render.Theme, y1, y2 int, top, bottom string, rx, tx int64, rates bool) {
	c.Text(face("ui9"), contentX, y1, fitText(face("ui9"), top, contentR-contentX), th.Text2)
	bottomMax := contentR - contentX
	if rates {
		bottomMax -= rateWidth(rx, tx) + 10
		if bottomMax < 80 {
			bottomMax = 80
		}
	}
	c.Text(face("mono11"), contentX, y2, fitText(face("mono11"), dashIfEmpty(bottom), bottomMax), th.Text)
	if rates {
		drawRates(c, th, contentR, y2, rx, tx)
	}
}

func drawTextStrip(c *render.Canvas, th *render.Theme, y1, y2 int, top, bottom string) {
	c.Text(face("ui9"), contentX, y1, fitText(face("ui9"), top, contentR-contentX), th.Text2)
	c.Text(face("ui9"), contentX, y2, fitText(face("ui9"), bottom, contentR-contentX), th.Text2)
}

func drawRates(c *render.Canvas, th *render.Theme, right, baseline int, rx, tx int64) {
	f := face("mono11")
	down, up := "↓ "+fmtRate(rx), "↑ "+fmtRate(tx)
	uw, dw := f.TextWidth(up), f.TextWidth(down)
	c.Text(f, right-uw-dw-8, baseline, down, th.Accent)
	c.Text(f, right-uw, baseline, up, th.OK)
}

func rateWidth(rx, tx int64) int {
	f := face("mono11")
	return f.TextWidth("↓ "+fmtRate(rx)) + 8 + f.TextWidth("↑ "+fmtRate(tx))
}

// ---- 数值/名称 ----

// rateParts 拆分主数值与单位；入参按快照契约是 Byte/s，换算为 bit/s 显示。
func rateParts(bytesPerSec int64) (string, string) {
	if bytesPerSec < 0 {
		bytesPerSec = 0
	}
	bps := float64(bytesPerSec) * 8
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f", bps/1e9), "Gbps"
	case bps >= 1e8:
		return fmt.Sprintf("%.0f", bps/1e6), "Mbps"
	case bps >= 1e6:
		return fmt.Sprintf("%.1f", bps/1e6), "Mbps"
	case bps >= 1e3:
		return fmt.Sprintf("%.0f", bps/1e3), "Kbps"
	default:
		return fmt.Sprintf("%.0f", bps), "bps"
	}
}

func tempParts(c *float64, unit string) (string, string) {
	if c == nil || *c < -40 || *c > 150 {
		return "—", ""
	}
	v := *c
	if unit == "f" {
		return fmt.Sprintf("%.0f", v*9/5+32), "°F"
	}
	return fmt.Sprintf("%.0f", v), "°C"
}

func uptimeParts(sec int64, lang string) (string, string) {
	if sec < 0 {
		sec = 0
	}
	d, h, m := sec/86400, (sec%86400)/3600, (sec%3600)/60
	if d > 0 {
		return fmt.Sprintf("%d", d), tr(lang, "天", "d")
	}
	if h > 0 {
		return fmt.Sprintf("%d", h), tr(lang, "小时", "h")
	}
	return fmt.Sprintf("%d", m), tr(lang, "分钟", "min")
}

func bootClock(now time.Time, sec int64) string {
	if sec <= 0 {
		return "—"
	}
	return now.Add(-time.Duration(sec) * time.Second).Format("15:04")
}

func sizeParts(n int64) (string, string) {
	f := float64(n)
	switch {
	case f >= 1<<40:
		return fmt.Sprintf("%.2f", f/(1<<40)), "TB"
	case f >= 1<<30:
		return fmt.Sprintf("%.1f", f/(1<<30)), "GB"
	case f >= 1<<20:
		return fmt.Sprintf("%.1f", f/(1<<20)), "MB"
	default:
		return fmt.Sprintf("%d", n), "B"
	}
}

func sizePair(used, total int64) string {
	if total <= 0 {
		return "—"
	}
	uv, uu := sizeParts(used)
	tv, tu := sizeParts(total)
	if uu == tu {
		return uv + " / " + tv + " " + tu
	}
	return uv + " " + uu + " / " + tv + " " + tu
}

func fmtMHz(mhz int) string {
	if mhz <= 0 {
		return ""
	}
	if mhz >= 1000 {
		return fmt.Sprintf("%.1f GHz", float64(mhz)/1000)
	}
	return fmt.Sprintf("%d MHz", mhz)
}

func cpuShort(lang string, s *snapshot.Snapshot) string {
	if s == nil {
		return ""
	}
	if s.System.Cores > 0 {
		for _, k := range []string{"A55", "A53", "A72", "A76", "A78"} {
			if strings.Contains(s.System.CPUModel, k) {
				return fmt.Sprintf("%d×%s", s.System.Cores, k)
			}
		}
		return fmt.Sprintf(tr(lang, "%d 核", "%d cores"), s.System.Cores)
	}
	return s.System.CPUModel
}

func pctUnit(ok bool) string {
	if ok {
		return "%"
	}
	return ""
}

func barOf(ok bool, v float64) float64 {
	if !ok {
		return -1
	}
	return v
}

func qualityColor(th *render.Theme, q float64) uint32 {
	switch {
	case q >= 60:
		return th.OK
	case q >= 30:
		return th.Warn
	default:
		return th.Err
	}
}

func peak(v []int64) int64 {
	var m int64
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}

func shortIPv6(s string) string {
	if len(s) <= 18 {
		return s
	}
	parts := strings.Split(s, ":")
	if len(parts) >= 4 {
		keep := parts[len(parts)-3:]
		return parts[0] + ":…:" + strings.Join(keep, ":")
	}
	return s[:15] + "…"
}

func pickOperator(lang string, m *snapshot.Modem) (string, string) {
	if m == nil {
		return tr(lang, "未知运营商", "unknown operator"), ""
	}
	var cands []string
	cands = append(cands, m.Operator)
	if m.SIM != nil {
		cands = append(cands, m.SIM.ISP)
	}
	cands = append(cands, m.Vendor)
	name := ""
	for _, c := range cands {
		if !validOperator(c) {
			continue
		}
		n, ic := operatorInfo(lang, c)
		if ic != "" {
			return n, ic
		}
		if name == "" {
			name = n
		}
	}
	if name == "" {
		name = tr(lang, "未知运营商", "unknown operator")
	}
	return name, ""
}

func operatorOverride(lang, key string) (string, string) {
	switch key {
	case "mobile":
		return tr(lang, "中国移动", "China Mobile"), "china-mobile"
	case "unicom":
		return tr(lang, "中国联通", "China Unicom"), "china-unicom"
	case "telecom":
		return tr(lang, "中国电信", "China Telecom"), "china-telecom"
	case "broadnet":
		return tr(lang, "中国广电", "China Broadnet"), ""
	default:
		return tr(lang, "未知运营商", "unknown operator"), ""
	}
}

func validOperator(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return false
	}
	switch t {
	case "????", "???", "---", "n/a", "na", "unknown", "none", "0":
		return false
	}
	return true
}

func fmtBand(m *snapshot.Modem) string {
	if m == nil || m.CellInfo == nil {
		return "—"
	}
	b := strings.TrimSpace(m.CellInfo.Band)
	if b == "" {
		return "—"
	}
	net := m.NetworkMode + " " + m.NetworkType + " " + m.CellInfo.NetworkMode
	if isDigits(b) && (strings.Contains(net, "5G") || strings.Contains(strings.ToUpper(net), "NR")) {
		return "n" + b
	}
	return b
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func fmtBandwidth(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	if isDigits(s) {
		return s + "MHz"
	}
	if !strings.HasSuffix(s, "Hz") && (strings.HasSuffix(s, "M") || strings.HasSuffix(s, "G")) {
		return s + "Hz"
	}
	return s
}

func shortRelease(s string) string {
	if i := strings.Index(s, " ("); i > 0 {
		return s[:i]
	}
	return s
}

func joinParts(parts ...string) string {
	out := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func simLabel(lang, status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return "—"
	case "ready":
		return tr(lang, "就绪", "ready")
	case "absent":
		return tr(lang, "无卡", "absent")
	case "not_ready":
		return tr(lang, "未就绪", "not ready")
	default:
		return strings.ToUpper(status)
	}
}

func wifiSecurity(enc string) string {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "", "none":
		return tr("", "开放", "Open")
	case "psk", "psk2":
		return "WPA2-PSK"
	case "psk-mixed":
		return "WPA/WPA2"
	case "sae":
		return "WPA3-SAE"
	case "sae-mixed":
		return "WPA2/WPA3"
	case "owe":
		return "OWE"
	default:
		return strings.ToUpper(enc)
	}
}

func wifiStandard(htmode string) string {
	u := strings.ToUpper(htmode)
	switch {
	case strings.HasPrefix(u, "HE"):
		return "802.11ax"
	case strings.HasPrefix(u, "VHT"):
		return "802.11ac"
	case strings.HasPrefix(u, "HT"):
		return "802.11n"
	default:
		return ""
	}
}

func onOff(lang string, v bool) string {
	if v {
		return tr(lang, "开启", "on")
	}
	return tr(lang, "关闭", "off")
}

func yesNo(lang string, v bool) string {
	if v {
		return tr(lang, "是", "Yes")
	}
	return tr(lang, "否", "No")
}

func operatorInfo(lang, raw string) (name, icon string) {
	s := strings.TrimSpace(raw)
	up := strings.ToUpper(s)
	plmn := plmn5(s)
	in := func(v string) bool { return s == v }
	switch {
	case strings.Contains(s, "移动") || strings.Contains(up, "MOBILE") || in("46000") || in("46002") || in("46004") || in("46007") || in("46008"):
		return tr(lang, "中国移动", "China Mobile"), "china-mobile"
	case strings.Contains(s, "联通") || strings.Contains(up, "UNICOM") || in("46001") || in("46006") || in("46009"):
		return tr(lang, "中国联通", "China Unicom"), "china-unicom"
	case strings.Contains(s, "电信") || strings.Contains(up, "TELECOM") || in("46003") || in("46005") || in("46011"):
		return tr(lang, "中国电信", "China Telecom"), "china-telecom"
	case strings.Contains(s, "广电") || strings.Contains(up, "BROADNET") || plmn == "46015":
		return tr(lang, "中国广电", "China Broadnet"), ""
	}
	if s == "" {
		return "—", ""
	}
	return s, ""
}

func plmn5(s string) string {
	for i := 0; i+5 <= len(s); i++ {
		ok := true
		for j := i; j < i+5; j++ {
			if s[j] < '0' || s[j] > '9' {
				ok = false
				break
			}
		}
		if ok {
			return s[i : i+5]
		}
	}
	return ""
}

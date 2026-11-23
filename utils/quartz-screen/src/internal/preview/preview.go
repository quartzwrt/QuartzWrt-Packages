// Package preview 渲染单页为 PNG/Base64（LuCI 预览与设计稿共用同一渲染器）。
package preview

import (
	"math"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/config"
	"github.com/quartzwrt/quartz-screen/internal/pages"
	"github.com/quartzwrt/quartz-screen/internal/render"
	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

// Options 预览参数。
type Options struct {
	Page     string
	Theme    string
	Lang     string
	Demo     bool
	AllPages bool
	Scale    int
	Now      time.Time
}

// Render 渲染指定页面（返回画布）。
func Render(cfg config.Config, opt Options) (*render.Canvas, error) {
	if err := render.LoadFonts(); err != nil {
		return nil, err
	}
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	var s *snapshot.Snapshot
	var w snapshot.Wireless
	if opt.Demo {
		s, w = Demo()
		if strings.TrimSpace(cfg.Note.Title) == "" {
			cfg.Note.Title = "机房便签"
			cfg.Note.Lines = [4]string{"联系: @nrxboat", "SSID: QuartzWrt-5G", "维护: 周日 02:00", "固件: v0.2.4"}
		}
	} else {
		s, _, _ = snapshot.Load(cfg.StatusFile)
		w = config.LoadWireless()
	}
	lang := opt.Lang
	if lang == "" {
		lang = cfg.Lang
	}
	if lang == "auto" || lang == "" {
		lang = "zh"
	}
	pageList := pages.Resolve(cfg, s, w)
	if opt.AllPages {
		pageList = nil
		for _, id := range PageIDs() {
			if pages.Available(id, s, w, cfg) {
				pageList = append(pageList, id)
			}
		}
	}
	page := opt.Page
	if page == "" {
		page = pageList[0]
	}
	idx := 0
	for i, id := range pageList {
		if id == page {
			idx = i
		}
	}
	theme := opt.Theme
	if theme == "" {
		theme = cfg.Theme
	}
	ctx := &pages.Context{
		Now:      now,
		Snapshot: s,
		Wireless: w,
		Config:   cfg,
		Lang:     lang,
		Theme:    render.ThemeByName(theme),
		Index:    idx,
		Count:    len(pageList),
		Stale:    s == nil || now.Unix()-s.Ts > 30,
	}
	c := render.NewCanvas(render.ScreenW, render.ScreenH)
	pages.Render(c, page, ctx)
	return c, nil
}

// PNG 渲染并编码 PNG。
func PNG(cfg config.Config, opt Options) ([]byte, error) {
	c, err := Render(cfg, opt)
	if err != nil {
		return nil, err
	}
	scale := opt.Scale
	if scale < 1 {
		scale = 1
	}
	return c.PNG(scale)
}

// Demo 生成设计稿用的样例数据。
func Demo() (*snapshot.Snapshot, snapshot.Wireless) {
	var s snapshot.Snapshot
	s.V = 1
	s.Ts = time.Now().Unix()
	s.Host.Hostname = "NL68K"
	s.Host.Model = "NLnet XiGuaPi V3"
	s.Host.Release = "QuartzWrt 0.2.4"
	s.Host.Kernel = "6.12.112"
	s.Host.Uptime = 28*60 + 12
	s.System.CPU = 12.3
	s.System.Mem.Total = 4 << 30
	s.System.Mem.Used = 1610612736
	s.System.Mem.Pct = 38.4
	s.System.Rootfs.Total = 9126805504 // 8.5 GiB
	s.System.Rootfs.Used = 2147483648  // 2.0 GiB
	s.System.Rootfs.Pct = 23.0
	temp := 46.2
	s.System.TempC = &temp
	s.System.Load = [3]float64{0.12, 0.08, 0.05}
	s.System.CPUModel = "Cortex-A55"
	s.System.Cores = 4
	s.System.CPUMHz = 1272
	s.System.Procs = 211
	s.Net.WAN.Up = true
	s.Net.WAN.Ifname = "eth1"
	s.Net.WAN.Proto = "dhcp"
	s.Net.WAN.IPv4 = "192.168.1.2"
	s.Net.WAN.IPv6 = "240e:3b8:1a2f:8c00::2"
	s.Net.WAN.RxBps = 12_400_000
	s.Net.WAN.TxBps = 3_100_000
	s.Net.Series.Step = 2
	for i := 0; i < 60; i++ {
		rx := int64(9_000_000 + 4_000_000*math.Sin(float64(i)/5.0))
		tx := int64(2_200_000 + 1_200_000*math.Cos(float64(i)/7.0))
		s.Net.Series.Rx = append(s.Net.Series.Rx, rx)
		s.Net.Series.Tx = append(s.Net.Series.Tx, tx)
	}
	s.Clients = &snapshot.Clients{Total: 23, Items: []snapshot.Client{
		{MAC: "aa:bb:cc:00:11:22", IP: "192.168.1.100", Host: "MacBook-Pro", Iface: "br-lan"},
		{MAC: "aa:bb:cc:00:33:44", IP: "192.168.1.101", Host: "iPhone", Iface: "br-lan"},
		{MAC: "aa:bb:cc:00:55:66", IP: "192.168.1.102", Host: "NAS", Iface: "br-lan"},
	}}
	s.Bandix = &snapshot.Bandix{Available: true, Top: []snapshot.BandixTop{
		{Host: "NAS", IP: "192.168.1.102", Pct: 38, RxB: 9_000_000, TxB: 5_000_000, RxR: 5_200_000, TxR: 3_000_000},
		{Host: "MacBook-Pro", IP: "192.168.1.100", Pct: 22, RxB: 5_000_000, TxB: 2_000_000, RxR: 3_300_000, TxR: 1_400_000},
		{Host: "iPhone", IP: "192.168.1.101", Pct: 11, RxB: 2_000_000, TxB: 600_000, RxR: 1_800_000, TxR: 500_000},
	}}
	s.Cellular = &snapshot.Cellular{Available: true}
	rsrp := int64(-82)
	sinr := 21.4
	q := 78.0
	mtemp := 38.0
	modem := snapshot.Modem{
		ID: "1_1", Model: "RM500Q-GL", Operator: "中国联通",
		NetworkMode: "5G SA", NetworkType: "TDD NR5G", Registered: true,
		ConnectStatus: "connected", TempC: &mtemp,
		CellInfo: &snapshot.CellInfo{Band: "n78", DLBandwidth: "100MHz", PCI: "123", ARFCN: "630000"},
		Signal:   &snapshot.Signal{RSRP: &rsrp, SINR: &sinr, QualityPct: &q},
		Dial:     &snapshot.Dial{Running: true, IPv4: "10.64.1.2"},
		Traffic:  &snapshot.Traffic{Available: true, RxBytes: 12_300_000_000, TxBytes: 1_200_000_000},
		SIM:      &snapshot.SIM{Status: "ready", ISP: "46001"},
	}
	s.Cellular.Modems = append(s.Cellular.Modems, modem)

	w := snapshot.Wireless{
		Present:    true,
		SSID:       "QuartzWrt-5G",
		Band:       "5g",
		Channel:    "36",
		HTMode:     "HE80",
		Radio:      "radio0",
		Iface:      "default_radio0",
		Encryption: "sae",
		Mode:       "ap",
		Network:    "lan",
		Hidden:     false,
		WMM:        true,
	}
	return &s, w
}

// PageIDs 返回内置页面 ID 列表。
func PageIDs() []string {
	out := make([]string, 0, len(pages.Catalog))
	for _, p := range pages.Catalog {
		out = append(out, p.ID)
	}
	return out
}

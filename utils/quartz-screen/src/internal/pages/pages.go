// Package pages 实现小屏页面库与排版。
package pages

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/config"
	"github.com/quartzwrt/quartz-screen/internal/render"
	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

// Page 描述一个内置页面。
type Page struct {
	ID    string
	Title [2]string // zh, en
}

// Catalog 是页面库（顺序即 LuCI 默认展示顺序）。
var Catalog = []Page{
	{"overview", [2]string{"概览", "Overview"}},
	{"network", [2]string{"网络", "Network"}},
	{"wireless", [2]string{"无线", "Wireless"}},
	{"cellular", [2]string{"蜂窝", "Cellular"}},
	{"system", [2]string{"系统", "System"}},
	{"clock", [2]string{"时钟", "Clock"}},
	{"note", [2]string{"便签", "Note"}},
}

// Context 是渲染一页所需的上下文。
type Context struct {
	Now      time.Time
	Snapshot *snapshot.Snapshot
	Wireless snapshot.Wireless
	Config   config.Config
	Lang     string // zh|en
	Theme    *render.Theme
	Index    int
	Count    int
	Stale    bool

	currentID string
}

// Title 返回页面标题。
func Title(lang, id string) string {
	for _, p := range Catalog {
		if p.ID == id {
			if lang == "en" {
				return p.Title[1]
			}
			return p.Title[0]
		}
	}
	return id
}

// Available 判断页面在当前离线/数据条件下是否可展示。
func Available(id string, s *snapshot.Snapshot, w snapshot.Wireless, cfg config.Config) bool {
	switch id {
	case "cellular":
		return s != nil && s.Cellular != nil && s.Cellular.Available && len(s.Cellular.Modems) > 0
	case "wireless":
		return w.Present
	case "note":
		if strings.TrimSpace(cfg.Note.Title) != "" {
			return true
		}
		for _, l := range cfg.Note.Lines {
			if strings.TrimSpace(l) != "" {
				return true
			}
		}
		return false
	}
	return true
}

// Resolve 按配置顺序过滤出实际轮播页；空列表回退 overview。
func Resolve(cfg config.Config, s *snapshot.Snapshot, w snapshot.Wireless) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range cfg.Pages {
		if seen[id] || !Available(id, s, w, cfg) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		out = []string{"overview"}
	}
	return out
}

// Render 渲染页面到画布。
func Render(c *render.Canvas, id string, ctx *Context) {
	ctx.currentID = id
	c.Clear(ctx.Theme.Bg)
	renderTopBar(c, ctx)
	switch id {
	case "network":
		renderNetwork(c, ctx)
	case "wireless":
		renderWireless(c, ctx)
	case "cellular":
		renderCellular(c, ctx)
	case "system":
		renderSystem(c, ctx)
	case "clock":
		renderClock(c, ctx)
	case "note":
		renderNote(c, ctx)
	default:
		renderOverview(c, ctx)
	}
}

func renderTopBar(c *render.Canvas, ctx *Context) {
	th := ctx.Theme
	title := Title(ctx.Lang, currentPageID(ctx))
	if currentPageID(ctx) == "overview" && ctx.Config.Title != "" {
		title = ctx.Config.Title
	}
	c.Text(render.FaceByName("uib13"), render.Margin, 20, fitText(render.FaceByName("uib13"), title, 132), th.Text)

	// 页点放顶栏，底部留给数据行（R2）。
	n := ctx.Count
	if n < 1 {
		n = 1
	}
	const dot, gap = 4, 6
	total := n*dot + (n-1)*gap
	x := (render.ScreenW - total) / 2
	for i := 0; i < n; i++ {
		cx := x + i*(dot+gap) + dot/2
		col := th.Text3
		if i == ctx.Index {
			col = th.Accent
		}
		c.RoundRect(cx-2, 7, 4, 4, 1, col, col)
	}

	clock := ctx.Now.Format("15:04")
	if !ctx.Config.Clock24 {
		clock = ctx.Now.Format("3:04 PM")
	}
	if currentPageID(ctx) == "clock" {
		clock = ctx.Now.Format("01/02")
	}
	if ctx.Stale {
		c.Circle(276, 13, 2, th.Warn, true)
	}
	c.TextRight(render.FaceByName("mono11"), render.ScreenW-render.Margin-2, 20, clock, th.Text2)
}

// currentPageID 由 Render 设置到 ctx（避免每个页面重复传 id）。
func currentPageID(ctx *Context) string {
	if ctx.currentID == "" {
		return "overview"
	}
	return ctx.currentID
}

// ---- 格式化 ----

func tr(lang, zh, en string) string {
	if lang == "en" {
		return en
	}
	return zh
}

// fmtRate 格式化速率。入参按快照契约是 Byte/s（net.wan.rx_bps、
// bandix rx_r/tx_r 与 net.series 同源），显示前换算为 bit/s。
func fmtRate(bytesPerSec int64) string {
	if bytesPerSec < 0 {
		bytesPerSec = 0
	}
	bps := float64(bytesPerSec) * 8
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f Gbps", bps/1e9)
	case bps >= 1e8:
		return fmt.Sprintf("%.0f Mbps", bps/1e6)
	case bps >= 1e6:
		return fmt.Sprintf("%.1f Mbps", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.0f Kbps", bps/1e3)
	default:
		return fmt.Sprintf("%.0f bps", bps)
	}
}

func fmtBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	f := float64(n)
	switch {
	case f >= 1e12:
		return fmt.Sprintf("%.2f TB", f/1e12)
	case f >= 1e9:
		return fmt.Sprintf("%.1f GB", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("%.1f MB", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1f KB", f/1e3)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func fmtUptime(lang string, sec int64) string {
	if sec < 0 {
		sec = 0
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	if lang == "en" {
		if d > 0 {
			return fmt.Sprintf("%dd %dh", d, h)
		}
		if h > 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dm", m)
	}
	if d > 0 {
		return fmt.Sprintf("%d 天 %d 小时", d, h)
	}
	if h > 0 {
		return fmt.Sprintf("%d 小时 %d 分", h, m)
	}
	return fmt.Sprintf("%d 分钟", m)
}

func fmtTemp(lang string, c *float64, unit string) string {
	if c == nil || *c < -40 || *c > 150 {
		return "—"
	}
	v := *c
	if unit == "f" {
		return fmt.Sprintf("%.0f°F", v*9/5+32)
	}
	return fmt.Sprintf("%.0f°C", v)
}

func pct(v float64) string {
	return fmt.Sprintf("%.0f%%", v)
}

func dash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func fitText(f *render.Face, s string, maxW int) string {
	if f.TextWidth(s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if f.TextWidth(string(r)+"…") <= maxW {
			return string(r) + "…"
		}
	}
	return ""
}

// ---- 小组件 ----

func card(c *render.Canvas, th *render.Theme, x, y, w, h int) {
	c.RoundRect(x, y, w, h, render.CardRadius, th.Panel, th.Border)
}

func progress(c *render.Canvas, th *render.Theme, x, y, w int, p float64, col uint32) {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	c.FillRect(x, y, w, 3, th.Well)
	c.FillRect(x, y, int(math.Round(float64(w)*p/100)), 3, col)
}

func metricColor(th *render.Theme, p float64) uint32 {
	switch {
	case p >= 90:
		return th.Err
	case p >= 70:
		return th.Warn
	default:
		return th.Accent
	}
}

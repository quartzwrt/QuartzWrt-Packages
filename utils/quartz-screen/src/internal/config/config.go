// Package config 读取 /etc/config/quartz-screen（uci）。
package config

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

// 页面 ID（与 LuCI DynamicList 取值一致）。
var KnownPages = []string{"overview", "network", "wireless", "cellular", "system", "clock", "note"}

// DefaultPages 默认轮播页。
var DefaultPages = []string{"overview", "network", "cellular", "system"}

// Night 夜间窗口。
type Night struct {
	Enabled    bool
	Start      string // HH:MM
	End        string // HH:MM
	Brightness int    // 0-100
	Off        bool
}

// Note 便签页。
type Note struct {
	Title string
	Lines [4]string
}

// Config 运行配置。
type Config struct {
	Enabled       bool
	Brightness    int
	Theme         string // dark|light
	Lang          string // auto|zh|en
	Rotate        int    // 0|180
	Interval      int    // 秒；0=不轮播
	Pages         []string
	Title         string
	Operator      string // auto|mobile|unicom|telecom|broadnet
	TempUnit      string // c|f
	Clock24       bool
	ShowIPv6      bool
	ShowSparkline bool // deprecated: R2 起不再绘制折线图（兼容旧配置）
	ShowPeaks     bool
	ShowClients   bool
	FB            string
	Backlight     string
	StatusFile    string
	Night         Night
	Note          Note
}

// Defaults 内置默认值。
func Defaults() Config {
	return Config{
		Enabled:       true,
		Brightness:    80,
		Theme:         "dark",
		Lang:          "auto",
		Rotate:        0,
		Interval:      8,
		Pages:         append([]string(nil), DefaultPages...),
		Operator:      "auto",
		TempUnit:      "c",
		Clock24:       true,
		ShowIPv6:      true,
		ShowSparkline: true,
		ShowPeaks:     true,
		ShowClients:   true,
		FB:            "auto",
		Backlight:     "auto",
		StatusFile:    "/var/run/quartz/status.json",
		Night:         Night{Enabled: true, Start: "23:00", End: "07:00", Brightness: 10},
	}
}

// Load 读取系统配置（uci 不可用时返回默认值）。
func Load() Config {
	cfg := Defaults()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "uci", "-q", "show", "quartz-screen").Output(); err == nil {
		ApplyUCIShow(string(out), &cfg)
	}
	return cfg
}

// ApplyUCIShow 解析 `uci -q show quartz-screen` 输出。
func ApplyUCIShow(text string, cfg *Config) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimPrefix(strings.TrimSpace(line[:eq]), "quartz-screen.")
		val := unquote(strings.TrimSpace(line[eq+1:]))
		applyKey(key, val, cfg)
	}
	cfg.sanitize()
}

func applyKey(key, val string, cfg *Config) {
	switch key {
	case "main.enabled":
		cfg.Enabled = boolVal(val, cfg.Enabled)
	case "main.brightness":
		cfg.Brightness = intVal(val, cfg.Brightness)
	case "main.theme":
		cfg.Theme = val
	case "main.lang":
		cfg.Lang = val
	case "main.rotate":
		cfg.Rotate = intVal(val, cfg.Rotate)
	case "main.interval":
		cfg.Interval = intVal(val, cfg.Interval)
	case "main.pages":
		cfg.Pages = ParsePages(val)
	case "main.title":
		cfg.Title = val
	case "main.operator":
		cfg.Operator = val
	case "main.temp_unit":
		cfg.TempUnit = val
	case "main.clock_24h":
		cfg.Clock24 = boolVal(val, cfg.Clock24)
	case "main.show_ipv6":
		cfg.ShowIPv6 = boolVal(val, cfg.ShowIPv6)
	case "main.show_sparkline":
		cfg.ShowSparkline = boolVal(val, cfg.ShowSparkline)
	case "main.show_peaks":
		cfg.ShowPeaks = boolVal(val, cfg.ShowPeaks)
	case "main.show_clients":
		cfg.ShowClients = boolVal(val, cfg.ShowClients)
	case "main.fb":
		cfg.FB = val
	case "main.backlight":
		cfg.Backlight = val
	case "main.status_file":
		cfg.StatusFile = val
	case "night.enabled":
		cfg.Night.Enabled = boolVal(val, cfg.Night.Enabled)
	case "night.start":
		cfg.Night.Start = val
	case "night.end":
		cfg.Night.End = val
	case "night.brightness":
		cfg.Night.Brightness = intVal(val, cfg.Night.Brightness)
	case "night.off":
		cfg.Night.Off = boolVal(val, cfg.Night.Off)
	case "note.title":
		cfg.Note.Title = val
	case "note.line1":
		cfg.Note.Lines[0] = val
	case "note.line2":
		cfg.Note.Lines[1] = val
	case "note.line3":
		cfg.Note.Lines[2] = val
	case "note.line4":
		cfg.Note.Lines[3] = val
	}
}

func (c *Config) sanitize() {
	c.Brightness = clamp(c.Brightness, 1, 100)
	c.Night.Brightness = clamp(c.Night.Brightness, 0, 100)
	if c.Interval < 0 {
		c.Interval = 0
	}
	if c.Interval > 300 {
		c.Interval = 300
	}
	if c.Theme != "light" {
		c.Theme = "dark"
	}
	switch c.Lang {
	case "zh", "en":
	default:
		c.Lang = "auto"
	}
	if c.TempUnit != "f" {
		c.TempUnit = "c"
	}
	switch c.Operator {
	case "mobile", "unicom", "telecom", "broadnet":
	default:
		c.Operator = "auto"
	}
	if c.Rotate != 180 {
		c.Rotate = 0
	}
	if c.FB == "" {
		c.FB = "auto"
	}
	if c.Backlight == "" {
		c.Backlight = "auto"
	}
	if c.StatusFile == "" {
		c.StatusFile = "/var/run/quartz/status.json"
	}
	valid := map[string]bool{}
	for _, p := range KnownPages {
		valid[p] = true
	}
	seen := map[string]bool{}
	var pages []string
	for _, p := range c.Pages {
		p = strings.TrimSpace(p)
		if valid[p] && !seen[p] {
			seen[p] = true
			pages = append(pages, p)
		}
	}
	c.Pages = pages
	if c.Night.Start == "" {
		c.Night.Start = "23:00"
	}
	if c.Night.End == "" {
		c.Night.End = "07:00"
	}
}

// ParsePages 解析逗号/空格分隔的页面列表（兼容 uci list 的多引号形态）。
func ParsePages(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\'' || r == '"'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ParseClock 解析 HH:MM 为当日分钟数。
func ParseClock(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// InNightWindow 判断当前时刻是否落在夜间窗口（支持跨零点）。
func (c *Config) InNightWindow(now time.Time) bool {
	if !c.Night.Enabled {
		return false
	}
	start, ok1 := ParseClock(c.Night.Start)
	end, ok2 := ParseClock(c.Night.End)
	if !ok1 || !ok2 || start == end {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	if start < end {
		return cur >= start && cur < end
	}
	return cur >= start || cur < end
}

// LoadWireless 从 /etc/config/wireless 提取主 SSID/频段/信道/加密/模式等（静态信息）。
func LoadWireless() snapshot.Wireless {
	var w snapshot.Wireless
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "uci", "-q", "show", "wireless").Output()
	if err != nil {
		return w
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		values[strings.TrimSpace(line[:eq])] = unquote(strings.TrimSpace(line[eq+1:]))
	}
	// 收集 wifi-iface section，优先 mode=ap，其次按 section 名稳定排序。
	var sections []string
	for key := range values {
		if strings.HasPrefix(key, "wireless.") && strings.Count(key, ".") == 2 {
			section := strings.TrimSuffix(strings.TrimPrefix(key, "wireless."), ".ssid")
			if strings.HasSuffix(key, ".ssid") && values[key] != "" {
				sections = append(sections, section)
			}
		}
	}
	sort.Strings(sections)
	for _, section := range sections {
		if w.Iface == "" || values["wireless."+section+".mode"] == "ap" {
			w.Iface = section
			w.SSID = values["wireless."+section+".ssid"]
		}
	}
	if w.Iface != "" {
		radio := values["wireless."+w.Iface+".device"]
		if radio == "" {
			radio = "radio0"
		}
		w.Radio = radio
		w.Band = values["wireless."+radio+".band"]
		if w.Band == "" {
			w.Band = "2g"
		}
		w.Channel = values["wireless."+radio+".channel"]
		w.HTMode = values["wireless."+radio+".htmode"]
		w.Encryption = values["wireless."+w.Iface+".encryption"]
		w.Mode = values["wireless."+w.Iface+".mode"]
		w.Network = values["wireless."+w.Iface+".network"]
		w.Hidden = boolVal(values["wireless."+w.Iface+".hidden"], false)
		w.WMM = boolVal(values["wireless."+radio+".wmm"], true)
		w.Present = true
	}
	return w
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func boolVal(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func intVal(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

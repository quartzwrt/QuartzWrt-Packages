// Package daemon 实现 quartz-screen 的常驻渲染循环。
package daemon

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/config"
	"github.com/quartzwrt/quartz-screen/internal/fb"
	"github.com/quartzwrt/quartz-screen/internal/pages"
	"github.com/quartzwrt/quartz-screen/internal/preview"
	"github.com/quartzwrt/quartz-screen/internal/render"
	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

const (
	testDuration  = 8 * time.Second
	snapshotStale = 30 * time.Second
)

// StatePath / PIDPath 允许通过 QUARTZ_SCREEN_RUN_DIR 重定向（VM e2e 用）。
func StatePath() string { return filepath.Join(runDir(), "quartz-screen.json") }
func PIDPath() string   { return filepath.Join(runDir(), "quartz-screen.pid") }

func runDir() string {
	if v := os.Getenv("QUARTZ_SCREEN_RUN_DIR"); v != "" {
		return v
	}
	return "/var/run"
}

// Options 开发/测试选项（生产由 procd 无参调用）。
type Options struct {
	FBFile     string // 假 framebuffer 文件（VM e2e）
	StatusFile string // 覆盖快照路径
	Demo       bool   // 使用 demo 数据
	Once       bool   // 只渲染一帧后退出
}

type daemon struct {
	opts       Options
	cfg        config.Config
	lang       string
	wireless   snapshot.Wireless
	snap       *snapshot.Snapshot
	bandixPrev map[string]bandixPrev
	snapMod    int64
	pageList   []string
	pageIdx    int
	pageAt     time.Time

	dev       *fb.Device
	canvas    *render.Canvas
	lastHash  [20]byte
	lastPaint time.Time
	lastNight bool
	nightSet  bool
	lastBri   int

	testUntil time.Time
	nextFBTry time.Time
	briErr    bool
	errs      []string
}

// Run 启动守护进程（前台）。
func Run(opts Options) error {
	d := &daemon{
		opts:   opts,
		cfg:    config.Load(),
		canvas: render.NewCanvas(render.ScreenW, render.ScreenH),
	}
	if opts.StatusFile != "" {
		d.cfg.StatusFile = opts.StatusFile
	}
	if opts.Demo {
		d.snap, d.wireless = demoData()
	}
	if err := render.LoadFonts(); err != nil {
		return err
	}
	d.lang = resolveLang(d.cfg.Lang)
	if !opts.Demo {
		d.wireless = config.LoadWireless()
		d.reloadSnapshot()
	}
	d.pageList = pages.Resolve(d.cfg, d.snap, d.wireless)
	d.pageAt = time.Now()
	if opts.Once {
		d.openFB()
		d.paint()
		return nil
	}
	d.writePID()
	defer os.Remove(PIDPath())

	sig := make(chan os.Signal, 4)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sig)

	// 首次尝试打开 framebuffer；失败不退出，后台重试。
	d.openFB()
	d.applyBrightness(true)
	d.paint()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case s := <-sig:
			switch s {
			case syscall.SIGHUP:
				d.reloadConfig()
			case syscall.SIGUSR1:
				d.testUntil = time.Now().Add(testDuration)
				log.Printf("test pattern for %s", testDuration)
			case syscall.SIGTERM, syscall.SIGINT:
				d.shutdown()
				return nil
			}
		case now := <-tick.C:
			d.step(now)
		}
	}
}

func (d *daemon) step(now time.Time) {
	if d.dev == nil {
		if now.Before(d.nextFBTry) {
			d.writeStatus(now)
			return
		}
		d.nextFBTry = now.Add(5 * time.Second)
		d.openFB()
		if d.dev == nil {
			d.writeStatus(now)
			return
		}
		d.paint()
	}
	d.maybeReloadSnapshot()
	d.applyBrightness(false)
	if now.Before(d.testUntil) {
		d.drawTest(now)
		d.flush()
		d.writeStatus(now)
		return
	}
	if d.cfg.Interval > 0 && len(d.pageList) > 1 && now.Sub(d.pageAt) >= time.Duration(d.cfg.Interval)*time.Second {
		d.pageIdx = (d.pageIdx + 1) % len(d.pageList)
		d.pageAt = now
	}
	if d.pageIdx >= len(d.pageList) {
		d.pageIdx = 0
	}
	// 数据变化或跨分钟时重绘；其余情况跳过 SPI 写。
	d.redraw(now, false)
	d.writeStatus(now)
}

func (d *daemon) redraw(now time.Time, force bool) {
	if len(d.pageList) == 0 {
		d.pageList = []string{"overview"}
	}
	id := d.pageList[d.pageIdx%len(d.pageList)]
	ctx := &pages.Context{
		Now:      now,
		Snapshot: d.snap,
		Wireless: d.wireless,
		Config:   d.cfg,
		Lang:     d.lang,
		Theme:    render.ThemeByName(d.cfg.Theme),
		Index:    d.pageIdx,
		Count:    len(d.pageList),
		Stale:    d.snap == nil || now.Unix()-d.snap.Ts > int64(snapshotStale/time.Second),
	}
	pages.Render(d.canvas, id, ctx)
	if !force && d.hashCanvas() == d.lastHash {
		return
	}
	d.flush()
}

func (d *daemon) paint() {
	d.redraw(time.Now(), true)
}

func (d *daemon) flush() {
	if d.dev == nil {
		return
	}
	if err := d.dev.Write(d.canvas, d.cfg.Rotate == 180); err != nil {
		d.addErr("fb: " + err.Error())
		d.dev.Close()
		d.dev = nil
		return
	}
	d.lastHash = d.hashCanvas()
	d.lastPaint = time.Now()
}

func (d *daemon) hashCanvas() [20]byte {
	buf := make([]byte, 0, len(d.canvas.Pix)*4)
	for _, v := range d.canvas.Pix {
		buf = append(buf, byte(v>>16), byte(v>>8), byte(v))
	}
	return sha1.Sum(buf)
}

func (d *daemon) openFB() {
	if d.dev != nil {
		return
	}
	if d.opts.FBFile != "" {
		dev, err := fb.OpenFile(d.opts.FBFile, render.ScreenW, render.ScreenH, 16)
		if err != nil {
			d.addErr("fb: " + err.Error())
			return
		}
		d.dev = dev
		log.Printf("fake framebuffer %s (%dx%d)", dev.Path, dev.W, dev.H)
		return
	}
	path, err := fb.Detect(d.cfg.FB)
	if err != nil {
		d.addErr("fb: " + err.Error())
		return
	}
	dev, err := fb.Open(path)
	if err != nil {
		d.addErr("fb: " + err.Error())
		return
	}
	if dev.W != render.ScreenW || dev.H != render.ScreenH {
		d.addErr(fmt.Sprintf("fb: unexpected mode %dx%d (want %dx%d)", dev.W, dev.H, render.ScreenW, render.ScreenH))
		// 仍按实际分辨率画？尺寸不符时直接失败，避免撕裂。
		dev.Close()
		return
	}
	d.dev = dev
	log.Printf("framebuffer %s (%s)", dev.Info(), strings.TrimSpace(dev.Name))
}

func (d *daemon) maybeReloadSnapshot() {
	if d.opts.Demo {
		return
	}
	path := d.cfg.StatusFile
	st, err := os.Stat(path)
	if err != nil {
		if d.snap != nil {
			d.snap = nil
			d.reloadPages()
		}
		return
	}
	if st.ModTime().Unix() == d.snapMod && d.snap != nil {
		return
	}
	d.reloadSnapshot()
}

func (d *daemon) reloadSnapshot() {
	s, mod, err := snapshot.Load(d.cfg.StatusFile)
	if err != nil {
		if d.snap == nil {
			d.addErr("snapshot: " + err.Error())
		}
		return
	}
	d.snap = s
	d.snapMod = mod
	d.applyBandixRates(s, time.Now())
	d.clearErr("snapshot:")
	d.reloadPages()
}

type bandixPrev struct {
	rx, tx, at int64
}

// applyBandixRates 用 bandix.top 累计字节的差分补算设备 WAN 速率。
// bandix 0.12.x 的 t_rx_r/t_tx_r 可能恒为 0（实测），但 rx_b/tx_b 正常。
// 结果保持快照契约单位 Byte/s（渲染层再换算 bit/s）。
func (d *daemon) applyBandixRates(s *snapshot.Snapshot, now time.Time) {
	if s == nil || s.Bandix == nil {
		return
	}
	if d.bandixPrev == nil {
		d.bandixPrev = make(map[string]bandixPrev)
	}
	for i := range s.Bandix.Top {
		t := &s.Bandix.Top[i]
		key := t.MAC
		if key == "" {
			key = t.Host + "|" + t.IP
		}
		prev, ok := d.bandixPrev[key]
		if ok && now.Unix() > prev.at {
			dt := now.Unix() - prev.at
			if t.RxB >= prev.rx {
				t.RxR = (t.RxB - prev.rx) / dt
			} else {
				t.RxR = 0
			}
			if t.TxB >= prev.tx {
				t.TxR = (t.TxB - prev.tx) / dt
			} else {
				t.TxR = 0
			}
		}
		d.bandixPrev[key] = bandixPrev{rx: t.RxB, tx: t.TxB, at: now.Unix()}
	}
}

func (d *daemon) reloadConfig() {
	d.cfg = config.Load()
	if d.opts.StatusFile != "" {
		d.cfg.StatusFile = d.opts.StatusFile
	}
	d.lang = resolveLang(d.cfg.Lang)
	if !d.opts.Demo {
		d.wireless = config.LoadWireless()
	}
	d.reloadPages()
	d.applyBrightness(true)
	d.redraw(time.Now(), true)
	log.Printf("config reloaded")
}

func (d *daemon) reloadPages() {
	old := d.pageList
	d.pageList = pages.Resolve(d.cfg, d.snap, d.wireless)
	if len(old) != len(d.pageList) {
		d.pageIdx = 0
		d.pageAt = time.Now()
	}
}

func (d *daemon) shutdown() {
	if d.dev != nil {
		d.dev.Close()
		d.dev = nil
	}
	d.setBrightness(0)
}

func (d *daemon) clearErr(prefix string) {
	out := d.errs[:0]
	for _, e := range d.errs {
		if !strings.Contains(e, prefix) {
			out = append(out, e)
		}
	}
	d.errs = out
}

func (d *daemon) addErr(s string) {
	s = time.Now().Format("15:04:05") + " " + s
	d.errs = append(d.errs, s)
	if len(d.errs) > 8 {
		d.errs = d.errs[len(d.errs)-8:]
	}
	log.Print(s)
}

func (d *daemon) writePID() {
	if err := os.WriteFile(PIDPath(), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		log.Printf("pidfile: %v", err)
	}
}

func (d *daemon) writeStatus(now time.Time) {
	state := map[string]any{
		"running":        true,
		"pid":            os.Getpid(),
		"width":          render.ScreenW,
		"height":         render.ScreenH,
		"page":           d.currentPage(),
		"pages":          d.pageList,
		"brightness":     d.currentBrightness(),
		"night":          d.cfg.InNightWindow(now),
		"last_render_at": d.lastPaint.Unix(),
		"snapshot_ok":    d.snap != nil,
		"stale":          d.snap == nil || now.Unix()-d.snap.Ts > int64(snapshotStale/time.Second),
		"errors":         d.errs,
		"config_enabled": d.cfg.Enabled,
		"interval":       d.cfg.Interval,
	}
	if d.dev != nil {
		state["fb"] = d.dev.Path
		state["fb_name"] = strings.TrimSpace(d.dev.Name)
		state["bpp"] = d.dev.Bpp
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	path := StatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (d *daemon) currentPage() string {
	if len(d.pageList) == 0 {
		return "overview"
	}
	return d.pageList[d.pageIdx%len(d.pageList)]
}

func (d *daemon) currentBrightness() int {
	if d.cfg.InNightWindow(time.Now()) {
		if d.cfg.Night.Off {
			return 0
		}
		return d.cfg.Night.Brightness
	}
	return d.cfg.Brightness
}

// resolveLang 解析 auto 语言（跟随 LuCI 语言，默认中文）。
func resolveLang(lang string) string {
	if lang == "zh" || lang == "en" {
		return lang
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "uci", "-q", "get", "luci.main.lang").Output()
	if err == nil {
		if strings.HasPrefix(strings.TrimSpace(string(out)), "en") {
			return "en"
		}
		return "zh"
	}
	return "zh"
}

// BacklightPath 解析背光设备（LED 或 backlight class）。
func BacklightPath(configured string) (brightness, max string) {
	if configured != "" && configured != "auto" {
		return filepath.Join(configured, "brightness"), filepath.Join(configured, "max_brightness")
	}
	leds, _ := filepath.Glob("/sys/class/leds/*backlight*")
	for _, dir := range leds {
		return filepath.Join(dir, "brightness"), filepath.Join(dir, "max_brightness")
	}
	bls, _ := filepath.Glob("/sys/class/backlight/*")
	for _, dir := range bls {
		return filepath.Join(dir, "brightness"), filepath.Join(dir, "max_brightness")
	}
	return "", ""
}

func demoData() (*snapshot.Snapshot, snapshot.Wireless) {
	return preview.Demo()
}

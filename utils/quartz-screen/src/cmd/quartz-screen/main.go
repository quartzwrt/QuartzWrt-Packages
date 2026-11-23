// quartz-screen — QuartzWrt 小屏渲染守护进程与预览工具。
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/config"
	"github.com/quartzwrt/quartz-screen/internal/daemon"
	"github.com/quartzwrt/quartz-screen/internal/fb"
	"github.com/quartzwrt/quartz-screen/internal/preview"
	"github.com/quartzwrt/quartz-screen/internal/render"
	"github.com/quartzwrt/quartz-screen/internal/snapshot"
	"github.com/quartzwrt/quartz-screen/internal/tz"
)

var (
	version = "0.1.0"
	release = "dev"
)

func main() {
	tz.Setup()
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "status":
		err = cmdStatus()
	case "preview":
		err = cmdPreview(os.Args[2:])
	case "render":
		err = cmdRender(os.Args[2:])
	case "test":
		err = cmdTest()
	case "apply":
		err = cmdApply()
	case "version", "-v", "--version":
		fmt.Printf("quartz-screen %s (%s)\n", version, release)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "quartz-screen:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`usage: quartz-screen <command> [options]

commands:
  run                         前台运行渲染守护进程（procd 调用）
  status                      输出运行状态 JSON
  preview --page ID [...]     渲染单页：--format png|base64 --scale N
  render --all --out DIR      生成设计稿（暗/浅双主题 + preview.html）
  test                        在屏幕上显示测试图案 8 秒
  apply                       重启 quartz-screen 服务（LuCI 保存后调用）
  version                     版本`)
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var opts daemon.Options
	fs.StringVar(&opts.FBFile, "fb-file", "", "fake framebuffer file (VM e2e)")
	fs.StringVar(&opts.StatusFile, "status-file", "", "override snapshot path")
	fs.BoolVar(&opts.Demo, "demo", false, "use demo data")
	fs.BoolVar(&opts.Once, "once", false, "render one frame and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return daemon.Run(opts)
}

func cmdStatus() error {
	cfg := config.Load()
	out := map[string]any{
		"running":        false,
		"config_enabled": cfg.Enabled,
		"theme":          cfg.Theme,
		"lang":           cfg.Lang,
		"rotate":         cfg.Rotate,
		"interval":       cfg.Interval,
		"pages":          cfg.Pages,
		"brightness":     cfg.Brightness,
		"night":          false,
		"width":          render.ScreenW,
		"height":         render.ScreenH,
	}
	if raw, err := os.ReadFile(daemon.StatePath()); err == nil {
		var st map[string]any
		if json.Unmarshal(raw, &st) == nil {
			if pid, ok := st["pid"].(float64); ok && procAlive(int(pid)) {
				out = st
				out["running"] = true
			}
		}
	}
	if path, err := fb.Detect(cfg.FB); err == nil {
		out["fb"] = path
		if dev, err := fb.Open(path); err == nil {
			out["fb_name"] = strings.TrimSpace(dev.Name)
			out["width"] = dev.W
			out["height"] = dev.H
			out["bpp"] = dev.Bpp
			dev.Close()
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func cmdPreview(args []string) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	page := fs.String("page", "", "page id")
	theme := fs.String("theme", "", "dark|light")
	lang := fs.String("lang", "", "zh|en")
	demo := fs.Bool("demo", false, "use demo data")
	format := fs.String("format", "png", "png|base64")
	scale := fs.Int("scale", 1, "nearest-neighbor scale")
	out := fs.String("out", "", "output file (default stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := config.Load()
	opt := preview.Options{Page: *page, Theme: *theme, Lang: *lang, Demo: *demo, Scale: *scale}
	data, err := preview.PNG(cfg, opt)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil && filepath.Dir(*out) != "." {
			return err
		}
		return os.WriteFile(*out, data, 0o644)
	}
	if *format == "base64" {
		_, err := fmt.Fprintln(os.Stdout, base64.StdEncoding.EncodeToString(data))
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	all := fs.Bool("all", false, "render every page")
	out := fs.String("out", "mockups", "output directory")
	scale := fs.Int("scale", 2, "nearest-neighbor scale")
	demo := fs.Bool("demo", true, "use demo data")
	page := fs.String("page", "", "single page id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	cfg := config.Load()
	ids := []string{*page}
	if *all {
		ids = preview.PageIDs()
	}
	var sheet strings.Builder
	sheet.WriteString("<!doctype html><meta charset=utf-8><title>quartz-screen 320×172 mockups</title>")
	sheet.WriteString("<style>body{background:#111;color:#ddd;font:13px -apple-system,'Segoe UI',sans-serif;padding:18px}" +
		"h2{font-size:15px;margin:22px 0 8px;color:#fff}.row{display:flex;gap:18px;flex-wrap:wrap}" +
		".item{background:#1c1c1e;border:1px solid #3a3a3c;border-radius:12px;padding:10px}" +
		".item img{display:block;image-rendering:pixelated;width:640px;max-width:100%;height:auto;border-radius:6px}" +
		".cap{color:#999;font-size:12px;margin-top:6px}</style>")
	for _, theme := range []string{"dark", "light"} {
		fmt.Fprintf(&sheet, "<h2>%s</h2><div class=row>", theme)
		for _, id := range ids {
			opt := preview.Options{Page: id, Theme: theme, Demo: *demo, AllPages: true, Scale: *scale}
			data, err := preview.PNG(cfg, opt)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", theme, id, err)
			}
			name := fmt.Sprintf("%s-%s.png", theme, id)
			if err := os.WriteFile(filepath.Join(*out, name), data, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(&sheet, "<div class=item><img src=%q><div class=cap>%s · %s · %dx%d</div></div>",
				name, id, theme, render.ScreenW, render.ScreenH)
			fmt.Printf("wrote %s\n", filepath.Join(*out, name))
		}
		sheet.WriteString("</div>")
	}
	if *all {
		if err := os.WriteFile(filepath.Join(*out, "preview.html"), []byte(sheet.String()), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", filepath.Join(*out, "preview.html"))
	}
	return nil
}

func cmdTest() error {
	raw, err := os.ReadFile(daemon.PIDPath())
	if err != nil {
		return fmt.Errorf("service not running")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || !procAlive(pid) {
		return fmt.Errorf("service not running")
	}
	if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		return err
	}
	fmt.Println("test pattern enabled for 8s")
	return nil
}

func cmdApply() error {
	cmd := exec.Command("/etc/init.d/quartz-screen", "restart")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func procAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

var _ = snapshot.Snapshot{}
var _ = time.Now

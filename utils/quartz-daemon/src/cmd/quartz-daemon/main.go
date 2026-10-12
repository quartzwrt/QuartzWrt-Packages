// quartz-daemon — QuartzWrt 数据聚合守护进程（WP-4）。
//
// 设计规范: docs/DESIGN.md
// 数据契约: luci-app-dashboard/docs/AGGREGATE-API.md（冻结 v1，本进程为生产者）
//
// 运行模型（DESIGN §2）：单写者——fast 循环每 interval 装配快照并一次
// marshal，同时原子写文件与发布 HTTP；slow 循环独立刷新外部数据源缓存。
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/collect"
	"github.com/quartzwrt/quartz-daemon/internal/config"
	"github.com/quartzwrt/quartz-daemon/internal/httpsrv"
	"github.com/quartzwrt/quartz-daemon/internal/output"
)

// version/release 经 -ldflags -X 注入（Makefile GO_PKG_LDFLAGS_X）。
var (
	version = "dev"
	release = "1"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("quartz-daemon %s-%s\n", version, release)
		return
	}

	log.SetFlags(log.LstdFlags)
	log.SetPrefix("quartz-daemon: ")

	cfg := config.Load()
	log.Printf("starting %s-%s interval=%ds http=%s",
		version, release, cfg.Interval,
		net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.Port)))

	if err := output.EnsureDir(filepath.Dir(collect.StatusPath)); err != nil {
		log.Printf("warning: cannot create snapshot dir: %v", err)
	}

	col := collect.New(cfg)
	writer := output.New(collect.StatusPath)
	srv := httpsrv.New(net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.Port)))
	go srv.Run()

	// 启动即采：首个快照不等首个 interval（tmpfs 重启清空，尽快供数）
	col.Slow()
	publish := func() {
		b, err := col.Fast()
		if err != nil {
			log.Printf("collect: %v", err)
			return
		}
		if err := writer.Write(b); err != nil {
			log.Printf("write %s: %v", collect.StatusPath, err)
		}
		srv.Set(b)
	}
	publish()

	fastTick := time.NewTicker(time.Duration(cfg.Interval) * time.Second)
	slowTick := time.NewTicker(collect.SlowInterval)
	defer fastTick.Stop()
	defer slowTick.Stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case <-fastTick.C:
			publish()
		case <-slowTick.C:
			col.Slow()
		case s := <-sig:
			log.Printf("signal %s: exiting", s)
			return
		}
	}
}

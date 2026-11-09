// Package collect 实现快慢分离的数据采集（DESIGN §2）：
//
//   - 快周期（每 interval，默认 2s）：纯 /proc + /sys 文件读，装配全量快照，
//     一次 marshal → 原子写文件 + HTTP 同字节（单写者，无并发写竞争）
//   - 慢周期（每 10s）：ubus / docker socket / 设备列表 / 探针描述文件，
//     独立超时，失败只记 errors 不阻塞快周期；结果经互斥锁保护的缓存发布
package collect

import (
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/config"
	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

const (
	// StatusPath 快照文件路径（契约 §1 固定）。
	StatusPath = "/var/run/quartz/status.json"

	// SlowInterval 慢刷周期。
	SlowInterval = 10 * time.Second

	// ClientMarkerPath web Dashboard 活动标记：Dashboard 轮询快照时 touch 此文件。
	ClientMarkerPath = "/var/run/quartz/client.active"

	// probesShareDir 包内置探针描述目录。
	probesShareDir = "/usr/share/quartz-daemon/probes.d"
	// probesLocalDir 本地追加探针描述目录（同 id 覆盖内置）。
	probesLocalDir = "/etc/quartz-daemon/probes.d"
)

// Collector 采集器。
type Collector struct {
	cfg config.Config
	now func() time.Time

	// 快周期内部状态（仅 fast goroutine 访问）
	prevCPU    cpuTimes
	hasPrevCPU bool
	netPrev    netCounters
	hasNetPrev bool
	ringRx     *ring
	ringTx     *ring
	cellSeries *cellularSeriesStore // cellular 每模组 RSRP 环形（仅慢 goroutine 访问）

	// 空闲模式：App HTTP 请求（lastClient）或 web Dashboard 标记文件
	// （ClientMarkerPath）在 idle_timeout 内出现过，才跑慢采集。
	lastClient   atomic.Int64 // 最近一次 App HTTP 请求（UnixNano）
	idle         atomic.Bool
	wake         chan struct{}
	clientMarker string // web Dashboard 活动标记（测试可覆盖）

	mu   sync.Mutex
	slow slowCache
}

type slowCache struct {
	model       string // 型号兜底（/tmp/sysinfo/model 缺失时）
	wanProto    string
	docker      *snapshot.Docker
	clients     *snapshot.Clients
	services    map[string]any
	hasServices bool
	bandix      *snapshot.Bandix
	cellular    *snapshot.Cellular
	errs        map[string]string // 组件 → 当前错误（恢复即清除）
	probeWarns  []string          // 非法探针描述文件告警
}

// New 创建采集器。
func New(cfg config.Config) *Collector {
	c := &Collector{
		cfg:          cfg,
		now:          time.Now,
		ringRx:       newRing(snapshot.MaxSeries),
		ringTx:       newRing(snapshot.MaxSeries),
		cellSeries:   newCellularSeriesStore(),
		wake:         make(chan struct{}, 1),
		clientMarker: ClientMarkerPath,
	}
	// 启动即视为活跃：首轮慢采集必须执行，之后无客户端才进入空闲。
	c.lastClient.Store(c.now().UnixNano())
	return c
}

// Touch 标记一次 App HTTP 请求；若此前处于空闲，立即唤醒慢采集。
func (c *Collector) Touch() {
	c.lastClient.Store(c.now().UnixNano())
	if c.idle.Swap(false) {
		c.wakeSlow()
	}
}

// MaybeWake 由快周期调用：空闲时若检测到 web/App 活动（标记文件或 HTTP
// 请求），唤醒慢采集。web Dashboard 读快照不走 daemon HTTP，只 touch 标记
// 文件，因此需要在快周期里检查，避免等下一个 10s 慢 tick。
func (c *Collector) MaybeWake() {
	if !c.idle.Load() {
		return
	}
	if c.slowActive(c.now()) {
		c.idle.Store(false)
		c.wakeSlow()
	}
}

// Wake 返回慢采集唤醒通道（main 的 select 使用）。
func (c *Collector) Wake() <-chan struct{} { return c.wake }

func (c *Collector) wakeSlow() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// slowActive 判断是否应执行慢采集：idle_timeout<=0 表示禁用空闲模式。
func (c *Collector) slowActive(now time.Time) bool {
	if c.cfg.IdleTimeout <= 0 {
		return true
	}
	timeout := time.Duration(c.cfg.IdleTimeout) * time.Second
	if last := c.lastClient.Load(); last > 0 && now.Sub(time.Unix(0, last)) <= timeout {
		return true
	}
	if fi, err := os.Stat(c.clientMarker); err == nil && now.Sub(fi.ModTime()) <= timeout {
		return true
	}
	return false
}

// Fast 执行一轮快采集并返回快照字节（host/system/net/battery 直读 + 慢缓存装配）。
func (c *Collector) Fast() ([]byte, error) {
	t0 := c.now()

	// 慢缓存快照（短锁）
	c.mu.Lock()
	model := c.slow.model
	proto := c.slow.wanProto
	docker := c.slow.docker
	clients := c.slow.clients
	services := c.slow.services
	hasServices := c.slow.hasServices
	bandix := c.slow.bandix
	cellular := c.slow.cellular
	var errs []string
	for _, e := range c.slow.errs {
		errs = append(errs, e)
	}
	errs = append(errs, c.slow.probeWarns...)
	c.mu.Unlock()

	s := &snapshot.Snapshot{
		V:        snapshot.ContractVersion,
		Interval: c.cfg.Interval,
		Errors:   append([]string{}, errs...),
	}
	s.Host = c.hostFast(model)
	s.System = c.systemFast()
	s.Net = c.netFast(t0, proto)
	s.Battery = c.batteryFast()
	s.Docker = docker
	s.Clients = clients
	s.Bandix = bandix
	s.Cellular = cellular
	if hasServices {
		s.Services = services
	}
	sort.Strings(s.Errors)

	s.Ts = c.now().Unix()
	ms := c.now().Sub(t0).Milliseconds()
	s.CollectMs = &ms

	return snapshot.Marshal(s)
}

// Slow 执行一轮慢采集：刷新 WAN 明细 / 服务 / Docker / 设备列表 / bandix 缓存。
// 空闲模式（idle_timeout>0）下，无 web/App 活动时直接跳过整轮慢采集。
func (c *Collector) Slow() {
	now := c.now()
	if !c.slowActive(now) {
		if c.idle.CompareAndSwap(false, true) {
			log.Printf("idle: no web/app client for %ds, pausing slow collection", c.cfg.IdleTimeout)
		}
		return
	}
	if c.idle.Swap(false) {
		log.Printf("active: web/app client detected, resuming slow collection")
	}

	wanIface := parseDefaultRoute(readFile("/proc/net/route"))

	// 服务：探针描述文件 + ubus service list
	descs, warns := loadDescriptors([]string{probesShareDir, probesLocalDir})
	services := map[string]any{}
	var svcErr string
	if st, err := ubusServiceStates(); err != nil {
		svcErr = err.Error()
	} else {
		runner := &serviceRunner{svcList: st, execCache: map[string]execResult{}, now: c.now}
		services = buildServices(descs, runner)
	}

	docker, dockerErr := collectDocker()
	clients, clientsErr := collectClients(wanIface)
	bandix, bandixErr := collectBandix(c.now())
	cellular, cellularErr := collectCellular(c.now(), c.cellSeries)
	proto := ubusWANProto(wanIface)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slow.errs == nil {
		c.slow.errs = map[string]string{}
	}
	set := func(k, v string) {
		if v == "" {
			delete(c.slow.errs, k)
		} else {
			c.slow.errs[k] = v
		}
	}

	c.slow.probeWarns = warns
	c.slow.wanProto = proto

	if svcErr != "" {
		set("services", svcErr)
		c.slow.hasServices = false
		c.slow.services = nil
	} else {
		set("services", "")
		c.slow.hasServices = true
		c.slow.services = services
	}
	if dockerErr != nil {
		set("docker", dockerErr.Error())
		c.slow.docker = nil
	} else {
		set("docker", "")
		c.slow.docker = docker
	}
	if clientsErr != nil {
		set("clients", clientsErr.Error())
		c.slow.clients = nil
	} else {
		set("clients", "")
		c.slow.clients = clients
	}
	if bandixErr != nil {
		set("bandix", bandixErr.Error())
		c.slow.bandix = nil
	} else {
		set("bandix", "")
		c.slow.bandix = bandix
	}
	if cellularErr != nil {
		set("cellular", cellularErr.Error())
		c.slow.cellular = nil
		for k := range c.slow.errs { // 整节失败：清掉旧的子块错误
			if strings.HasPrefix(k, "cellular:") {
				delete(c.slow.errs, k)
			}
		}
	} else {
		// 节内子块错误逐条上报（cellular.Errors），恢复即清除
		cur := map[string]bool{}
		for _, e := range cellular.Errors {
			cur[e] = true
		}
		for k, v := range c.slow.errs {
			if strings.HasPrefix(k, "cellular:") && !cur[v] {
				delete(c.slow.errs, k)
			}
		}
		for _, e := range cellular.Errors {
			set("cellular:"+e, e)
		}
		c.slow.cellular = cellular
	}

	// 型号兜底：文件缺失且尚无缓存时低频尝试 ubus system.board
	if c.slow.model == "" && readTrim("/tmp/sysinfo/model") == "" {
		c.slow.model = ubusBoardModel()
	}
}

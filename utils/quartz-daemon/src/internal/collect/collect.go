// Package collect 实现快慢分离的数据采集（DESIGN §2）：
//
//   - 快周期（每 interval，默认 2s）：纯 /proc + /sys 文件读，装配全量快照，
//     一次 marshal → 原子写文件 + HTTP 同字节（单写者，无并发写竞争）
//   - 慢周期（每 10s）：ubus / docker socket / 设备列表 / 探针描述文件，
//     独立超时，失败只记 errors 不阻塞快周期；结果经互斥锁保护的缓存发布
package collect

import (
	"sort"
	"sync"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/config"
	"github.com/quartzwrt/quartz-daemon/internal/snapshot"
)

const (
	// StatusPath 快照文件路径（契约 §1 固定）。
	StatusPath = "/var/run/quartz/status.json"

	// SlowInterval 慢刷周期。
	SlowInterval = 10 * time.Second

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
	errs        map[string]string // 组件 → 当前错误（恢复即清除）
	probeWarns  []string          // 非法探针描述文件告警
}

// New 创建采集器。
func New(cfg config.Config) *Collector {
	return &Collector{
		cfg:    cfg,
		now:    time.Now,
		ringRx: newRing(snapshot.MaxSeries),
		ringTx: newRing(snapshot.MaxSeries),
	}
}

// Fast 执行一轮快采集并返回快照字节（host/system/net 直读 + 慢缓存装配）。
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
	s.Docker = docker
	s.Clients = clients
	if hasServices {
		s.Services = services
	}
	sort.Strings(s.Errors)

	s.Ts = c.now().Unix()
	ms := c.now().Sub(t0).Milliseconds()
	s.CollectMs = &ms

	return snapshot.Marshal(s)
}

// Slow 执行一轮慢采集：刷新 WAN 明细 / 服务 / Docker / 设备列表缓存。
func (c *Collector) Slow() {
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

	// 型号兜底：文件缺失且尚无缓存时低频尝试 ubus system.board
	if c.slow.model == "" && readTrim("/tmp/sysinfo/model") == "" {
		c.slow.model = ubusBoardModel()
	}
}

// Package ratelimit 实现登录防爆破（DESIGN §3.3/§9-D10）。
//
// 插件每调用一进程，限速状态必须跨进程：落 /var/run/quartz-app-api/ratelimit.json
// （tmpfs——重启清空可接受），读改写全程 flock。
//
// 键语义：uhttpd 的 /ubus 端点**不向插件转发客户端 IP**（源码核实：仅注入
// ubus_rpc_session，DESIGN v1.0.1 修正），故 v1 使用**全局键**（GlobalKey）——
// 任意来源的失败统一计数，对单管理员口令场景等价于按源限制且更强（不可绕）。
// 计数器结构保留按任意 key 计数的能力（若未来机制出现客户端标识可直接启用）。
//
// 语义：失败累计 ≥ Fails → 锁定 Base·2^backoff（上限 64×）；成功登录清零。
// 时间戳由调用方注入（可测）。
package ratelimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DefaultPath 限速状态文件（tmpfs）。
const DefaultPath = "/var/run/quartz-app-api/ratelimit.json"

// GlobalKey v1 使用的唯一计数键。
const GlobalKey = "global"

// Config 阈值参数（uci 可调，DESIGN §9-D8）。
type Config struct {
	Fails int // 触发锁定的失败数，默认 5
	Base  int // 基础锁定秒数，默认 300
}

// Defaults 内置默认值。
func Defaults() Config { return Config{Fails: 5, Base: 300} }

type entry struct {
	Fails   int   `json:"fails"`
	Locked  int64 `json:"locked_until"`
	Backoff int   `json:"backoff_count"`
}

type state struct {
	Keys map[string]*entry `json:"keys"`
}

// Limiter 限速器（文件后端）。
type Limiter struct {
	Path string
	Cfg  Config
}

// New 创建限速器（path 为空用 DefaultPath；cfg 非法用默认）。
func New(path string, cfg Config) *Limiter {
	if path == "" {
		path = DefaultPath
	}
	if cfg.Fails <= 0 || cfg.Base <= 0 {
		cfg = Defaults()
	}
	return &Limiter{Path: path, Cfg: cfg}
}

// withState 在 flock 下读改写。
func (l *Limiter) withState(fn func(*state)) error {
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(l.Path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	st := state{}
	if b, err := os.ReadFile(l.Path); err == nil {
		_ = json.Unmarshal(b, &st) // 损坏视为从零开始（限速数据非关键）
	}
	if st.Keys == nil {
		st.Keys = map[string]*entry{}
	}
	fn(&st)
	b, err := json.Marshal(&st)
	if err != nil {
		return err
	}
	tmp := l.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.Path)
}

// Check 查询 key 是否被锁定；锁定返回 retry_after 秒（≥1）。
func (l *Limiter) Check(key string, now time.Time) (bool, int) {
	locked := false
	retry := 0
	_ = l.withState(func(st *state) {
		if e, ok := st.Keys[key]; ok && e.Locked > now.Unix() {
			locked = true
			retry = int(e.Locked - now.Unix())
		}
	})
	if retry < 1 {
		retry = 1
	}
	return locked, retry
}

// Fail 记录一次失败（触发锁定/退避升级）。
func (l *Limiter) Fail(key string, now time.Time) error {
	return l.withState(func(st *state) {
		e := st.Keys[key]
		if e == nil {
			e = &entry{}
			st.Keys[key] = e
		}
		e.Fails++
		if e.Fails >= l.Cfg.Fails {
			mult := 1 << e.Backoff
			if mult > 64 {
				mult = 64
			}
			e.Locked = now.Unix() + int64(l.Cfg.Base*mult)
			e.Fails = 0
			e.Backoff++
		}
	})
}

// Reset 成功登录后清零该 key 的失败/退避/锁定。
func (l *Limiter) Reset(key string, now time.Time) error {
	return l.withState(func(st *state) {
		if e, ok := st.Keys[key]; ok {
			e.Fails = 0
			e.Backoff = 0
			e.Locked = 0
		}
	})
}

// Package config 读取 /etc/config/quartz-app-api（uci）（DESIGN §3/§9-D8）。
//
// 与 daemon 同款读取方式：exec `uci -q show quartz-app-api`，解析
// `quartz-app-api.main.option='value'` 行；缺失/非法 → 内置默认值收敛。
package config

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// initNameRe 白名单项字符集（与 actions 包同规：init 脚本名）。
var initNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// 默认值（DESIGN §9-D8 定案参数）。
const (
	DefaultAccessTTL     = 900      // access_token 15min
	DefaultRefreshTTL    = 2592000  // refresh 滑动 30d
	DefaultRefreshAbsTTL = 15552000 // refresh 绝对上限 180d
	DefaultLoginFails    = 5
	DefaultLockoutBase   = 300
	DefaultGlobalFails   = 20
	DefaultGlobalWindow  = 600
	DefaultDaemonURL     = "http://127.0.0.1:8899/api/v1/status"
)

// Config 运行配置。
type Config struct {
	AccessTTL     int    // 秒
	RefreshTTL    int    // 秒（滑动）
	RefreshAbsTTL int    // 秒（自设备创建起绝对上限）
	LoginFails    int    // 全局限速阈值
	LockoutBase   int    // 基础锁定秒数（指数退避基数）
	GlobalFails   int    // 预留（v1 未用，0）
	GlobalWindow  int    // 预留（v1 未用）
	DaemonURL     string // daemon 快照上游
	// WhitelistExtra 动作白名单追加项（uci list service_whitelist；叠加在
	// actions.DefaultWhitelist 之上，DESIGN §5）。
	WhitelistExtra []string
}

// Defaults 内置默认配置。
func Defaults() Config {
	return Config{
		AccessTTL:     DefaultAccessTTL,
		RefreshTTL:    DefaultRefreshTTL,
		RefreshAbsTTL: DefaultRefreshAbsTTL,
		LoginFails:    DefaultLoginFails,
		LockoutBase:   DefaultLockoutBase,
		GlobalFails:   DefaultGlobalFails,
		GlobalWindow:  DefaultGlobalWindow,
		DaemonURL:     DefaultDaemonURL,
	}
}

// Load 读取系统配置（uci 不可用/无配置时返回默认值）。
func Load() Config {
	cfg := Defaults()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "uci", "-q", "show", "quartz-app-api").Output(); err == nil {
		ApplyUCIShow(string(out), &cfg)
	}
	return cfg
}

// ApplyUCIShow 解析 `uci -q show quartz-app-api` 输出并合并进 cfg（含收敛）。
func ApplyUCIShow(out string, cfg *Config) {
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) < 3 || parts[0] != "quartz-app-api" {
			continue
		}
		val = unquote(val)
		switch parts[len(parts)-1] {
		case "access_ttl":
			setInt(val, &cfg.AccessTTL)
		case "refresh_ttl":
			setInt(val, &cfg.RefreshTTL)
		case "refresh_abs_ttl":
			setInt(val, &cfg.RefreshAbsTTL)
		case "login_fails":
			setInt(val, &cfg.LoginFails)
		case "lockout":
			setInt(val, &cfg.LockoutBase)
		case "global_fails":
			setInt(val, &cfg.GlobalFails)
		case "global_window":
			setInt(val, &cfg.GlobalWindow)
		case "daemon_url":
			if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
				cfg.DaemonURL = val
			}
		case "service_whitelist":
			if initNameRe.MatchString(val) {
				cfg.WhitelistExtra = append(cfg.WhitelistExtra, val)
			}
		}
	}
	clamp(cfg)
}

func setInt(v string, dst *int) {
	if n, err := strconv.Atoi(v); err == nil {
		*dst = n
	}
}

// clamp 范围收敛（防误配把认证窗口关死）。
func clamp(cfg *Config) {
	clampInt(&cfg.AccessTTL, 60, 86400)
	clampInt(&cfg.RefreshTTL, 3600, 31536000)
	clampInt(&cfg.RefreshAbsTTL, cfg.RefreshTTL, 63072000)
	clampInt(&cfg.LoginFails, 1, 100)
	clampInt(&cfg.LockoutBase, 5, 86400)
	clampInt(&cfg.GlobalFails, 0, 10000) // 0 = 关闭全局阈值
	clampInt(&cfg.GlobalWindow, 60, 86400)
	if cfg.DaemonURL == "" {
		cfg.DaemonURL = DefaultDaemonURL
	}
}

func clampInt(v *int, lo, hi int) {
	if *v < lo {
		*v = lo
	}
	if *v > hi {
		*v = hi
	}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

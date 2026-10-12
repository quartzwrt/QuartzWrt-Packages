// Package config 读取 /etc/config/quartz-daemon（uci）。
//
// 读取方式（DESIGN §6）：启动时 exec `uci -q show quartz-daemon`——
// 输出为规范化 `quartz-daemon.main.option='value'` 形式，免自写 uci 解析。
// 缺失文件 / 缺项 / 非法值 → 内置默认值与范围收敛。
package config

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultInterval 采集周期（秒），契约 §1 默认 2s。
	DefaultInterval = 2
	// DefaultListen HTTP 监听地址（回环）。
	DefaultListen = "127.0.0.1"
	// DefaultPort HTTP 端口（VM 实测空闲，DESIGN §9-D4）。
	DefaultPort = 8899

	IntervalMin = 1
	IntervalMax = 300
)

// Config 运行配置。
type Config struct {
	Interval int
	Listen   string
	Port     int
}

// Load 读取系统配置（uci 不可用/无配置时返回默认值）。
func Load() Config {
	cfg := Defaults()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "uci", "-q", "show", "quartz-daemon").Output(); err == nil {
		ApplyUCIShow(string(out), &cfg)
	}
	return cfg
}

// Defaults 返回内置默认配置。
func Defaults() Config {
	return Config{Interval: DefaultInterval, Listen: DefaultListen, Port: DefaultPort}
}

// ApplyUCIShow 解析 `uci -q show quartz-daemon` 输出并合并进 cfg（含范围收敛）。
// 输出样张：quartz-daemon.main.interval='2'
func ApplyUCIShow(out string, cfg *Config) {
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) < 3 || parts[0] != "quartz-daemon" {
			continue
		}
		val = unquote(val)
		switch parts[len(parts)-1] {
		case "interval":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.Interval = n
			}
		case "listen":
			if val != "" {
				cfg.Listen = val
			}
		case "port":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.Port = n
			}
		}
	}
	clamp(cfg)
}

func clamp(cfg *Config) {
	if cfg.Interval < IntervalMin {
		cfg.Interval = IntervalMin
	}
	if cfg.Interval > IntervalMax {
		cfg.Interval = IntervalMax
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		cfg.Port = DefaultPort
	}
	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
}

// unquote 去掉 uci 输出中的单/双引号包裹。
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

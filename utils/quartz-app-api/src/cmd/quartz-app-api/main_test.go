package main

import (
	"testing"
	"time"

	"github.com/quartzwrt/quartz-app-api/internal/config"
	"github.com/quartzwrt/quartz-app-api/internal/envelope"
	"github.com/quartzwrt/quartz-app-api/internal/pkgs"
)

// reply 必须把裸载荷统一包上信封——回归防护：曾因 auth.Logout 返回裸载荷、
// reply 直通而导致 logout 回空 `[0]`（VM 验收暴露）。
func TestReplyWrapsPayload(t *testing.T) {
	out := reply(map[string]any{"device_id": "d1"}, nil)
	if out["ok"] != true || out["api"] != envelope.API || out["device_id"] != "d1" {
		t.Fatalf("wrapped reply: %v", out)
	}
	// 空载荷（logout 等）也必须带信封
	out = reply(map[string]any{}, nil)
	if out["ok"] != true || out["api"] != envelope.API {
		t.Fatalf("empty payload reply: %v", out)
	}
	// nil 载荷同样带信封
	out = reply(nil, nil)
	if out["ok"] != true {
		t.Fatalf("nil payload reply: %v", out)
	}
	// 错误直通错误信封
	out = reply(nil, envelope.E(envelope.CodeRateLimited, "locked"))
	if out["ok"] != false {
		t.Fatalf("error reply: %v", out)
	}
}

func TestMethodListSurface(t *testing.T) {
	m := methodList()
	for _, want := range []string{
		"info", "login", "refresh", "logout", "status", "action", "devices", "revoke", "packages",
		"config.read", "config.write", "config.changes", "config.revert",
		"config.apply", "config.confirm", "config.rollback",
	} {
		if _, has := m[want]; !has {
			t.Fatalf("method list missing %s", want)
		}
	}
	if len(m) != 16 {
		t.Fatalf("method surface changed: %d", len(m))
	}
}

func TestBudgetPerMethod(t *testing.T) {
	// 默认 10s；packages 读 20s+2s；packages.* 动作 50s+2s（DESIGN §4.4 v1.1）
	if got := budget("status", nil); got != pluginTimeout {
		t.Fatalf("status budget: %v", got)
	}
	if got := budget("packages", nil); got <= pluginTimeout {
		t.Fatalf("packages budget should exceed default: %v", got)
	}
	act := map[string]any{"action": "packages.upgrade"}
	if got := budget("action", act); got <= pkgs.QueryTimeout {
		t.Fatalf("packages upgrade budget: %v", got)
	}
	// 所有预算必须落在 uhttpd app 实例单次 /ubus 调用 60s 窗口内
	//（源码核实：uloop_timeout_set(script_timeout × 1000ms)）。
	for _, got := range []time.Duration{budget("packages", nil), budget("action", act)} {
		if got >= 60*time.Second {
			t.Fatalf("budget must stay below uhttpd ubus window: %v", got)
		}
	}
}

func TestWhitelistIncludesDefaultsPlusExtra(t *testing.T) {
	cfg := config.Defaults()
	cfg.WhitelistExtra = []string{"passwall"}
	wl := whitelist(cfg)
	found := map[string]bool{}
	for _, n := range wl {
		found[n] = true
	}
	for _, want := range []string{"openclash", "dockerd", "dnsmasq", "passwall"} {
		if !found[want] {
			t.Fatalf("whitelist missing %s: %v", want, wl)
		}
	}
}

package config

import "testing"

const sampleShow = `quartz-app-api.main=api
quartz-app-api.main.access_ttl='1200'
quartz-app-api.main.refresh_ttl='86400'
quartz-app-api.main.login_fails='7'
quartz-app-api.main.lockout='60'
quartz-app-api.main.daemon_url='http://127.0.0.1:9000/api/v1/status'
other.section.x='1'
`

func TestApplyUCIShow(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow(sampleShow, &cfg)

	if cfg.AccessTTL != 1200 || cfg.RefreshTTL != 86400 {
		t.Fatalf("ttl: %+v", cfg)
	}
	if cfg.LoginFails != 7 || cfg.LockoutBase != 60 {
		t.Fatalf("lockout: %+v", cfg)
	}
	if cfg.DaemonURL != "http://127.0.0.1:9000/api/v1/status" {
		t.Fatalf("daemon_url: %q", cfg.DaemonURL)
	}
	// 未出现的项保持默认
	if cfg.GlobalFails != DefaultGlobalFails || cfg.RefreshAbsTTL != DefaultRefreshAbsTTL {
		t.Fatalf("untouched fields changed: %+v", cfg)
	}
}

func TestClamp(t *testing.T) {
	cfg := Config{AccessTTL: 5, RefreshTTL: 10, RefreshAbsTTL: 1, LoginFails: 0, LockoutBase: 0, GlobalFails: -1, GlobalWindow: 1}
	clamp(&cfg)
	if cfg.AccessTTL != 60 {
		t.Fatalf("access ttl clamp: %d", cfg.AccessTTL)
	}
	if cfg.RefreshTTL != 3600 {
		t.Fatalf("refresh ttl clamp: %d", cfg.RefreshTTL)
	}
	if cfg.RefreshAbsTTL < cfg.RefreshTTL {
		t.Fatalf("abs ttl must be >= refresh ttl: %d", cfg.RefreshAbsTTL)
	}
	if cfg.LoginFails != 1 || cfg.LockoutBase != 5 {
		t.Fatalf("lockout clamp: %+v", cfg)
	}
	if cfg.GlobalFails != 0 {
		t.Fatalf("global fails clamp: %d", cfg.GlobalFails)
	}
	if cfg.GlobalWindow != 60 {
		t.Fatalf("global window clamp: %d", cfg.GlobalWindow)
	}
}

func TestBadDaemonURLIgnored(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow("quartz-app-api.main.daemon_url='file:///etc/shadow'\n", &cfg)
	if cfg.DaemonURL != DefaultDaemonURL {
		t.Fatalf("bad url accepted: %q", cfg.DaemonURL)
	}
}

func TestWhitelistExtra(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow("quartz-app-api.main.service_whitelist='passwall'\nquartz-app-api.main.service_whitelist='bad;name'\nquartz-app-api.main.service_whitelist='adguardhome'\n", &cfg)
	if len(cfg.WhitelistExtra) != 2 || cfg.WhitelistExtra[0] != "passwall" || cfg.WhitelistExtra[1] != "adguardhome" {
		t.Fatalf("whitelist extra: %v", cfg.WhitelistExtra)
	}
}

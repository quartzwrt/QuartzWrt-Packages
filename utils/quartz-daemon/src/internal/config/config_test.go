package config

import "testing"

func TestApplyUCIShow(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow(`quartz-daemon.main=daemon
quartz-daemon.main.interval='5'
quartz-daemon.main.listen='127.0.0.1'
quartz-daemon.main.port='9000'
`, &cfg)
	if cfg.Interval != 5 {
		t.Fatalf("interval = %d, want 5", cfg.Interval)
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.Port != 9000 {
		t.Fatalf("port = %d, want 9000", cfg.Port)
	}
}

func TestApplyUCIShowClamp(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow(`quartz-daemon.main.interval='999'
quartz-daemon.main.port='0'
quartz-daemon.main.listen=''
`, &cfg)
	if cfg.Interval != IntervalMax {
		t.Fatalf("interval = %d, want clamped %d", cfg.Interval, IntervalMax)
	}
	if cfg.Port != DefaultPort {
		t.Fatalf("port = %d, want default %d", cfg.Port, DefaultPort)
	}
	if cfg.Listen != DefaultListen {
		t.Fatalf("listen = %q, want default %q", cfg.Listen, DefaultListen)
	}
}

func TestApplyUCIShowIgnoresGarbage(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow(`not a uci line
otherpkg.main.interval='7'
quartz-daemon.main.interval=notanumber
`, &cfg)
	if cfg.Interval != DefaultInterval {
		t.Fatalf("interval = %d, want default %d", cfg.Interval, DefaultInterval)
	}
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Interval != DefaultInterval || d.Listen != DefaultListen || d.Port != DefaultPort {
		t.Fatalf("unexpected defaults: %+v", d)
	}
}

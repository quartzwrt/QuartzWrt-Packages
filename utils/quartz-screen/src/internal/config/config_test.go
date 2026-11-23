package config

import (
	"testing"
	"time"
)

func TestApplyUCIShow(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow(`quartz-screen.main=screen
quartz-screen.main.enabled='0'
quartz-screen.main.brightness='55'
quartz-screen.main.theme='light'
quartz-screen.main.lang='en'
quartz-screen.main.rotate='180'
quartz-screen.main.interval='15'
quartz-screen.main.pages='overview' 'wireless' 'note'
quartz-screen.main.title='Lab Router'
quartz-screen.main.operator='unicom'
quartz-screen.main.temp_unit='f'
quartz-screen.main.clock_24h='0'
quartz-screen.night=night
quartz-screen.night.enabled='1'
quartz-screen.night.start='22:30'
quartz-screen.night.end='06:15'
quartz-screen.night.brightness='5'
quartz-screen.night.off='1'
quartz-screen.note=note
quartz-screen.note.title='Rack A'
quartz-screen.note.line1='contact: ops'
`, &cfg)

	if cfg.Enabled {
		t.Fatal("enabled should be false")
	}
	if cfg.Brightness != 55 || cfg.Theme != "light" || cfg.Lang != "en" || cfg.Rotate != 180 {
		t.Fatalf("unexpected basic cfg: %+v", cfg)
	}
	if cfg.Interval != 15 {
		t.Fatalf("interval = %d", cfg.Interval)
	}
	want := []string{"overview", "wireless", "note"}
	if len(cfg.Pages) != len(want) {
		t.Fatalf("pages = %v", cfg.Pages)
	}
	for i := range want {
		if cfg.Pages[i] != want[i] {
			t.Fatalf("pages = %v", cfg.Pages)
		}
	}
	if cfg.Title != "Lab Router" || cfg.Operator != "unicom" || cfg.TempUnit != "f" || cfg.Clock24 {
		t.Fatalf("content cfg: %+v", cfg)
	}
	if cfg.Night.Start != "22:30" || !cfg.Night.Off || cfg.Night.Brightness != 5 {
		t.Fatalf("night cfg: %+v", cfg.Night)
	}
	if cfg.Note.Title != "Rack A" || cfg.Note.Lines[0] != "contact: ops" {
		t.Fatalf("note cfg: %+v", cfg.Note)
	}
}

func TestParsePagesDedupAndFilter(t *testing.T) {
	cfg := Defaults()
	ApplyUCIShow(`quartz-screen.main.pages='overview' 'overview' 'bogus' 'clock'`, &cfg)
	if len(cfg.Pages) != 2 || cfg.Pages[0] != "overview" || cfg.Pages[1] != "clock" {
		t.Fatalf("pages = %v", cfg.Pages)
	}
}

func TestParseClockAndWindow(t *testing.T) {
	if v, ok := ParseClock("23:15"); !ok || v != 23*60+15 {
		t.Fatalf("ParseClock = %d %v", v, ok)
	}
	if _, ok := ParseClock("24:00"); ok {
		t.Fatal("24:00 should be invalid")
	}
	cfg := Defaults()
	cfg.Night.Start = "23:00"
	cfg.Night.End = "07:00"
	in := func(h, m int) bool {
		return cfg.InNightWindow(time.Date(2026, 11, 23, h, m, 0, 0, time.Local))
	}
	if !in(23, 30) || !in(2, 0) || !in(6, 59) {
		t.Fatal("expected inside night window")
	}
	if in(12, 0) || in(7, 0) {
		t.Fatal("expected outside night window")
	}
}

func TestLoadWireless(t *testing.T) {
	// 仅验证解析函数在 uci 不可用时返回零值，不依赖真机。
	w := LoadWireless()
	if w.Present && w.SSID == "" {
		t.Fatalf("present but empty ssid: %+v", w)
	}
}

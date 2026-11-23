package pages_test

import (
	"testing"
	"time"

	"github.com/quartzwrt/quartz-screen/internal/config"
	"github.com/quartzwrt/quartz-screen/internal/pages"
	"github.com/quartzwrt/quartz-screen/internal/preview"
	"github.com/quartzwrt/quartz-screen/internal/render"
	"github.com/quartzwrt/quartz-screen/internal/snapshot"
)

func TestResolveAvailability(t *testing.T) {
	cfg := config.Defaults()
	cfg.Pages = []string{"overview", "wireless", "cellular", "note", "system"}
	cfg.Note.Title = "hello"
	var s snapshot.Snapshot
	// cellular 不可用、wireless 不存在、note 有内容
	got := pages.Resolve(cfg, &s, snapshot.Wireless{})
	want := []string{"overview", "note", "system"}
	if len(got) != len(want) {
		t.Fatalf("resolved = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolved = %v", got)
		}
	}

	s.Cellular = &snapshot.Cellular{Available: true, Modems: []snapshot.Modem{{ID: "1_1"}}}
	got = pages.Resolve(cfg, &s, snapshot.Wireless{Present: true, SSID: "x"})
	if len(got) != 5 {
		t.Fatalf("resolved with data = %v", got)
	}
}

func TestRenderAllPages(t *testing.T) {
	if err := render.LoadFonts(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Note.Title = "Note"
	cfg.Note.Lines[0] = "line"
	s, w := preview.Demo()
	now := time.Date(2026, 11, 23, 14, 23, 0, 0, time.Local)
	for _, id := range preview.PageIDs() {
		c := render.NewCanvas(render.ScreenW, render.ScreenH)
		ctx := &pages.Context{
			Now: now, Snapshot: s, Wireless: w, Config: cfg,
			Lang: "zh", Theme: render.Dark, Index: 0, Count: 7,
		}
		pages.Render(c, id, ctx)
		if len(c.Pix) != render.ScreenW*render.ScreenH {
			t.Fatalf("%s canvas size", id)
		}
	}
}

func TestRenderDegraded(t *testing.T) {
	if err := render.LoadFonts(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	for _, id := range preview.PageIDs() {
		c := render.NewCanvas(render.ScreenW, render.ScreenH)
		pages.Render(c, id, &pages.Context{
			Now: time.Now(), Snapshot: nil, Wireless: snapshot.Wireless{},
			Config: cfg, Lang: "zh", Theme: render.Dark, Count: 1,
		})
		if len(c.Pix) != render.ScreenW*render.ScreenH {
			t.Fatalf("%s canvas size", id)
		}
	}
}

func TestRenderEnglishLight(t *testing.T) {
	if err := render.LoadFonts(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Clock24 = false
	s, w := preview.Demo()
	c := render.NewCanvas(render.ScreenW, render.ScreenH)
	pages.Render(c, "overview", &pages.Context{
		Now: time.Now(), Snapshot: s, Wireless: w, Config: cfg,
		Lang: "en", Theme: render.Light, Count: 1,
	})
}

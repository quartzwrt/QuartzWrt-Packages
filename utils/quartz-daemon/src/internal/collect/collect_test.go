package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quartzwrt/quartz-daemon/internal/config"
)

func TestSlowActive(t *testing.T) {
	c := New(config.Config{IdleTimeout: 60})
	c.clientMarker = filepath.Join(t.TempDir(), "client.active")
	now := time.Now()

	c.lastClient.Store(now.Add(-30 * time.Second).UnixNano())
	if !c.slowActive(now) {
		t.Fatal("recent HTTP activity should be active")
	}

	c.lastClient.Store(now.Add(-90 * time.Second).UnixNano())
	if c.slowActive(now) {
		t.Fatal("stale HTTP without marker should be idle")
	}

	if err := os.WriteFile(c.clientMarker, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.slowActive(now) {
		t.Fatal("recent marker should be active")
	}

	old := now.Add(-90 * time.Second)
	if err := os.Chtimes(c.clientMarker, old, old); err != nil {
		t.Fatal(err)
	}
	if c.slowActive(now) {
		t.Fatal("old marker should be idle")
	}

	c.cfg.IdleTimeout = 0
	if !c.slowActive(now) {
		t.Fatal("idle_timeout=0 must disable idle mode")
	}
}

func TestTouchWakesIdle(t *testing.T) {
	c := New(config.Config{IdleTimeout: 60})
	c.idle.Store(true)
	c.Touch()

	select {
	case <-c.Wake():
	default:
		t.Fatal("Touch should wake the slow loop")
	}
	if c.idle.Load() {
		t.Fatal("Touch should clear idle")
	}
}
